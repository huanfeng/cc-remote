// Package update implements "cc-remote update": it finds a newer GitHub
// release, verifies its sha256 against checksums.txt and installs it, either
// through "service install" (when the background service is installed) or by
// replacing the running executable in place.
package update

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"cc-remote/internal/service"
)

const checksumsName = "checksums.txt"

type updater struct {
	repo, current string
	whenIdle      bool // service install --when-idle
	goos, goarch  string
	src           source
	out           io.Writer
	// exePath is the executable replaced when no service is installed.
	exePath string
	// installedPath is where "service install" keeps its copy of the binary.
	installedPath string
	runCmd        func(name string, args ...string) error
}

// Run executes "cc-remote update [--check] [--version vX.Y.Z] [--yes]".
// current is the version compiled into the running binary (main.version).
func Run(current string, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "only report whether a newer release exists")
	version := fs.String("version", "", "install this release tag instead of the latest (e.g. v0.2.0)")
	yes := fs.Bool("yes", false, "install even when not newer (dev builds, reinstall, downgrade)")
	whenIdle := fs.Bool("when-idle", false, "install without stopping the running service; it restarts itself once no session is busy")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: cc-remote update [--check] [--version vX.Y.Z] [--yes] [--when-idle]")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	installed, err := service.InstalledExe()
	if err != nil {
		return err
	}
	repo := os.Getenv("CC_REMOTE_REPO")
	if repo == "" {
		repo = DefaultRepo
	}
	u := &updater{
		repo: repo, current: current, whenIdle: *whenIdle,
		goos: runtime.GOOS, goarch: runtime.GOARCH,
		src: pickSource(repo), out: os.Stdout,
		exePath: exe, installedPath: installed,
		runCmd: func(name string, args ...string) error {
			cmd := exec.Command(name, args...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			return cmd.Run()
		},
	}
	// Bounds metadata requests and downloads; the service reinstall runs
	// outside this context.
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	return u.run(ctx, *check, normalizeTag(*version), *yes)
}

// downloadTimeout caps the whole release lookup and download.
const downloadTimeout = 10 * time.Minute

func (u *updater) run(ctx context.Context, check bool, tag string, yes bool) error {
	service.CleanupAside(u.exePath)
	rel, err := u.src.release(ctx, tag)
	if errors.Is(err, errNotFound) {
		if tag != "" {
			return fmt.Errorf("release %s not found in %s", tag, u.repo)
		}
		// GitHub's "latest" release never includes prereleases.
		return fmt.Errorf("no stable release found in %s (prereleases are skipped; pass --version <tag> to install one)", u.repo)
	}
	if err != nil {
		return err
	}
	cmp, comparable := compareVersions(rel.Tag, u.current)
	fmt.Fprintf(u.out, "current version: %s\nrelease version: %s (via %s)\n", u.current, rel.Tag, u.src.name())
	if check {
		switch {
		case !comparable:
			fmt.Fprintf(u.out, "current build is not a release version; run \"cc-remote update --yes\" to install %s\n", rel.Tag)
		case cmp > 0:
			fmt.Fprintln(u.out, "a newer release is available; run \"cc-remote update\" to install it")
		default:
			fmt.Fprintln(u.out, "already up to date")
		}
		return nil
	}
	if !yes {
		switch {
		case !comparable:
			fmt.Fprintf(u.out, "current build %q is not a release version; rerun with --yes to install %s\n", u.current, rel.Tag)
			return nil
		case cmp == 0:
			fmt.Fprintln(u.out, "already up to date (use --yes to reinstall)")
			return nil
		case cmp < 0:
			fmt.Fprintf(u.out, "%s is older than the current version; rerun with --yes to downgrade\n", rel.Tag)
			return nil
		}
	}
	bin, cleanup, err := u.fetch(ctx, rel)
	if err != nil {
		return err
	}
	defer cleanup()
	return u.apply(bin, rel.Tag)
}

// fetch downloads the platform archive and checksums.txt, verifies the
// archive and extracts it. The returned cleanup removes the temp directory.
func (u *updater) fetch(ctx context.Context, rel Release) (string, func(), error) {
	asset, err := selectAsset(rel.Assets, u.goos, u.goarch)
	if err != nil {
		return "", nil, err
	}
	sumsAsset, ok := findAsset(rel.Assets, checksumsName)
	if !ok {
		return "", nil, fmt.Errorf("release %s has no %s; refusing to install unverified files", rel.Tag, checksumsName)
	}
	tmp, err := os.MkdirTemp("", "cc-remote-update-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	fail := func(err error) (string, func(), error) { cleanup(); return "", nil, err }
	dl := filepath.Join(tmp, "download")
	if err := os.MkdirAll(dl, 0o755); err != nil {
		return fail(err)
	}
	fmt.Fprintf(u.out, "downloading %s\n", asset.Name)
	archive, err := u.src.download(ctx, rel, asset, dl)
	if err != nil {
		return fail(err)
	}
	sumsPath, err := u.src.download(ctx, rel, sumsAsset, dl)
	if err != nil {
		return fail(err)
	}
	f, err := os.Open(sumsPath)
	if err != nil {
		return fail(err)
	}
	sums, err := parseChecksums(f)
	f.Close()
	if err != nil {
		return fail(err)
	}
	want, ok := sums[asset.Name]
	if !ok {
		return fail(fmt.Errorf("%s has no entry for %s", checksumsName, asset.Name))
	}
	if err := verifyFile(archive, want); err != nil {
		return fail(err)
	}
	fmt.Fprintln(u.out, "sha256 verified")
	bin, err := extractArchive(archive, filepath.Join(tmp, "extract"), u.goos)
	if err != nil {
		return fail(err)
	}
	return bin, cleanup, nil
}

func (u *updater) serviceInstalled() bool {
	if u.installedPath == "" {
		return false
	}
	_, err := os.Stat(u.installedPath)
	return err == nil
}

// apply installs the verified binary. With an installed service the new
// binary's own "service install" stops the service, copies itself to the
// stable location and starts it again; otherwise the executable is swapped.
func (u *updater) apply(bin, tag string) error {
	if u.serviceInstalled() {
		fmt.Fprintf(u.out, "service installation found at %s; running \"service install\" from %s\n", u.installedPath, tag)
		args := []string{"service", "install"}
		if u.whenIdle {
			args = append(args, "--when-idle")
		}
		if err := u.runCmd(bin, args...); err != nil {
			return fmt.Errorf("service install with the new binary failed: %w", err)
		}
		fmt.Fprintf(u.out, "updated to %s\n", tag)
		return nil
	}
	if err := replaceExecutable(u.exePath, bin, u.goos); err != nil {
		return err
	}
	fmt.Fprintf(u.out, "updated %s to %s\n", u.exePath, tag)
	return nil
}

// replaceExecutable puts newBin at target. A running Windows executable can be
// renamed but not overwritten, so it moves aside to target+".old" (or
// ".old-<time>" when a stale .old is still locked) and is removed by
// service.CleanupAside on the next update run.
func replaceExecutable(target, newBin, goos string) error {
	staged := target + ".new"
	if err := copyFile(newBin, staged); err != nil {
		return fmt.Errorf("stage new binary next to %s: %w", target, err)
	}
	if goos == "windows" {
		old := service.AsidePath(target)
		if err := os.Rename(target, old); err != nil && !os.IsNotExist(err) {
			os.Remove(staged)
			return fmt.Errorf("move running executable aside: %w", err)
		}
		if err := os.Rename(staged, target); err != nil {
			os.Rename(old, target)
			os.Remove(staged)
			return fmt.Errorf("install new executable: %w", err)
		}
		return nil
	}
	if err := os.Rename(staged, target); err != nil {
		os.Remove(staged)
		return fmt.Errorf("install new executable: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := writeEntry(dst, in, 0o755); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

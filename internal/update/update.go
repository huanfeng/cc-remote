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
	"strings"
	"time"

	"cc-remote/internal/i18n"
	"cc-remote/internal/service"
)

const checksumsName = "checksums.txt"

type updater struct {
	repo, current string
	force         bool // service install --force: restart the running host now
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
	check := fs.Bool("check", false, i18n.T("only report whether a newer release exists", "只检查是否有更新的版本"))
	version := fs.String("version", "", i18n.T("install this release tag instead of the latest (e.g. v0.2.0)", "安装指定版本标签而不是最新版（例如 v0.2.0）"))
	yes := fs.Bool("yes", false, i18n.T("install even when not newer (dev builds, reinstall, downgrade)", "即使不是更新的版本也安装（开发版、重装、降级）"))
	force := fs.Bool("force", false, i18n.T("restart the running service right away, even with busy sessions (default: it restarts itself once no session is busy)", "立即重启运行中的服务，即使有忙碌的会话（默认：等没有忙碌的会话时自行重启）"))
	_ = fs.Bool("when-idle", false, i18n.T("the default now; accepted for older scripts", "现已是默认行为；为兼容旧脚本保留"))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return i18n.Errorf("usage: cc-remote update [--check] [--version vX.Y.Z] [--yes] [--force]", "用法：cc-remote update [--check] [--version vX.Y.Z] [--yes] [--force]")
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
		repo: repo, current: current, force: *force,
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
			return i18n.Errorf("release %s not found in %s", "在 %[2]s 中找不到版本 %[1]s", tag, u.repo)
		}
		// GitHub's "latest" release never includes prereleases.
		return i18n.Errorf("no stable release found in %s (prereleases are skipped; pass --version <tag> to install one)", "%s 中没有正式版本（预发布版本会被跳过；用 --version <标签> 安装指定版本）", u.repo)
	}
	if err != nil {
		return err
	}
	cmp, comparable := compareVersions(rel.Tag, u.current)
	fmt.Fprint(u.out, i18n.F("current version: %s\nrelease version: %s (via %s)\n", "当前版本：%s\n发布版本：%s（通过 %s）\n", u.current, rel.Tag, u.src.name()))
	if check {
		switch {
		case !comparable:
			fmt.Fprint(u.out, i18n.F("current build is not a release version; run \"cc-remote update --yes\" to install %s\n", "当前构建不是正式版本；运行 \"cc-remote update --yes\" 安装 %s\n", rel.Tag))
		case cmp > 0:
			fmt.Fprintln(u.out, i18n.T("a newer release is available; run \"cc-remote update\" to install it", "有更新的版本；运行 \"cc-remote update\" 安装"))
		default:
			fmt.Fprintln(u.out, i18n.T("already up to date", "已是最新版本"))
		}
		return nil
	}
	if !yes {
		switch {
		case !comparable:
			fmt.Fprint(u.out, i18n.F("current build %q is not a release version; rerun with --yes to install %s\n", "当前构建 %q 不是正式版本；加 --yes 重新运行以安装 %s\n", u.current, rel.Tag))
			return nil
		case cmp == 0:
			fmt.Fprintln(u.out, i18n.T("already up to date (use --yes to reinstall)", "已是最新版本（用 --yes 重新安装）"))
			return nil
		case cmp < 0:
			fmt.Fprint(u.out, i18n.F("%s is older than the current version; rerun with --yes to downgrade\n", "%s 比当前版本旧；加 --yes 重新运行以降级\n", rel.Tag))
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
		return "", nil, i18n.Errorf("release %s has no %s; refusing to install unverified files", "版本 %s 没有 %s；拒绝安装未经校验的文件", rel.Tag, checksumsName)
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
	fmt.Fprint(u.out, i18n.F("downloading %s\n", "正在下载 %s\n", asset.Name))
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
		return fail(i18n.Errorf("%s has no entry for %s", "%s 中没有 %s 的条目", checksumsName, asset.Name))
	}
	if err := verifyFile(archive, want); err != nil {
		return fail(err)
	}
	fmt.Fprintln(u.out, i18n.T("sha256 verified", "sha256 校验通过"))
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
		fmt.Fprint(u.out, i18n.F("service installation found at %s; running \"service install\" from %s\n", "在 %s 发现服务安装；运行 %s 的 \"service install\"\n", u.installedPath, tag))
		args := append([]string{"service", "install"}, installFlags(tag, u.force)...)
		if err := u.runCmd(bin, args...); err != nil {
			return i18n.Errorf("service install with the new binary failed: %w", "用新版本执行 service install 失败：%w", err)
		}
		fmt.Fprint(u.out, i18n.F("updated to %s\n", "已更新到 %s\n", tag))
		return nil
	}
	if err := replaceExecutable(u.exePath, bin, u.goos); err != nil {
		return err
	}
	fmt.Fprint(u.out, i18n.F("updated %s to %s\n", "已把 %s 更新到 %s\n", u.exePath, tag))
	return nil
}

// installFlags asks the new binary's "service install" to keep a running
// host until it is idle, or with force to restart it now, in the flags that
// release understands: --when-idle since v0.5.0 (the default since v0.10.0,
// which added --force); earlier releases always restart.
func installFlags(tag string, force bool) []string {
	since := func(min string) bool {
		base, _, _ := strings.Cut(tag, "-")
		cmp, ok := compareVersions(base, min)
		return !ok || cmp >= 0
	}
	switch {
	case force && since("v0.10.0"):
		return []string{"--force"}
	case !force && since("v0.5.0"):
		return []string{"--when-idle"}
	}
	return nil
}

// replaceExecutable puts newBin at target. A running Windows executable can be
// renamed but not overwritten, so it moves aside to target+".old" (or
// ".old-<time>" when a stale .old is still locked) and is removed by
// service.CleanupAside on the next update run.
func replaceExecutable(target, newBin, goos string) error {
	staged := target + ".new"
	if err := copyFile(newBin, staged); err != nil {
		return i18n.Errorf("stage new binary next to %s: %w", "在 %s 旁暂存新版本失败：%w", target, err)
	}
	if goos == "windows" {
		old := service.AsidePath(target)
		if err := os.Rename(target, old); err != nil && !os.IsNotExist(err) {
			os.Remove(staged)
			return i18n.Errorf("move running executable aside: %w", "移开正在运行的可执行文件失败：%w", err)
		}
		if err := os.Rename(staged, target); err != nil {
			os.Rename(old, target)
			os.Remove(staged)
			return i18n.Errorf("install new executable: %w", "安装新的可执行文件失败：%w", err)
		}
		return nil
	}
	if err := os.Rename(staged, target); err != nil {
		os.Remove(staged)
		return i18n.Errorf("install new executable: %w", "安装新的可执行文件失败：%w", err)
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

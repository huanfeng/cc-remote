package update

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIVersion(t *testing.T) {
	for _, tc := range []struct{ id, output, want string }{
		{"claude", "2.1.89 (Claude Code)\n", "2.1.89"},
		{"codex", "codex-cli 0.130.0-alpha.1\r\n", "0.130.0-alpha.1"},
		{"codex", "codex-cli 0.130.0+build.1", "0.130.0+build.1"},
		{"codex", "codex-cli development", ""},
		{"codex", "codex-cli 0.030.0", ""},
		{"codex", "codex-cli 0.130.0-01", ""},
	} {
		if got := cliVersion(tc.id, tc.output); got != tc.want {
			t.Errorf("cliVersion(%q, %q) = %q, want %q", tc.id, tc.output, got, tc.want)
		}
	}
	for _, unsafe := range []string{"latest", "^0.130.0", "file:./package", "0.130.0 --force", "0.130.0;done", "0.130.0+../package"} {
		if validCLIVersion(unsafe) {
			t.Errorf("accepted non-concrete package version %q", unsafe)
		}
	}
}

func TestCLIUpdateCommandOutputCap(t *testing.T) {
	var out cliOutput
	chunk := strings.Repeat("x", 60<<10)
	for range 3 {
		n, err := out.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("write count %d, err %v", n, err)
		}
	}
	if got := out.buf.Len(); got != 64<<10 {
		t.Fatalf("retained %d bytes", got)
	}
}

func TestCLINPMInstallationOwnership(t *testing.T) {
	spec, err := cliFor("codex")
	if err != nil {
		t.Fatal(err)
	}
	prefix := t.TempDir()
	root := filepath.Join(prefix, "node_modules")
	pkg := filepath.Join(root, "@openai", "codex")
	entry := filepath.Join(pkg, "bin", "codex.js")
	shim := filepath.Join(prefix, "codex.cmd")
	writeCLIFile(t, entry, "// package entry")
	manifest := filepath.Join(pkg, "package.json")
	writeCLIFile(t, manifest, `{"name":"@openai/codex","bin":{"codex":"bin/codex.js"}}`)
	writeCLIFile(t, shim, `"%_prog%" "%dp0%\node_modules\@openai\codex\bin\codex.js" %*`)
	if got := npmPackageEntry(root, spec); !samePath(got, entry, runtime.GOOS) {
		t.Fatalf("entry = %q, want %q", got, entry)
	}
	if !npmLauncher(shim, prefix, "codex", "windows") || !npmShimOwns(shim, entry, root, spec) {
		t.Fatal("npm package launcher not recognized")
	}
	writeCLIFile(t, shim, `"C:\another\codex.exe" %*`)
	if npmShimOwns(shim, entry, root, spec) {
		t.Fatal("accepted a launcher targeting another binary")
	}
	writeCLIFile(t, manifest, `{"name":"@openai/codex","bin":"../../unrelated.js"}`)
	writeCLIFile(t, filepath.Join(root, "unrelated.js"), "")
	if got := npmPackageEntry(root, spec); got != "" {
		t.Fatalf("accepted entry outside package: %q", got)
	}
}

func TestCLINativeWindowsClaudeLauncherOwnership(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".local", "bin", "claude.exe")
	claudeVersion := filepath.Join(home, ".local", "share", "claude", "versions", "2.1.89")
	writeCLIFile(t, claudeVersion, "binary")
	writeCLIFile(t, claude, "different launcher")
	if windowsClaudeVersionMatches(claude, home, "2.1.89") {
		t.Fatal("accepted mismatched Windows Claude launcher")
	}
	writeCLIFile(t, claude, "binary")
	if !nativeClaude(claude, home, "windows") || !windowsClaudeVersionMatches(claude, home, "2.1.89") {
		t.Fatal("failed to detect the Windows Claude launcher")
	}
}

func TestCLINativeUnixLauncherOwnership(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable symlink fixtures require Unix")
	}
	home := t.TempDir()
	claude := filepath.Join(home, ".local", "bin", "claude")
	claudeVersion := filepath.Join(home, ".local", "share", "claude", "versions", "2.1.89")
	writeCLIFile(t, claudeVersion, "binary")
	writeCLIFile(t, claude, "custom launcher")
	if nativeClaude(claude, home, "linux") {
		t.Fatal("accepted customized Claude launcher")
	}
	if err := os.Remove(claude); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(claudeVersion, claude); err != nil {
		t.Fatal(err)
	}
	if !nativeClaude(claude, home, "linux") || nativeClaude(claudeVersion, home, "linux") {
		t.Fatal("Claude ownership must identify the movable launcher, not a pinned binary")
	}
	owner := filepath.Join(home, "packages", "standalone")
	release := filepath.Join(owner, "releases", "0.130.0-test")
	writeCLIFile(t, filepath.Join(release, "bin", "codex"), "binary")
	current := filepath.Join(owner, "current")
	if err := os.Symlink(release, current); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Dir(claude)
	codex := filepath.Join(binDir, "codex")
	if err := os.Symlink(filepath.Join(current, "bin", "codex"), codex); err != nil {
		t.Fatal(err)
	}
	if !nativeCodex(codex, home, binDir, "linux") || nativeCodex(filepath.Join(release, "bin", "codex"), home, binDir, "linux") {
		t.Fatal("Codex ownership must identify the movable launcher, not a pinned binary")
	}
}

func TestCLIInstallPinnedNPMRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake npm executable uses a Unix shell")
	}
	prefix := t.TempDir()
	bin := filepath.Join(prefix, "bin")
	root := filepath.Join(prefix, "lib", "node_modules")
	pkg := filepath.Join(root, "@openai", "codex")
	entry := filepath.Join(pkg, "bin", "codex")
	launcher := filepath.Join(bin, "codex")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLI_UPDATE_TEST_PREFIX", prefix)
	t.Setenv("CLI_UPDATE_TEST_ROOT", root)
	t.Setenv("CLI_UPDATE_TEST_ENTRY", entry)
	t.Setenv("CC_REMOTE_TOKEN", "server-token")
	writeCLIFile(t, entry, "#!/bin/sh\nprintf 'codex-cli 0.130.0\\n'\n")
	writeCLIFile(t, filepath.Join(pkg, "package.json"), `{"name":"@openai/codex","bin":"bin/codex"}`)
	writeCLIFile(t, filepath.Join(bin, "npm"), `#!/bin/sh
case "$1" in
  prefix) printf '%s\n' "$CLI_UPDATE_TEST_PREFIX" ;;
  root) printf '%s\n' "$CLI_UPDATE_TEST_ROOT" ;;
  install)
    test -z "$CC_REMOTE_TOKEN" || exit 2
    test "$*" = "install --global --prefix $CLI_UPDATE_TEST_PREFIX --no-audit --no-fund @openai/codex@0.131.0" || exit 3
    printf '#!/bin/sh\nprintf "codex-cli 0.131.0\\n"\n' > "$CLI_UPDATE_TEST_ENTRY"
    ;;
  *) exit 4 ;;
esac
`)
	if err := os.Symlink(entry, launcher); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		version, want string
		wantErr       bool
	}{
		{"0.130.0;echo unsafe", "0.130.0", true},
		{"0.129.0", "0.130.0", true},
		{"0.131.0", "0.131.0", false},
		{"0.131.0", "0.131.0", true},
	} {
		got, err := InstallCLI(context.Background(), "codex", launcher, "", CLICandidate{CurrentVersion: "0.130.0", Version: tc.version, Available: true})
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("version %q: result %q, error %v", tc.version, got, err)
		}
	}
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

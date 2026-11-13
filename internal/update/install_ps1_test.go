//go:build windows

package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These are intentionally non-executable fixture files. Every installation uses
// -NoService -NoAlias -NoLaunch, so tests never touch the user's host or apps.
var installerExecutables = []string{"cc-remote.exe", "cc-remote-desktop.exe", "cc-remote-tray.exe"}

type installerFixture struct {
	t                *testing.T
	shell, script    string
	root, installDir string
	appData, tempDir string
	env              []string
	mu               sync.RWMutex
	assets           map[string][]byte
	downloads        atomic.Int32
}

func newInstallerFixture(t *testing.T, shell string) *installerFixture {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	f := &installerFixture{t: t, shell: shell, script: script, root: t.TempDir()}
	f.installDir = filepath.Join(f.root, "program files 中文", "bin")
	f.appData = filepath.Join(f.root, "roaming")
	f.tempDir = filepath.Join(f.root, "temp")
	for _, dir := range []string{f.appData, f.tempDir, filepath.Join(f.root, "local"), filepath.Join(f.root, "empty-path")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.RLock()
		defer f.mu.RUnlock()
		switch r.URL.Path {
		case "/repos/test/repo/releases/latest", "/repos/test/repo/releases/tags/v0.28.0":
			w.Header().Set("Content-Type", "application/json")
			assets := []map[string]string{}
			for name := range f.assets {
				assets = append(assets, map[string]string{"name": name, "id": name})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.28.0", "assets": assets})
		default:
			name := strings.TrimPrefix(r.URL.Path, "/repos/test/repo/releases/assets/")
			if data, ok := f.assets[name]; ok {
				f.downloads.Add(1)
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(data)
			} else {
				http.NotFound(w, r)
			}
		}
	}))
	t.Cleanup(server.Close)
	// Keep system variables required by PowerShell, but prevent credential, gh,
	// proxy, or installer configuration from escaping the fixture environment.
	replaced := map[string]string{
		"LOCALAPPDATA": filepath.Join(f.root, "local"), "APPDATA": f.appData,
		"TEMP": f.tempDir, "TMP": f.tempDir, "PATH": filepath.Join(f.root, "empty-path"),
		"PROCESSOR_ARCHITECTURE": "AMD64", "PROCESSOR_ARCHITEW6432": "",
		"CC_REMOTE_API_URL": server.URL, "CC_REMOTE_REPO": "", "CC_REMOTE_LANG": "",
		"GITHUB_TOKEN": "", "GH_TOKEN": "", "HTTP_PROXY": "", "HTTPS_PROXY": "", "ALL_PROXY": "",
		"NO_PROXY":     "127.0.0.1,localhost",
		"PSMODULEPATH": filepath.Join(filepath.Dir(shell), "Modules"),
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replace := replaced[strings.ToUpper(key)]; !replace {
			f.env = append(f.env, entry)
		}
	}
	for key, value := range replaced {
		if value != "" {
			f.env = append(f.env, key+"="+value)
		}
	}
	f.release("first", "", false)
	return f
}

func (f *installerFixture) release(version, missing string, badChecksum bool) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assets = map[string][]byte{}
	var checksums strings.Builder
	for _, desktop := range []bool{false, true} {
		var data bytes.Buffer
		archive := zip.NewWriter(&data)
		names := installerExecutables[:1]
		prefix := "cc-remote"
		if desktop {
			names = installerExecutables
			prefix = "cc-remote-desktop"
		}
		for _, name := range names {
			if name == missing {
				continue
			}
			entry, err := archive.Create(name)
			if err != nil {
				f.t.Fatal(err)
			}
			_, _ = fmt.Fprint(entry, version+":"+name)
		}
		// Extra archive entries must never be extracted, including traversal paths.
		for _, name := range []string{"unexpected.txt", "../outside.txt"} {
			entry, err := archive.Create(name)
			if err != nil {
				f.t.Fatal(err)
			}
			_, _ = fmt.Fprint(entry, "must not extract")
		}
		if err := archive.Close(); err != nil {
			f.t.Fatal(err)
		}
		name := prefix + "_0.28.0_windows_amd64.zip"
		f.assets[name] = data.Bytes()
		hash := sha256.Sum256(data.Bytes())
		if badChecksum {
			hash = [32]byte{}
		}
		fmt.Fprintf(&checksums, "%x  %s\n", hash, name)
	}
	f.assets["checksums.txt"] = []byte(checksums.String())
}

func (f *installerFixture) run(wantError string, options ...string) string {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", f.script,
		"-Repo", "test/repo", "-InstallDir", f.installDir, "-NoService", "-NoAlias"}
	cmd := exec.CommandContext(ctx, f.shell, append(args, options...)...)
	cmd.Env = f.env
	output, err := cmd.CombinedOutput()
	if wantError == "" {
		if err != nil {
			f.t.Fatalf("installer failed: %v\n%s", err, output)
		}
	} else if err == nil || !strings.Contains(string(output), wantError) {
		f.t.Fatalf("wanted failure containing %q; got %v\n%s", wantError, err, output)
	}
	leftovers, err := filepath.Glob(filepath.Join(f.tempDir, "cc-remote-install-*"))
	if err != nil || len(leftovers) != 0 {
		f.t.Fatalf("installer staging directories not cleaned: %v, %v", leftovers, err)
	}
	return string(output)
}

func (f *installerFixture) assertFile(path, want string) {
	f.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		f.t.Fatalf("%s: wanted %q, got %q (%v)", path, want, data, err)
	}
}

func (f *installerFixture) assertAbsent(path string) {
	f.t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		f.t.Fatalf("expected %s absent; got %v", path, err)
	}
}

func TestPowerShellInstaller(t *testing.T) {
	for _, name := range []string{"powershell.exe", "pwsh.exe"} {
		shell, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Run("backend compatibility", func(t *testing.T) {
				f := newInstallerFixture(t, shell)
				f.run("")
				f.assertFile(filepath.Join(f.installDir, installerExecutables[0]), "first:cc-remote.exe")
				f.assertAbsent(filepath.Join(f.installDir, installerExecutables[1]))
				f.assertAbsent(filepath.Join(f.appData, "Microsoft", "Windows", "Start Menu"))
			})
			t.Run("desktop install and repeat update", func(t *testing.T) {
				f := newInstallerFixture(t, shell)
				f.run("", "-Desktop", "-NoLaunch", "-Version", "0.28.0")
				startup := filepath.Join(f.appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "CC Remote Tray.lnk")
				f.assertAbsent(startup)
				for _, name := range installerExecutables {
					f.assertFile(filepath.Join(f.installDir, name), "first:"+name)
				}
				f.assertAbsent(filepath.Join(f.installDir, "unexpected.txt"))
				for _, name := range []string{"CC Remote.lnk", "CC Remote Tray.lnk"} {
					path := filepath.Join(f.appData, "Microsoft", "Windows", "Start Menu", "Programs", "CC Remote", name)
					if info, err := os.Stat(path); err != nil || info.Size() == 0 {
						t.Fatalf("missing Start Menu shortcut %s: %v", path, err)
					}
				}
				preserved := filepath.Join(f.root, "local", "cc-remote", "history-and-accounts.json")
				if err := os.MkdirAll(filepath.Dir(preserved), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(preserved, []byte("keep my data"), 0600); err != nil {
					t.Fatal(err)
				}
				f.release("second", "", false)
				f.run("", "-Desktop", "-NoLaunch", "-AutoStart")
				for _, name := range installerExecutables {
					f.assertFile(filepath.Join(f.installDir, name), "second:"+name)
				}
				f.assertFile(preserved, "keep my data")
				if info, err := os.Stat(startup); err != nil || info.Size() == 0 {
					t.Fatalf("missing startup shortcut: %v", err)
				}
				// An update without -AutoStart retains the explicit opt-in.
				f.run("", "-Desktop", "-NoLaunch")
				if _, err := os.Stat(startup); err != nil {
					t.Fatal(err)
				}
				for _, failure := range []struct {
					missing, message string
					corrupt          bool
				}{
					{message: "sha256 mismatch", corrupt: true},
					{missing: "cc-remote-tray.exe", message: "archive must contain one nonempty cc-remote-tray.exe"},
				} {
					f.release("bad", failure.missing, failure.corrupt)
					f.run(failure.message, "-Desktop", "-NoLaunch")
					for _, name := range installerExecutables {
						f.assertFile(filepath.Join(f.installDir, name), "second:"+name)
					}
					f.assertFile(preserved, "keep my data")
				}
			})
			t.Run("default launch starts only the hidden tray", func(t *testing.T) {
				f := newInstallerFixture(t, shell)
				record := filepath.Join(f.root, "launch.json")
				f.env = append(f.env, "CC_REMOTE_TEST_SCRIPT="+f.script,
					"CC_REMOTE_TEST_DIR="+f.installDir, "CC_REMOTE_TEST_LAUNCH="+record)
				// Mock just process creation. Downloads, verification, installation,
				// process selection and shortcut creation still execute the real script.
				wrapper := `function Start-Process {
    param($FilePath, $WorkingDirectory, $WindowStyle)
    @{ FilePath = $FilePath; WorkingDirectory = $WorkingDirectory; WindowStyle = $WindowStyle } |
        ConvertTo-Json -Compress | Add-Content -LiteralPath $env:CC_REMOTE_TEST_LAUNCH -Encoding UTF8
}
& $env:CC_REMOTE_TEST_SCRIPT -Repo test/repo -InstallDir $env:CC_REMOTE_TEST_DIR -Desktop -NoService -NoAlias
if (-not $?) { exit 1 }
$shell = New-Object -ComObject WScript.Shell
$menu = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\CC Remote'
foreach ($entry in @(@('CC Remote.lnk', 'cc-remote-desktop.exe'), @('CC Remote Tray.lnk', 'cc-remote-tray.exe'))) {
    $shortcut = $shell.CreateShortcut((Join-Path $menu $entry[0]))
    if ($shortcut.TargetPath -ne (Join-Path $env:CC_REMOTE_TEST_DIR $entry[1])) { throw 'shortcut target mismatch' }
    if ($shortcut.WorkingDirectory -ne $env:CC_REMOTE_TEST_DIR) { throw 'shortcut working directory mismatch' }
}
`
				f.script = filepath.Join(f.root, "launch-test.ps1")
				if err := os.WriteFile(f.script, []byte(wrapper), 0600); err != nil {
					t.Fatal(err)
				}
				f.run("")
				data, err := os.ReadFile(record)
				if err != nil {
					t.Fatal(err)
				}
				var launch map[string]string
				if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &launch); err != nil {
					t.Fatalf("expected exactly one launch record: %v\n%s", err, data)
				}
				if launch["FilePath"] != filepath.Join(f.installDir, "cc-remote-tray.exe") ||
					launch["WorkingDirectory"] != f.installDir || launch["WindowStyle"] != "Hidden" {
					t.Fatalf("unexpected launch: %v", launch)
				}
			})
			t.Run("dry run and unsupported architecture", func(t *testing.T) {
				f := newInstallerFixture(t, shell)
				f.run("", "-Desktop", "-NoLaunch", "-DryRun")
				f.assertAbsent(f.installDir)
				if f.downloads.Load() != 0 {
					t.Fatal("dry run downloaded release assets")
				}
				for i, value := range f.env {
					if strings.HasPrefix(value, "PROCESSOR_ARCHITECTURE=") {
						f.env[i] = "PROCESSOR_ARCHITECTURE=ARM64"
					}
				}
				f.run("Windows x64 only", "-Desktop", "-NoLaunch")
				f.assertAbsent(f.installDir)
			})
		})
	}
}

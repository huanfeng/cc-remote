package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"cc-remote/internal/service"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		cmp  int
		ok   bool
	}{
		{"v0.2.0", "v0.1.9", 1, true},
		{"0.2.0", "v0.2.0", 0, true},
		{"v1.10.0", "v1.9.0", 1, true},
		{"v1.0.0", "v1.0.0-rc.1", 1, true},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1, true},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1, true},
		{"v1.0.0-1", "v1.0.0-alpha", -1, true},
		{"v1.0.0+build.5", "v1.0.0", 0, true},
		{"v1.0.0", "dev", 0, false},
		{"v1.0", "v1.0.0", 0, false},
		{"v1.0.0-", "v1.0.0", 0, false},
	}
	for _, c := range cases {
		cmp, ok := compareVersions(c.a, c.b)
		if cmp != c.cmp || ok != c.ok {
			t.Errorf("compareVersions(%q, %q) = %d, %v; want %d, %v", c.a, c.b, cmp, ok, c.cmp, c.ok)
		}
	}
	if normalizeTag("0.2.0") != "v0.2.0" || normalizeTag("v0.2.0") != "v0.2.0" || normalizeTag("") != "" {
		t.Error("normalizeTag")
	}
}

func TestSelectAsset(t *testing.T) {
	assets := []Asset{
		{Name: "checksums.txt"},
		{Name: "install.sh"},
		{Name: "cc-remote_0.2.0_windows_amd64.zip"},
		{Name: "cc-remote_0.2.0_windows_arm64.zip"},
		{Name: "cc-remote_0.2.0_linux_amd64.tar.gz"},
		{Name: "cc-remote_0.2.0_darwin_arm64.tar.gz"},
	}
	for _, c := range []struct{ goos, goarch, want string }{
		{"windows", "amd64", "cc-remote_0.2.0_windows_amd64.zip"},
		{"windows", "arm64", "cc-remote_0.2.0_windows_arm64.zip"},
		{"linux", "amd64", "cc-remote_0.2.0_linux_amd64.tar.gz"},
		{"darwin", "arm64", "cc-remote_0.2.0_darwin_arm64.tar.gz"},
	} {
		a, err := selectAsset(assets, c.goos, c.goarch)
		if err != nil || a.Name != c.want {
			t.Errorf("%s/%s: got %q, %v", c.goos, c.goarch, a.Name, err)
		}
	}
	if _, err := selectAsset(assets, "linux", "arm64"); err == nil {
		t.Error("expected error for missing linux/arm64 asset")
	}
}

func TestParseChecksums(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	in := sum + "  cc-remote_0.2.0_linux_amd64.tar.gz\n\n" + strings.ToUpper(sum) + " *cc-remote_0.2.0_windows_amd64.zip\n"
	got, err := parseChecksums(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if got["cc-remote_0.2.0_linux_amd64.tar.gz"] != sum || got["cc-remote_0.2.0_windows_amd64.zip"] != sum {
		t.Fatalf("unexpected map %v", got)
	}
	for _, bad := range []string{"xyz  file\n", sum + "\n", strings.Repeat("zz", 32) + "  file\n"} {
		if _, err := parseChecksums(strings.NewReader(bad)); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestVerifyFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	os.WriteFile(p, []byte("hello"), 0o644)
	if err := verifyFile(p, sha256Hex([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(p, sha256Hex([]byte("other"))); err == nil {
		t.Fatal("expected mismatch")
	}
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type entry struct {
	name, body string
}

func makeZip(t *testing.T, path string, entries ...entry) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, e.body)
	}
	zw.Close()
	os.WriteFile(path, buf.Bytes(), 0o644)
}

func makeTarGz(t *testing.T, path string, entries ...entry) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if strings.HasSuffix(e.name, "/") {
			tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o755, Typeflag: tar.TypeDir})
			continue
		}
		tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg})
		io.WriteString(tw, e.body)
	}
	tw.Close()
	gz.Close()
	os.WriteFile(path, buf.Bytes(), 0o644)
}

func TestExtractArchives(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	makeZip(t, zipPath, entry{"README.md", "readme"}, entry{"cc-remote.exe", "WINBIN"})
	bin, err := extractArchive(zipPath, filepath.Join(dir, "zip"), "windows")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "WINBIN" {
		t.Fatalf("zip binary = %q", b)
	}
	tgz := filepath.Join(dir, "a.tar.gz")
	makeTarGz(t, tgz, entry{"cc-remote", "UNIXBIN"})
	bin, err = extractArchive(tgz, filepath.Join(dir, "tgz"), "linux")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "UNIXBIN" {
		t.Fatalf("tar binary = %q", b)
	}
	if _, err := extractArchive(tgz, filepath.Join(dir, "tgz2"), "windows"); err == nil {
		t.Fatal("expected missing cc-remote.exe error")
	}
}

// TestExtractDotRootTarGz mirrors the release layout written by
// "tar -C stage -czf x.tar.gz .": a "./" root entry followed by "./name" files.
func TestExtractDotRootTarGz(t *testing.T) {
	dir := t.TempDir()
	tgz := filepath.Join(dir, "a.tar.gz")
	makeTarGz(t, tgz, entry{"./", ""}, entry{"./LICENSE", "mit"}, entry{"./cc-remote", "UNIXBIN"})
	bin, err := extractArchive(tgz, filepath.Join(dir, "out"), "linux")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "UNIXBIN" {
		t.Fatalf("tar binary = %q", b)
	}
	// A regular file named "." must not be written over the destination.
	bad := filepath.Join(dir, "bad.tar.gz")
	makeTarGz(t, bad, entry{".", "x"})
	if _, err := extractArchive(bad, filepath.Join(dir, "bad"), "linux"); err == nil {
		t.Fatal("tar file entry \".\": expected error")
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	names := []string{"../evil", "a/../../evil", "/abs/evil", `..\evil`}
	if runtime.GOOS == "windows" {
		names = append(names, "C:/evil", "C:evil")
	}
	for _, name := range names {
		dir := t.TempDir()
		zipPath := filepath.Join(dir, "a.zip")
		makeZip(t, zipPath, entry{name, "x"})
		if _, err := extractArchive(zipPath, filepath.Join(dir, "out"), "windows"); err == nil || !strings.Contains(err.Error(), "escapes") {
			t.Errorf("zip %q: expected traversal error, got %v", name, err)
		}
		tgz := filepath.Join(dir, "a.tar.gz")
		makeTarGz(t, tgz, entry{name, "x"})
		if _, err := extractArchive(tgz, filepath.Join(dir, "out2"), "linux"); err == nil || !strings.Contains(err.Error(), "escapes") {
			t.Errorf("tar %q: expected traversal error, got %v", name, err)
		}
	}
}

// fakeGitHub serves a release through the REST API shapes used by apiSource.
func fakeGitHub(t *testing.T, tag string, files map[string][]byte, token string) *httptest.Server {
	t.Helper()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return false
		}
		return true
	}
	release := func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		type asset struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		var as []asset
		for i, n := range names {
			as = append(as, asset{int64(i + 1), n})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": as})
	}
	mux.HandleFunc("/repos/o/r/releases/latest", release)
	mux.HandleFunc("/repos/o/r/releases/tags/"+tag, release)
	mux.HandleFunc("/repos/o/r/releases/assets/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.Header.Get("Accept") != "application/octet-stream" {
			http.Error(w, "want octet-stream", http.StatusBadRequest)
			return
		}
		var id int
		fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/repos/o/r/releases/assets/"), "%d", &id)
		if id < 1 || id > len(names) {
			http.NotFound(w, r)
			return
		}
		w.Write(files[names[id-1]])
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func releaseFiles(t *testing.T, version string, corrupt bool) map[string][]byte {
	dir := t.TempDir()
	tgz := filepath.Join(dir, "a.tar.gz")
	makeTarGz(t, tgz, entry{"cc-remote", "NEW-" + version})
	archive, _ := os.ReadFile(tgz)
	name := "cc-remote_" + version + "_linux_amd64.tar.gz"
	sum := sha256Hex(archive)
	if corrupt {
		sum = sha256Hex([]byte("tampered"))
	}
	return map[string][]byte{
		name:            archive,
		"checksums.txt": []byte(sum + "  " + name + "\n"),
	}
}

func newTestUpdater(t *testing.T, srv *httptest.Server, token, current string) (*updater, *bytes.Buffer, *[]string) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cc-remote")
	os.WriteFile(exe, []byte("OLD"), 0o755)
	var out bytes.Buffer
	var ran []string
	u := &updater{
		repo: "o/r", current: current, goos: "linux", goarch: "amd64",
		src:     &apiSource{base: srv.URL, repo: "o/r", token: token, client: srv.Client()},
		out:     &out,
		exePath: exe, installedPath: filepath.Join(dir, "not-installed"),
		runCmd: func(name string, args ...string) error {
			ran = append(ran, filepath.Base(name)+" "+strings.Join(args, " "))
			return nil
		},
	}
	return u, &out, &ran
}

func TestUpdateReplacesExecutable(t *testing.T) {
	srv := fakeGitHub(t, "v0.3.0", releaseFiles(t, "0.3.0", false), "tok")
	u, out, _ := newTestUpdater(t, srv, "tok", "v0.2.0")
	if err := u.run(context.Background(), false, "", false); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(u.exePath); string(b) != "NEW-0.3.0" {
		t.Fatalf("exe = %q\n%s", b, out)
	}
}

func TestUpdateWindowsMovesRunningExeAside(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cc-remote.exe")
	newBin := filepath.Join(dir, "new.exe")
	os.WriteFile(target, []byte("OLD"), 0o755)
	os.WriteFile(newBin, []byte("NEW"), 0o755)
	if err := replaceExecutable(target, newBin, "windows"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "NEW" {
		t.Fatalf("target = %q", b)
	}
	if b, _ := os.ReadFile(target + ".old"); string(b) != "OLD" {
		t.Fatalf(".old = %q", b)
	}
	os.WriteFile(target+".old-123", []byte("OLDER"), 0o755)
	service.CleanupAside(target)
	if left, _ := filepath.Glob(target + ".old*"); len(left) != 0 {
		t.Fatalf("not cleaned: %v", left)
	}
}

func TestUpdateUsesServiceInstall(t *testing.T) {
	srv := fakeGitHub(t, "v0.3.0", releaseFiles(t, "0.3.0", false), "tok")
	u, out, ran := newTestUpdater(t, srv, "tok", "v0.2.0")
	os.WriteFile(u.installedPath, []byte("installed"), 0o755)
	if err := u.run(context.Background(), false, "", false); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(*ran) != 1 || (*ran)[0] != "cc-remote service install" {
		t.Fatalf("ran = %v", *ran)
	}
	if b, _ := os.ReadFile(u.exePath); string(b) != "OLD" {
		t.Fatal("executable must not be replaced when the service handles the update")
	}
}

func TestUpdateRejectsChecksumMismatch(t *testing.T) {
	srv := fakeGitHub(t, "v0.3.0", releaseFiles(t, "0.3.0", true), "tok")
	u, _, _ := newTestUpdater(t, srv, "tok", "v0.2.0")
	err := u.run(context.Background(), false, "", false)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected checksum error, got %v", err)
	}
	if b, _ := os.ReadFile(u.exePath); string(b) != "OLD" {
		t.Fatal("executable replaced despite checksum mismatch")
	}
}

func TestUpdateVersionGating(t *testing.T) {
	srv := fakeGitHub(t, "v0.3.0", releaseFiles(t, "0.3.0", false), "tok")
	for _, c := range []struct {
		current string
		check   bool
		yes     bool
		replace bool
		msg     string
	}{
		{"v0.3.0", false, false, false, "already up to date"},
		{"v0.4.0", false, false, false, "downgrade"},
		{"dev", false, false, false, "--yes"},
		{"dev", false, true, true, "updated"},
		{"v0.2.0", true, false, false, "newer release is available"},
	} {
		u, out, _ := newTestUpdater(t, srv, "tok", c.current)
		if err := u.run(context.Background(), c.check, "", c.yes); err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		b, _ := os.ReadFile(u.exePath)
		if (string(b) != "OLD") != c.replace || !strings.Contains(out.String(), c.msg) {
			t.Errorf("%+v: exe=%q out=%s", c, b, out)
		}
	}
}

func TestUpdateSpecificTagAndErrors(t *testing.T) {
	srv := fakeGitHub(t, "v0.3.0", releaseFiles(t, "0.3.0", false), "tok")
	u, out, _ := newTestUpdater(t, srv, "tok", "v0.2.0")
	if err := u.run(context.Background(), false, "v0.3.0", false); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	u, _, _ = newTestUpdater(t, srv, "tok", "v0.2.0")
	if err := u.run(context.Background(), false, "v9.9.9", false); err == nil || !strings.Contains(err.Error(), "v9.9.9 not found") {
		t.Fatalf("expected not found, got %v", err)
	}
	u, _, _ = newTestUpdater(t, srv, "", "v0.2.0")
	if err := u.run(context.Background(), true, "", false); err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("expected auth help, got %v", err)
	}
}

// A public repository answers anonymously; a missing tag there is "not
// found", not a credentials problem.
func TestUpdateAnonymousMissingTagOnPublicRepo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"full_name":"o/r"}`)) })
	mux.HandleFunc("/repos/o/r/releases/tags/", http.NotFound)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u, _, _ := newTestUpdater(t, srv, "", "v0.2.0")
	err := u.run(context.Background(), false, "v9.9.9", false)
	if err == nil || !strings.Contains(err.Error(), "v9.9.9 not found") || strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("expected plain not found, got %v", err)
	}
}

func TestReplaceExecutableWithLockedOld(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cc-remote.exe")
	newBin := filepath.Join(dir, "new.exe")
	os.WriteFile(target, []byte("OLD"), 0o755)
	os.WriteFile(newBin, []byte("NEW"), 0o755)
	// A directory named .old cannot be removed with os.Remove when non-empty,
	// standing in for a stale copy that is still locked.
	os.MkdirAll(filepath.Join(target+".old", "x"), 0o755)
	if err := replaceExecutable(target, newBin, "windows"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "NEW" {
		t.Fatalf("target = %q", b)
	}
	if aside, _ := filepath.Glob(target + ".old-*"); len(aside) != 1 {
		t.Fatalf("expected one .old-<time>, got %v", aside)
	}
}

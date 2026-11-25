package server

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runProjectGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = cwd, projectGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestProjectGitChangesAndDiff(t *testing.T) {
	_, ts, st := setup(t, nil)
	root := t.TempDir()
	runProjectGit(t, root, "init", "--quiet")
	runProjectGit(t, root, "config", "core.autocrlf", "false")
	cwd := filepath.Join(root, "workspace with space")
	writeFile(t, filepath.Join(cwd, "-literal [name].txt"), "before\n")
	writeFile(t, filepath.Join(cwd, "old name.txt"), "rename me\n")
	writeFile(t, filepath.Join(cwd, "gone.txt"), "removed\n")
	writeFile(t, filepath.Join(root, "outside.txt"), "outside\n")
	runProjectGit(t, root, "add", ".")
	runProjectGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "base")
	writeFile(t, filepath.Join(cwd, "-literal [name].txt"), "staged version\n")
	runProjectGit(t, cwd, "--literal-pathspecs", "add", "--", "-literal [name].txt")
	writeFile(t, filepath.Join(cwd, "-literal [name].txt"), "working version\n")
	runProjectGit(t, cwd, "mv", "old name.txt", "new name.txt")
	if err := os.Remove(filepath.Join(cwd, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cwd, "新文件 #1.txt"), "line one\r\nline two")
	writeFile(t, filepath.Join(cwd, "data.bin"), "\x00\x01\xff")
	writeFile(t, filepath.Join(root, "outside.txt"), "outside changes\n")

	// The service's environment may contain another checkout's Git overrides.
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "bogus"))
	sess, err := st.Create("Git workspace", cwd, "", false)
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/sessions/" + sess.ID + "/project-git"
	status, body := request(t, ts, "GET", base, nil, testToken)
	var listing projectGitListing
	if status != 200 || json.Unmarshal(body, &listing) != nil || !listing.Repository || listing.Truncated {
		t.Fatalf("list: %d %s", status, body)
	}
	wants := []projectGitChange{
		{Path: "-literal [name].txt", Stage: "staged", Status: "modified"},
		{Path: "-literal [name].txt", Stage: "working", Status: "modified"},
		{Path: "new name.txt", OriginalPath: "old name.txt", Stage: "staged", Status: "renamed"},
		{Path: "gone.txt", Stage: "working", Status: "deleted"},
		{Path: "新文件 #1.txt", Stage: "untracked", Status: "untracked"},
		{Path: "data.bin", Stage: "untracked", Status: "untracked"},
	}
	if len(listing.Entries) != len(wants) {
		t.Fatalf("list length: %+v", listing)
	}
	for _, want := range wants {
		if !containsProjectGitChange(listing.Entries, want) {
			t.Errorf("missing change %+v in %+v", want, listing.Entries)
		}
	}

	for _, c := range []struct {
		path, stage, want string
		code              int
	}{
		{"-literal [name].txt", "staged", "-before\n+staged version", 200},
		{"-literal [name].txt", "working", "-staged version\n+working version", 200},
		{"new name.txt", "staged", "rename from old name.txt\nrename to new name.txt", 200},
		{"gone.txt", "working", "-removed", 200},
		{"新文件 #1.txt", "untracked", "+line one\n+line two\n\\ No newline at end of file", 200},
		{"data.bin", "untracked", "Binary files /dev/null and b/data.bin differ", 200},
		{"../outside.txt", "working", "", 400},
		{"outside.txt", "working", "", 404},
		{"gone.txt", "cached", "", 400},
		{".", "working", "", 400},
	} {
		q := url.Values{"path": {c.path}, "stage": {c.stage}}
		code, body := request(t, ts, "GET", base+"/diff?"+q.Encode(), nil, testToken)
		if code != c.code {
			t.Errorf("diff %q/%s: %d %s; want %d", c.path, c.stage, code, body, c.code)
			continue
		}
		if c.code == 200 {
			var patch projectGitPatch
			if json.Unmarshal(body, &patch) != nil || patch.Truncated || !strings.Contains(patch.Text, c.want) {
				t.Errorf("diff %q/%s: %s; want %q", c.path, c.stage, body, c.want)
			}
		}
	}
	if status, _ := request(t, ts, "GET", base, nil, ""); status != 401 {
		t.Fatalf("anonymous list: %d", status)
	}
}

func containsProjectGitChange(entries []projectGitChange, want projectGitChange) bool {
	for _, entry := range entries {
		if entry == want {
			return true
		}
	}
	return false
}

func TestProjectGitDisableContentFilters(t *testing.T) {
	cwd := t.TempDir()
	runProjectGit(t, cwd, "init", "--quiet")
	writeFile(t, filepath.Join(cwd, "file.txt"), "base\n")
	runProjectGit(t, cwd, "add", ".")
	writeFile(t, filepath.Join(cwd, ".gitattributes"), "file.txt filter=check diff=check\n")
	writeFile(t, filepath.Join(cwd, "file.txt"), "modified by test\n")
	runProjectGit(t, cwd, "config", "filter.check.clean", "unavailable-test-filter")
	runProjectGit(t, cwd, "config", "filter.check.process", "unavailable-test-filter")
	runProjectGit(t, cwd, "config", "filter.check.required", "true")
	runProjectGit(t, cwd, "config", "diff.check.external", "unavailable-test-filter")
	runProjectGit(t, cwd, "config", "diff.check.textconv", "unavailable-test-filter")
	worktree, err := newProjectWorktree(t.Context(), cwd)
	if err != nil || worktree == nil {
		t.Fatalf("worktree: %v %v", worktree, err)
	}
	listing, err := worktree.changes(t.Context())
	if err != nil || !containsProjectGitChange(listing.Entries, projectGitChange{Path: "file.txt", Stage: "working", Status: "modified"}) {
		t.Fatalf("status with filters: %+v %v", listing, err)
	}
	patch, err := worktree.patch(t.Context(), projectGitChange{Path: "file.txt", Stage: "working", Status: "modified"})
	if err != nil || !strings.Contains(patch.Text, "+modified by test") {
		t.Fatalf("diff with filters: %+v %v", patch, err)
	}
}

func TestProjectGitNonRepositoryAndPatchLimit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	cwd := t.TempDir()
	worktree, err := newProjectWorktree(t.Context(), cwd)
	if err != nil || worktree != nil {
		t.Fatalf("non-repository: %v %v", worktree, err)
	}
	writeFile(t, filepath.Join(cwd, "large.txt"), strings.Repeat("large content\n", maxProjectGitDiff/10))
	patch, err := untrackedProjectPatch(cwd, "large.txt")
	if err != nil || !patch.Truncated || len(patch.Text) > maxProjectGitDiff || !strings.Contains(patch.Text, "+large content") {
		t.Fatalf("large patch: %d bytes, clipped=%v, err=%v", len(patch.Text), patch.Truncated, err)
	}
}

func TestProjectGitWorkingPaths(t *testing.T) {
	cwd, outside := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(cwd, "nested", "file.txt"), "project")
	writeFile(t, filepath.Join(outside, "file.txt"), "outside")
	if err := verifyGitWorkingPath(cwd, "nested/file.txt"); err != nil {
		t.Fatalf("regular file: %v", err)
	}
	if err := verifyGitWorkingPath(cwd, "removed/subdir/file.txt"); err != nil {
		t.Fatalf("removed directory: %v", err)
	}
	projectTestLink(t, filepath.Join(cwd, "linked"), outside)
	if err := verifyGitWorkingPath(cwd, "linked/file.txt"); err == nil {
		t.Fatal("accepted a file with a linked parent")
	}
}

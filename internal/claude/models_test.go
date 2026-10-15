package claude

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRecentModelsReadsAssistantModelsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "projects", "p")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(id, body string, age time.Duration) {
		path := filepath.Join(project, id+".jsonl")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		mod := time.Now().Add(-age)
		_ = os.Chtimes(path, mod, mod)
	}
	write("aaaaaaaa-1234-1234-1234-123456789abc", `{"type":"assistant","message":{"model":"claude-old-1"}}`+"\n", time.Hour)
	padding := strings.Repeat(`{"type":"user","message":{"content":"x"}}`+"\n", 5000)
	write("bbbbbbbb-1234-1234-1234-123456789abc", `{"type":"assistant","message":{"model":"claude-head-only"}}`+"\n"+padding+
		`{"type":"assistant","message":{"model":"claude-sonnet-4-5"}}`+"\n"+
		`{"type":"assistant","message":{"model":"<synthetic>"}}`+"\n"+
		`{"type":"user","message":{"model":"claude-user-ignored"}}`+"\n"+
		`{"type":"assistant","message":{"model":"claude-opus-4-7"}}`+"\n", time.Minute)
	write("not-a-session", `{"type":"assistant","message":{"model":"claude-ignored"}}`, 0)
	got := RecentModels(dir, 200)
	if !slices.Equal(got, []string{"claude-opus-4-7", "claude-sonnet-4-5", "claude-old-1"}) {
		t.Fatalf("models %v", got)
	}
	if got = RecentModels(dir, 1); !slices.Equal(got, []string{"claude-opus-4-7", "claude-sonnet-4-5"}) {
		t.Fatalf("file limit %v", got)
	}
	// Within the TTL the directory is not walked again.
	write("cccccccc-1234-1234-1234-123456789abc", `{"type":"assistant","message":{"model":"claude-new"}}`+"\n", 0)
	if got = RecentModels(dir, 200); slices.Contains(got, "claude-new") {
		t.Fatalf("cache not used %v", got)
	}
}

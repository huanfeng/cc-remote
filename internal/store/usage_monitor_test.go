package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestMonitorRetentionExpiresArchivedSamplesWithoutTouchingLegacy(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	old := now.Add(-31 * 24 * time.Hour)
	for _, source := range []string{"monitor:archived", "monitor-cache:archived", "oauth:legacy"} {
		if err = s.AddAccountUsageSample(source, old, json.RawMessage(`{}`), 60*24*time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.AddAccountUsageSample("monitor:current", now, json.RawMessage(`{}`), 60*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if err = s.PruneMonitorUsage(now.Add(-30 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"monitor:archived", "monitor-cache:archived", "oauth:legacy", "monitor:current"} {
		rows, err := s.AccountUsageSamples(source, old.Add(-time.Hour))
		want := 0
		if source == "oauth:legacy" || source == "monitor:current" {
			want = 1
		}
		if err != nil || len(rows) != want {
			t.Fatalf("%s: %d %v", source, len(rows), err)
		}
	}
}

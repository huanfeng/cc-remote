package claude

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"testing"
	"time"
)

func TestParseUsage(t *testing.T) {
	// The current response: limits name the windows and the model limits.
	u, err := ParseUsage([]byte(`{"five_hour":{"utilization":13.0,"resets_at":"2026-10-21T18:50:00Z"},"seven_day":{"utilization":9.0,"resets_at":"2026-10-28T10:00:00Z"},"seven_day_opus":null,
		"limits":[{"kind":"session","percent":13,"severity":"normal","resets_at":"2026-10-21T18:50:00Z","scope":null},
		{"kind":"weekly_all","percent":9,"severity":"warning","resets_at":"2026-10-28T10:00:00Z","scope":null},
		{"kind":"weekly_scoped","percent":140,"severity":"normal","resets_at":"2026-10-28T10:00:00Z","scope":{"model":{"display_name":"Fable"}}},
		{"kind":"weekly_scoped","percent":5,"scope":{"model":{"display_name":"fable"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if u.FiveHour == nil || u.FiveHour.Percent != 13 || u.FiveHour.ResetsAt != "2026-10-21T18:50:00Z" || u.Weekly == nil || u.Weekly.Severity != "warning" {
		t.Fatalf("windows %+v %+v", u.FiveHour, u.Weekly)
	}
	if len(u.Models) != 1 || u.Models[0].Name != "Fable" || u.Models[0].Percent != 100 {
		t.Fatalf("models %+v", u.Models)
	}
	// The older response has only the fixed fields.
	u, err = ParseUsage([]byte(`{"five_hour":{"utilization":42.5,"resets_at":"a"},"seven_day":{"utilization":7,"resets_at":"b"},"seven_day_opus":{"utilization":3,"resets_at":"c"}}`))
	if err != nil || u.FiveHour.Percent != 42.5 || u.Weekly.Percent != 7 || len(u.Models) != 1 || u.Models[0].Name != "Opus" {
		t.Fatalf("old shape %+v %v", u, err)
	}
	if _, err = ParseUsage([]byte(`{"five_hour":null}`)); err == nil {
		t.Fatal("a response without limits parsed")
	}
}

func TestReadOAuthToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	if _, err := ReadOAuthToken(dir); err != ErrNoOAuth && goruntime.GOOS != "darwin" {
		t.Fatalf("no file: %v", err)
	}
	expires := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	_ = os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat-x","refreshToken":"r","expiresAt":`+strconv.FormatInt(expires.UnixMilli(), 10)+`,"subscriptionType":"max"}}`), 0600)
	tok, err := ReadOAuthToken(dir)
	if err != nil || tok.AccessToken != "sk-ant-oat-x" || !tok.ExpiresAt.Equal(expires) || tok.Plan != "max" {
		t.Fatalf("file: %+v %v", tok, err)
	}
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "env-token")
	if tok, err = ReadOAuthToken(dir); err != nil || tok.AccessToken != "env-token" || !tok.ExpiresAt.IsZero() {
		t.Fatalf("env: %+v %v", tok, err)
	}
}

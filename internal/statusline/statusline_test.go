package statusline

import (
	"strings"
	"testing"
)

func TestMissingAndZero(t *testing.T) {
	for _, s := range []string{"compact", "color", "multiline"} {
		if got := Render(strings.NewReader(`{}`), s, ""); got != "" {
			t.Fatal(got)
		}
		got := Render(strings.NewReader(`{"model":{"display_name":"Opus"},"effort":{"level":"high"},"context_window":{"used_percentage":null},"rate_limits":{"five_hour":{"used_percentage":0}}}`), s, "")
		if !strings.Contains(got, "5h: 0%") || strings.Contains(got, "7d") || strings.Contains(got, "Context") || !strings.Contains(got, "Opus") {
			t.Fatal(got)
		}
	}
}
func TestRenderStylesAndSanitize(t *testing.T) {
	body := `{"model":{"id":"test\u001b[31m\n"},"cost":{"total_cost_usd":0.125},"rate_limits":{"seven_day":{"used_percentage":42}}}`
	got := Render(strings.NewReader(body), "multiline", "")
	if strings.Contains(got, "\x1b") || !strings.Contains(got, "\nCost: $0.1250") || !strings.Contains(got, "7d: 42%") {
		t.Fatal(got)
	}
	if !strings.Contains(Render(strings.NewReader(body), "color", ""), "\x1b[38;2;167;139;250m") {
		t.Fatal("no color")
	}
	if Render(strings.NewReader(`broken`), "compact", "") != "" {
		t.Fatal("invalid JSON")
	}
}

func TestProviderOverride(t *testing.T) {
	t.Setenv("CC_REMOTE_PROVIDER_NAME", "My supplier")
	t.Setenv("ANTHROPIC_BASE_URL", "https://user:secret@example.com/key")
	if Provider() != "My supplier" {
		t.Fatal(Provider())
	}
	t.Setenv("CC_REMOTE_PROVIDER_NAME", "")
	if Provider() != "example.com" {
		t.Fatal(Provider())
	}
}

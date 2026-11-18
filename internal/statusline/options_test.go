package statusline

import (
	"strings"
	"testing"
)

func TestCustomOptions(t *testing.T) {
	o := Options{Segments: []string{"model", "context"}, Bars: true, Warning: 70, Critical: 90, Colors: map[string]string{"warning": "#ffaa00"}, Separator: " / "}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	got := RenderOptions(strings.NewReader(`{"model":{"display_name":"Opus"},"context_window":{"used_percentage":80}}`), "color", "secret-provider", &o)
	if strings.Contains(got, "secret-provider") || !strings.Contains(got, "38;2;255;170;0") || !strings.Contains(got, "80% [") || !strings.Contains(got, " / ") {
		t.Fatal(got)
	}
	o.Segments = []string{"bad"}
	if o.Validate() == nil {
		t.Fatal("invalid segment accepted")
	}
	o.Segments = []string{}
	if RenderOptions(strings.NewReader(`{}`), "compact", "provider", &o) != "" {
		t.Fatal("empty segments")
	}
}

func TestOptionsEncodingAndValidation(t *testing.T) {
	o := Options{Segments: []string{"provider", "context"}, Warning: 75, Critical: 90, Separator: " | ", Colors: map[string]string{"provider": "#00ff00"}}
	decoded := Decode(Encode(&o))
	if decoded == nil || decoded.Separator != o.Separator {
		t.Fatal(decoded)
	}
	for _, s := range []string{"bad", "", "e30"} {
		if Decode(s) != nil {
			t.Fatal(s)
		}
	}
	o.Colors["provider"] = "\x1b[31m"
	if o.Validate() == nil {
		t.Fatal("control color accepted")
	}
	o.Colors = nil
	o.Separator = "\n"
	if o.Validate() == nil {
		t.Fatal("control separator accepted")
	}
}

func TestShortLabelsAndRoundTrip(t *testing.T) {
	o := Options{Segments: Segments, ShortLabels: true, Warning: 75, Critical: 90, Separator: " | "}
	decoded := Decode(Encode(&o))
	if decoded == nil || !decoded.ShortLabels {
		t.Fatal("short label option lost")
	}
	got := RenderOptions(strings.NewReader(`{"model":{"display_name":"Opus"},"effort":{"level":"high"},"context_window":{"used_percentage":0,"total_input_tokens":0,"total_output_tokens":2},"cost":{"total_cost_usd":0,"total_duration_ms":60000},"workspace":{"current_dir":"project"},"rate_limits":{"five_hour":{"used_percentage":0},"seven_day":{"used_percentage":24}}}`), "compact", "Anthropic", decoded)
	want := "P: Anthropic | M: Opus | E: high | C: 0% | I: 0 tokens | O: 2 tokens | $0.0000 | 5h: 0% | 7d: 24% | D: project | T: 1m"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := RenderOptions(strings.NewReader(`{}`), "compact", "", decoded); got != "" {
		t.Fatalf("absent values: %q", got)
	}
	o.ShortLabels = false
	got = RenderOptions(strings.NewReader(`{"model":{"id":"opus"}}`), "compact", "Anthropic", &o)
	if got != "Provider: Anthropic | Model: opus" {
		t.Fatal(got)
	}
}

func TestDistinctDefaultColorsAndOverrides(t *testing.T) {
	data := `{"model":{"id":"opus"},"context_window":{"used_percentage":20}}`
	for _, o := range []*Options{nil, {Segments: []string{"provider", "model", "context"}, Warning: 75, Critical: 90}} {
		got := RenderOptions(strings.NewReader(data), "color", "Anthropic", o)
		for _, want := range []string{tint("Provider: Anthropic", DefaultColors["provider"]), tint("Model: opus", DefaultColors["model"]), tint("Context: 20%", DefaultColors["context"])} {
			if !strings.Contains(got, want) {
				t.Fatalf("default palette missing %q in %q", want, got)
			}
		}
	}
	o := Options{Segments: []string{"provider", "model"}, Warning: 75, Critical: 90, Colors: map[string]string{"normal": "#112233", "model": "#445566"}}
	got := RenderOptions(strings.NewReader(data), "color", "Anthropic", &o)
	if !strings.Contains(got, tint("Provider: Anthropic", "#112233")) || !strings.Contains(got, tint("Model: opus", "#445566")) {
		t.Fatal(got)
	}
}

func TestOrderRoundTripAndValidation(t *testing.T) {
	o := Options{Segments: []string{"model", "provider"}, Order: append([]string{}, Segments...), Warning: 75, Critical: 90}
	got := Decode(Encode(&o))
	if got == nil || len(got.Order) != len(Segments) || got.Segments[0] != "model" {
		t.Fatal("order not preserved")
	}
	o.Order = []string{"model"}
	if o.Validate() == nil {
		t.Fatal("partial order accepted")
	}
	o.Order = append([]string{}, Segments...)
	o.Order[1] = o.Order[0]
	if o.Validate() == nil {
		t.Fatal("duplicate order accepted")
	}
}

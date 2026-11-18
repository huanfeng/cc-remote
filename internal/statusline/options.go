package statusline

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var Segments = []string{"provider", "model", "effort", "context", "input", "output", "cost", "fiveHour", "sevenDay", "directory", "duration"}

type Options struct {
	Order       []string          `json:"order,omitempty"`
	ShortLabels bool              `json:"shortLabels,omitempty"`
	Segments    []string          `json:"segments"`
	Colors      map[string]string `json:"colors,omitempty"`
	Bars        bool              `json:"bars"`
	Warning     float64           `json:"warning"`
	Critical    float64           `json:"critical"`
	Separator   string            `json:"separator"`
}

var DefaultColors = map[string]string{
	"provider": "#22d3ee", "model": "#a78bfa", "effort": "#f472b6",
	"context": "#4ade80", "input": "#38bdf8", "output": "#2dd4bf",
	"cost": "#fbbf24", "fiveHour": "#60a5fa", "sevenDay": "#c084fc",
	"directory": "#94a3b8", "duration": "#cbd5e1",
}

func (o Options) Validate() error {
	if o.Warning < 0 || o.Critical > 100 || o.Warning >= o.Critical {
		return errors.New("invalid warning/critical thresholds")
	}
	if len(o.Separator) > 20 || clean(o.Separator) != o.Separator {
		return errors.New("invalid separator")
	}
	seen := map[string]bool{}
	for _, s := range o.Segments {
		if !contains(Segments, s) || seen[s] {
			return errors.New("invalid or duplicate segment")
		}
		seen[s] = true
	}
	if len(o.Order) > 0 {
		seenOrder := map[string]bool{}
		for _, key := range o.Order {
			if !contains(Segments, key) || seenOrder[key] {
				return errors.New("invalid or duplicate order segment")
			}
			seenOrder[key] = true
		}
		if len(o.Order) != len(Segments) {
			return errors.New("order must include all segments")
		}
	}
	for k, v := range o.Colors {
		if !contains(append(append([]string{}, Segments...), "normal", "warning", "critical"), k) || !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(v) {
			return errors.New("invalid color")
		}
	}
	return nil
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func Encode(o *Options) string {
	b, _ := json.Marshal(o)
	return base64.RawURLEncoding.EncodeToString(b)
}
func Decode(s string) *Options {
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil {
		return nil
	}
	var o Options
	if json.Unmarshal(b, &o) != nil || o.Validate() != nil {
		return nil
	}
	return &o
}
func tint(s, hex string) string {
	var r, g, b int
	if _, e := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); e != nil {
		return s
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, s)
}
func bar(n float64) string {
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	filled := int(n / 10)
	return " [" + strings.Repeat("█", filled) + strings.Repeat("░", 10-filled) + "]"
}

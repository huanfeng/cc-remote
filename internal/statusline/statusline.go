package statusline

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func ValidStyle(s string) bool { return s == "compact" || s == "color" || s == "multiline" }
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == 0x1b {
			return -1
		}
		return r
	}, s)
}

// Render only supplied values. A real zero is distinct from an absent value.
func Render(r io.Reader, style string, provider string) string {
	return RenderOptions(r, style, provider, nil)
}
func RenderOptions(r io.Reader, style string, provider string, options *Options) string {
	var v map[string]any
	if json.NewDecoder(io.LimitReader(r, 2<<20)).Decode(&v) != nil {
		return ""
	}
	get := func(path ...string) any {
		var x any = v
		for _, k := range path {
			m, ok := x.(map[string]any)
			if !ok {
				return nil
			}
			x = m[k]
		}
		return x
	}
	var parts []string
	values := map[string]string{}
	percentages := map[string]float64{}
	add := func(key, label string, x any, suffix string) {
		switch n := x.(type) {
		case string:
			if n != "" {
				values[key] = label + clean(n) + suffix
			}
		case float64:
			values[key] = label + fmt.Sprintf("%.4g", n) + suffix
			if suffix == "%" {
				percentages[key] = n
			}
		}
	}
	add("provider", "Provider: ", provider, "")
	model := get("model", "display_name")
	if model == nil || model == "" {
		model = get("model", "id")
	}
	add("model", "Model: ", model, "")
	add("effort", "Effort: ", get("effort", "level"), "")
	add("context", "Context: ", get("context_window", "used_percentage"), "%")
	add("input", "Input: ", get("context_window", "total_input_tokens"), " tokens")
	add("output", "Output: ", get("context_window", "total_output_tokens"), " tokens")
	if n, ok := get("cost", "total_cost_usd").(float64); ok {
		values["cost"] = fmt.Sprintf("Cost: $%.4f", n)
	}
	add("fiveHour", "5h: ", get("rate_limits", "five_hour", "used_percentage"), "%")
	add("sevenDay", "7d: ", get("rate_limits", "seven_day", "used_percentage"), "%")
	if dir, ok := get("workspace", "current_dir").(string); ok && dir != "" {
		values["directory"] = "Dir: " + clean(filepath.Base(dir))
	}
	if n, ok := get("cost", "total_duration_ms").(float64); ok {
		values["duration"] = fmt.Sprintf("Time: %.0fm", n/60000)
	}
	if options != nil && options.ShortLabels {
		for key, label := range map[string]string{"provider": "P", "model": "M", "effort": "E", "context": "C", "input": "I", "output": "O", "directory": "D", "duration": "T"} {
			if value := values[key]; value != "" {
				_, rest, _ := strings.Cut(value, ": ")
				values[key] = label + ": " + rest
			}
		}
		values["cost"] = strings.TrimPrefix(values["cost"], "Cost: ")
	}
	keys := Segments[:9]
	separator := " | "
	if options != nil {
		keys = options.Segments
		separator = options.Separator
	}
	split := 0
	for _, k := range keys {
		p := values[k]
		if p == "" {
			continue
		}
		if k == "provider" || k == "model" || k == "effort" {
			split++
		}
		if n, ok := percentages[k]; ok && options != nil && options.Bars {
			p += bar(n)
		}
		if style == "color" {
			color := DefaultColors[k]
			if options != nil {
				if custom := options.Colors[k]; custom != "" {
					color = custom
				} else if normal := options.Colors["normal"]; normal != "" {
					color = normal
				}
				if n, ok := percentages[k]; ok {
					if n >= options.Critical {
						color = options.Colors["critical"]
						if color == "" {
							color = "#ef4444"
						}
					} else if n >= options.Warning {
						color = options.Colors["warning"]
						if color == "" {
							color = "#f59e0b"
						}
					}
				}
			}
			p = tint(p, color)
		}
		parts = append(parts, p)
	}
	if style == "multiline" && split > 0 && split < len(parts) {
		return strings.Join(parts[:split], separator) + "\n" + strings.Join(parts[split:], separator)
	}
	return strings.Join(parts, separator)
}

func Provider() string {
	if name := os.Getenv("CC_REMOTE_PROVIDER_NAME"); name != "" {
		return clean(name)
	}
	for _, k := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		if os.Getenv(k) == "1" || os.Getenv(k) == "true" {
			return strings.TrimPrefix(k, "CLAUDE_CODE_USE_")
		}
	}
	if raw := os.Getenv("ANTHROPIC_BASE_URL"); raw != "" {
		if u, e := url.Parse(raw); e == nil {
			return u.Hostname()
		}
		return ""
	}
	return "Anthropic"
}

package server

import (
	"errors"
	"strconv"
	"strings"
)

// Only connection/model scalars are projected. Other tables (hooks, MCP,
// permissions, projects) are skipped rather than imported into the host.
func codexConnectionConfig(settings map[string]any) (map[string]any, error) {
	if normalized, ok := settings["configOverrides"].(map[string]any); ok {
		return normalized, nil
	}
	raw, _ := settings["config"].(string)
	return readConnectionTOML(raw)
}
func codexRootKey(k string) bool {
	switch k {
	case "model", "model_provider", "model_reasoning_effort", "model_reasoning_summary", "model_verbosity", "model_context_window", "model_auto_compact_token_limit", "disable_response_storage":
		return true
	}
	return false
}
func readConnectionTOML(raw string) (map[string]any, error) {
	records, err := tomlRecords(raw)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	var section []string
	for _, record := range records {
		line := strings.TrimSpace(record)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			array := strings.HasPrefix(line, "[[")
			closing := "]"
			opening := 1
			if array {
				closing = "]]"
				opening = 2
			}
			if !strings.HasSuffix(line, closing) {
				return nil, errors.New("Codex TOML 表定义无效")
			}
			section, err = tomlKeyPath(strings.TrimSpace(line[opening : len(line)-len(closing)]))
			if err != nil {
				return nil, err
			}
			if array && len(section) > 0 && section[0] == "model_providers" {
				return nil, errors.New("Codex 连接不能使用表数组")
			}
			if array {
				section = []string{"@ignored-array"}
			}
			continue
		}
		at := tomlEquals(line)
		if at < 0 {
			return nil, errors.New("Codex TOML 赋值无效")
		}
		keys, e := tomlKeyPath(strings.TrimSpace(line[:at]))
		if e != nil {
			return nil, e
		}
		path := append(append([]string{}, section...), keys...)
		if !(len(path) == 1 && codexRootKey(path[0]) || len(path) > 1 && path[0] == "model_providers") {
			continue
		}
		value, e := tomlScalar(strings.TrimSpace(line[at+1:]))
		if e != nil {
			return nil, e
		}
		m := result
		for _, key := range path[:len(path)-1] {
			if m[key] == nil {
				m[key] = map[string]any{}
			}
			sub, ok := m[key].(map[string]any)
			if !ok {
				return nil, errors.New("Codex TOML 连接路径冲突")
			}
			m = sub
		}
		key := path[len(path)-1]
		if _, exists := m[key]; exists {
			return nil, errors.New("Codex TOML 连接字段重复")
		}
		m[key] = value
	}
	return result, nil
}
func tomlScalar(raw string) (any, error) {
	if strings.HasPrefix(raw, "\"\"\"") || strings.HasPrefix(raw, "'''") {
		return nil, errors.New("Codex 连接字段不支持多行字符串")
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1], nil
	}
	if strings.HasPrefix(raw, "\"") {
		v, e := strconv.Unquote(raw)
		if e == nil {
			return v, nil
		}
		return nil, errors.New("Codex 连接字符串无效")
	}
	if raw == "true" {
		return true, nil
	}
	if raw == "false" {
		return false, nil
	}
	if n, e := strconv.ParseInt(strings.ReplaceAll(raw, "_", ""), 10, 64); e == nil {
		return float64(n), nil
	}
	return nil, errors.New("Codex 连接字段仅支持字符串、布尔值或整数；请使用标准 TOML 表定义")
}
func tomlEquals(s string) int {
	var quote rune
	escaped := false
	for i, c := range s {
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' && quote == '"' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
		} else if c == '=' {
			return i
		}
	}
	return -1
}
func tomlKeyPath(s string) ([]string, error) {
	var parts []string
	start := 0
	var quote byte
	escaped := false
	for i := 0; i <= len(s); i++ {
		if i < len(s) && quote != 0 {
			c := s[i]
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' && quote == '"' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if i < len(s) && (s[i] == '"' || s[i] == '\'') {
			quote = s[i]
			continue
		}
		if i == len(s) || s[i] == '.' {
			token := strings.TrimSpace(s[start:i])
			if token == "" {
				return nil, errors.New("Codex TOML 键无效")
			}
			if token[0] == '"' || token[0] == '\'' {
				v, e := tomlScalar(token)
				if e != nil {
					return nil, e
				}
				text, ok := v.(string)
				if !ok {
					return nil, errors.New("Codex TOML 键无效")
				}
				parts = append(parts, text)
			} else {
				for _, c := range token {
					if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
						return nil, errors.New("Codex TOML 键无效")
					}
				}
				parts = append(parts, token)
			}
			start = i + 1
		}
	}
	if quote != 0 {
		return nil, errors.New("Codex TOML 键引号未闭合")
	}
	return parts, nil
}

// Group complete TOML records, including ignored multiline strings/arrays, so
// text inside a prompt or MCP setting cannot be interpreted as a provider table.
func tomlRecords(raw string) ([]string, error) {
	var out []string
	var record strings.Builder
	var quote byte
	triple := false
	escaped := false
	comment := false
	depth := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if comment {
			if c != '\n' {
				continue
			}
			comment = false
		}
		if quote != 0 {
			record.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' && quote == '"' {
				escaped = true
				continue
			}
			if c == quote {
				if triple {
					if i+2 < len(raw) && raw[i+1] == quote && raw[i+2] == quote {
						record.WriteString(raw[i+1 : i+3])
						i += 2
						quote = 0
						triple = false
					}
				} else {
					quote = 0
				}
			}
			continue
		}
		if c == '#' {
			comment = true
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			record.WriteByte(c)
			if i+2 < len(raw) && raw[i+1] == c && raw[i+2] == c {
				triple = true
				record.WriteString(raw[i+1 : i+3])
				i += 2
			}
			continue
		}
		if c == '[' || c == '{' {
			depth++
		}
		if c == ']' || c == '}' {
			depth--
			if depth < 0 {
				return nil, errors.New("Codex TOML 括号无效")
			}
		}
		if c == '\n' && depth == 0 {
			out = append(out, record.String())
			record.Reset()
			continue
		}
		if c != '\r' {
			record.WriteByte(c)
		}
	}
	if quote != 0 || depth != 0 {
		return nil, errors.New("Codex TOML 字符串或括号未闭合")
	}
	if strings.TrimSpace(record.String()) != "" {
		out = append(out, record.String())
	}
	return out, nil
}

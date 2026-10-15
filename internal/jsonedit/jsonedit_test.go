package jsonedit

import (
	"encoding/json"
	"strings"
	"testing"
)

const typical = `{
  "env": {
    "ANTHROPIC_BASE_URL": "https://example.invalid",
    "DISABLE_TELEMETRY": "1"
  },
  "permissions": {
    "allow": [
      "Bash(git status)",
      "Read(**)"
    ],
    "defaultMode": "acceptEdits"
  },
  "model": "sonnet",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "echo \"done}\""}]}]
  },
  "zeta": true
}
`

func str(s string) json.RawMessage { b, _ := json.Marshal(s); return b }

func diffLines(t *testing.T, a, b string) []string {
	t.Helper()
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(x) != len(y) {
		t.Fatalf("line count changed:\n%s\n---\n%s", a, b)
	}
	var out []string
	for i := range x {
		if x[i] != y[i] {
			out = append(out, y[i])
		}
	}
	return out
}

func TestChangeOnlyTouchesOneLine(t *testing.T) {
	for _, tc := range []struct{ name, indent, nl string }{{"two", "  ", "\n"}, {"four", "    ", "\n"}, {"tab", "\t", "\n"}, {"crlf", "  ", "\r\n"}} {
		doc := strings.ReplaceAll(strings.ReplaceAll(typical, "  ", tc.indent), "\n", tc.nl)
		out, err := Set([]byte(doc), []string{"model"}, str("opus"))
		if err != nil {
			t.Fatal(err)
		}
		changed := diffLines(t, doc, string(out))
		if len(changed) != 1 || strings.TrimSpace(changed[0]) != `"model": "opus",` {
			t.Fatalf("%s: %q", tc.name, changed)
		}
		out, err = Set([]byte(doc), []string{"permissions", "defaultMode"}, str("bypassPermissions"))
		if err != nil {
			t.Fatal(err)
		}
		changed = diffLines(t, doc, string(out))
		if len(changed) != 1 || strings.TrimSpace(changed[0]) != `"defaultMode": "bypassPermissions"` {
			t.Fatalf("%s nested: %q", tc.name, changed)
		}
		if tc.nl == "\r\n" && strings.Count(string(out), "\r\n") != strings.Count(string(out), "\n") {
			t.Fatal("mixed newlines")
		}
	}
}

func TestInsertAndRemoveKeepFormatting(t *testing.T) {
	doc := strings.ReplaceAll(typical, "  ", "\t")
	doc = strings.ReplaceAll(doc, "\n", "\r\n")
	out, err := Set([]byte(doc), []string{"effortLevel"}, str("high"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(doc, "\t\"zeta\": true\r\n}", "\t\"zeta\": true,\r\n\t\"effortLevel\": \"high\"\r\n}", 1)
	if string(out) != want {
		t.Fatalf("insert:\n%q\nwant\n%q", out, want)
	}
	back, err := Set(out, []string{"effortLevel"}, nil)
	if err != nil || string(back) != doc {
		t.Fatalf("remove did not restore original: %v\n%q", err, back)
	}
	// Removing the only nested key drops the now-empty parent.
	small := "{\n    \"permissions\": {\n        \"defaultMode\": \"plan\"\n    },\n    \"model\": \"x\"\n}\n"
	out, err = Set([]byte(small), []string{"permissions", "defaultMode"}, nil)
	if err != nil || string(out) != "{\n    \"model\": \"x\"\n}\n" {
		t.Fatalf("remove parent: %v %q", err, out)
	}
	// Creating a parent uses the detected indent.
	out, err = Set(out, []string{"permissions", "defaultMode"}, str("plan"))
	if err != nil || string(out) != "{\n    \"model\": \"x\",\n    \"permissions\": {\n        \"defaultMode\": \"plan\"\n    }\n}\n" {
		t.Fatalf("create parent: %v %q", err, out)
	}
	// Removing a middle key keeps neighbours intact.
	out, err = Set([]byte(typical), []string{"model"}, nil)
	if err != nil || strings.Contains(string(out), "model") || !json.Valid(out) || !strings.Contains(string(out), "  },\n  \"hooks\"") {
		t.Fatalf("remove middle: %v\n%s", err, out)
	}
}

func TestDuplicateMembers(t *testing.T) {
	doc := "{\n  \"model\": \"a\",\n  \"x\": 1,\n  \"model\": \"b\",\n  \"model\": \"c\"\n}\n"
	out, err := Set([]byte(doc), []string{"model"}, str("opus"))
	if err != nil || string(out) != "{\n  \"x\": 1,\n  \"model\": \"opus\"\n}\n" {
		t.Fatalf("set: %v %q", err, out)
	}
	out, err = Set([]byte(doc), []string{"model"}, nil)
	if err != nil || string(out) != "{\n  \"x\": 1\n}\n" {
		t.Fatalf("remove: %v %q", err, out)
	}
	nested := `{"permissions":{"defaultMode":"plan","defaultMode":"auto"},"permissions":{"allow":[],"defaultMode":"acceptEdits"}}`
	out, err = Set([]byte(nested), []string{"permissions", "defaultMode"}, str("default"))
	if err != nil || string(out) != `{"permissions":{"allow":[],"defaultMode":"default"}}` {
		t.Fatalf("nested set: %v %s", err, out)
	}
	out, err = Set([]byte(nested), []string{"permissions", "defaultMode"}, nil)
	if err != nil || string(out) != `{"permissions":{"allow":[]}}` {
		t.Fatalf("nested remove: %v %s", err, out)
	}
	var v map[string]any
	if json.Unmarshal(out, &v) != nil {
		t.Fatal("invalid output")
	}
}

func TestBOMPreserved(t *testing.T) {
	doc := "\xEF\xBB\xBF{\n  \"model\": \"sonnet\"\n}\n"
	out, err := Set([]byte(doc), []string{"model"}, str("opus"))
	if err != nil || string(out) != "\xEF\xBB\xBF{\n  \"model\": \"opus\"\n}\n" {
		t.Fatalf("set: %v %q", err, out)
	}
	out, err = Set([]byte("\xEF\xBB\xBF"), []string{"model"}, str("opus"))
	if err != nil || string(out) != "\xEF\xBB\xBF{\n  \"model\": \"opus\"\n}\n" {
		t.Fatalf("bom only: %v %q", err, out)
	}
	if string(StripBOM(out)) != string(out[3:]) {
		t.Fatal("StripBOM")
	}
}

func TestCompactAndEmptyDocuments(t *testing.T) {
	out, err := Set([]byte(`{"a":1,"b":{"c":"}"}}`), []string{"model"}, str("opus"))
	if err != nil || string(out) != `{"a":1,"b":{"c":"}"},"model":"opus"}` {
		t.Fatalf("compact: %v %s", err, out)
	}
	out, err = Set(nil, []string{"permissions", "defaultMode"}, str("plan"))
	if err != nil || string(out) != "{\n  \"permissions\": {\n    \"defaultMode\": \"plan\"\n  }\n}\n" {
		t.Fatalf("empty: %v %q", err, out)
	}
	if _, err = Set([]byte(`[1]`), []string{"a"}, str("x")); err == nil {
		t.Fatal("array accepted")
	}
	if _, err = Set([]byte(`{"permissions":"x"}`), []string{"permissions", "defaultMode"}, str("x")); err == nil {
		t.Fatal("non-object parent accepted")
	}
	if _, err = Set([]byte(`{"a":`), []string{"a"}, str("x")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

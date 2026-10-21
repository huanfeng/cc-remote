package claude

import "testing"

func TestSameCall(t *testing.T) {
	q := func(text string) map[string]any {
		return map[string]any{"questions": []any{map[string]any{"question": text, "multiSelect": false}}}
	}
	for _, c := range []struct {
		name             string
		tool             string
		hook, transcript map[string]any
		want             bool
	}{
		{"same question", "AskUserQuestion", q("Pick"), map[string]any{"questions": []any{map[string]any{"question": "Pick"}}}, true},
		{"other question", "AskUserQuestion", q("Pick"), q("Other"), false},
		{"plan input differs", "ExitPlanMode", map[string]any{"plan": "p"}, map[string]any{}, true},
		{"same command", "Bash", map[string]any{"command": "ls", "timeout": 1000.0}, map[string]any{"command": "ls"}, true},
		{"other command", "Bash", map[string]any{"command": "ls"}, map[string]any{"command": "rm x"}, false},
		{"nested input", "Edit", map[string]any{"file_path": "a", "edits": []any{"x"}}, map[string]any{"file_path": "a", "edits": []any{"x"}}, true},
		{"nothing shared", "Bash", map[string]any{"command": "ls"}, map[string]any{"cmd": "ls"}, false},
		{"both empty", "Tool", map[string]any{}, map[string]any{}, true},
	} {
		if got := SameCall(c.tool, c.hook, c.transcript); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

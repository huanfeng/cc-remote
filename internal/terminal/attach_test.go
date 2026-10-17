package terminal

import (
	"reflect"
	"testing"
)

// ccr reads like claude: the resume command Claude Code prints on exit and
// Claude Code's own options work with ccr in place of claude.
func TestSplitCCR(t *testing.T) {
	for _, c := range []struct {
		args []string
		want ccrArgs
	}{
		{[]string{"--resume", "7d3ed0ff-f71c"}, ccrArgs{resume: resumeArgs{set: true, query: "7d3ed0ff-f71c"}}},
		{[]string{"-r"}, ccrArgs{resume: resumeArgs{set: true}}},
		{[]string{"--resume", "--model", "opus"}, ccrArgs{own: []string{"--model", "opus"}, resume: resumeArgs{set: true}}},
		{[]string{"--resume=abc"}, ccrArgs{resume: resumeArgs{set: true, query: "abc"}}},
		{[]string{"-c", "--effort", "high"}, ccrArgs{own: []string{"--effort", "high"}, resume: resumeArgs{set: true, last: true}}},
		{[]string{"--new", "--title", "x"}, ccrArgs{own: []string{"--new", "--title", "x"}}},
		{[]string{"abc"}, ccrArgs{words: []string{"abc"}}},
		// Claude Code options, with the values Claude Code would take, and its prompt.
		{[]string{"--host", "h", "--add-dir", "a", "b", "--chrome", "-n", "fix", "--dangerously-skip-permissions"},
			ccrArgs{own: []string{"--host", "h"}, claude: []string{"--add-dir", "a", "b", "--chrome", "-n", "fix", "--dangerously-skip-permissions"}}},
		{[]string{"--verbose", "fix the bug"}, ccrArgs{claude: []string{"--verbose"}, words: []string{"fix the bug"}}},
		// cc-remote -- claude [options]: everything after "--" is Claude Code's.
		{[]string{"--data-dir", "d", "--", "--model", "opus", "-c", "--mcp-config", "m.json"},
			ccrArgs{own: []string{"--data-dir", "d", "--model", "opus"}, resume: resumeArgs{set: true, last: true}, claude: []string{"--mcp-config", "m.json"}, launch: true}},
		{[]string{"--", "hello"}, ccrArgs{words: []string{"hello"}, launch: true}},
	} {
		if got := splitCCR(c.args); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v:\n got %+v\nwant %+v", c.args, got, c.want)
		}
	}
	if n := splitCCR([]string{"--add-dir", "a", "-n", "Login fix"}).name(); n != "Login fix" {
		t.Errorf("name %q", n)
	}
}

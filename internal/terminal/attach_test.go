package terminal

import (
	"bufio"
	"reflect"
	"strings"
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

// ccr in a directory with sessions asks which; Enter takes the most recent one
// no terminal is attached to, and attaching to a busy one needs a yes.
func TestChoose(t *testing.T) {
	list := []remoteSession{
		{ID: "11111111-a", Title: "busy", Mode: "terminal", Online: true, Status: "running", Attached: 1},
		{ID: "22222222-b", Title: "free", Mode: "terminal"},
	}
	created := false
	create := func() (remoteSession, error) { created = true; return remoteSession{ID: "new"}, nil }
	answer := func(input string) {
		stdin = bufio.NewReader(strings.NewReader(input))
	}
	answer("\n")
	if x, err := choose(list, create); err != nil || x.Title != "free" {
		t.Fatalf("default %+v %v", x, err)
	}
	answer("0\n")
	if x, _ := choose(list, create); !created || x.ID != "new" {
		t.Fatalf("new %+v", x)
	}
	// Every session attached: Enter starts a new one.
	list[1].Attached, created = 2, false
	answer("\n")
	if _, _ = choose(list, create); !created {
		t.Fatal("did not default to a new session")
	}
	answer("y\n")
	if !confirmAttached(list[0]) {
		t.Fatal("yes refused")
	}
	answer("\n")
	if confirmAttached(list[0]) || !confirmAttached(remoteSession{}) {
		t.Fatal("attached without a yes, or asked for a free session")
	}
}

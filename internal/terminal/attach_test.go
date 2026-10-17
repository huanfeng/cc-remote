package terminal

import (
	"reflect"
	"testing"
)

// The resume command Claude Code prints on exit works with ccr in place of claude.
func TestClaudeResume(t *testing.T) {
	for _, c := range []struct {
		args, rest []string
		want       resumeArgs
	}{
		{[]string{"--resume", "7d3ed0ff-f71c"}, nil, resumeArgs{set: true, query: "7d3ed0ff-f71c"}},
		{[]string{"-r"}, nil, resumeArgs{set: true}},
		{[]string{"--resume", "--model", "opus"}, []string{"--model", "opus"}, resumeArgs{set: true}},
		{[]string{"--resume=abc"}, nil, resumeArgs{set: true, query: "abc"}},
		{[]string{"-c", "--effort", "high"}, []string{"--effort", "high"}, resumeArgs{set: true, last: true}},
		{[]string{"--new", "x"}, []string{"--new", "x"}, resumeArgs{}},
		{[]string{"--shared", "--", "-c"}, []string{"--shared", "--", "-c"}, resumeArgs{}},
	} {
		rest, got := claudeResume(c.args)
		if got != c.want || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("%v: %v %+v, want %v %+v", c.args, rest, got, c.rest, c.want)
		}
	}
}

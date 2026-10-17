package claude

import (
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	got := ParseArgs([]string{"--model", "opus", "-d", "--add-dir", "a", "b", "--verbose", "fix it", "--name=x", "--", "-c"})
	want := []Arg{
		{Name: "--model", Values: []string{"opus"}},
		{Name: "-d"},
		{Name: "--add-dir", Values: []string{"a", "b"}},
		{Name: "--verbose"},
		{Values: []string{"fix it"}},
		{Name: "--name", Values: []string{"x"}, Inline: true},
		{Values: []string{"-c"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if w := want[5].Words(); !reflect.DeepEqual(w, []string{"--name=x"}) {
		t.Fatal(w)
	}
	// An optional value takes the next word unless it is an option, as Claude Code does.
	if got := ParseArgs([]string{"-r", "abc", "-w", "--chrome"}); len(got) != 3 || got[0].Values[0] != "abc" || got[1].Values != nil {
		t.Fatalf("optional values %+v", got)
	}
}

func TestCheckTerminalArgs(t *testing.T) {
	for args, want := range map[string][]string{
		"":           {"--add-dir", "../lib", "--dangerously-skip-permissions", "--mcp-config", "m.json"},
		"--print":    {"--verbose", "--print"},
		"--settings": {"--settings=x.json"},
		"hello":      {"--chrome", "hello"},
	} {
		if got := CheckTerminalArgs(want); got != args {
			t.Errorf("%v: %q, want %q", want, got, args)
		}
	}
}

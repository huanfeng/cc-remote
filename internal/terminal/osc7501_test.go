package terminal

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func newTestOSC() (*oscStatus, *bytes.Buffer) {
	var out bytes.Buffer
	return &oscStatus{out: &out, inner: &out}, &out
}

func oscEnv(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestOSCStatusEnabledOnlyInHCTerminals(t *testing.T) {
	var out bytes.Buffer
	cases := []struct {
		name string
		env  map[string]string
		on   bool
	}{
		{"hc terminal", map[string]string{"HC_TERMINAL_ID": "t7"}, true},
		{"plain terminal", map[string]string{}, false},
		{"asked for", map[string]string{"CC_REMOTE_OSC7501": "1"}, true},
		{"hc terminal, osc off", map[string]string{"HC_TERMINAL_ID": "t7", "CC_REMOTE_OSC7501": "0"}, false},
		{"hc terminal, herdr off", map[string]string{"HC_TERMINAL_ID": "t7", "CC_REMOTE_HERDR": "0"}, false},
	}
	for _, c := range cases {
		if got := newOSCStatus(oscEnv(c.env), &out) != nil; got != c.on {
			t.Errorf("%s: enabled = %v, want %v", c.name, got, c.on)
		}
	}
}

func TestOSCSequenceFormat(t *testing.T) {
	if got := string(oscSequence("working")); got != "\x1b]7501;state=working:app=claude-code\x1b\\" {
		t.Errorf("working = %q", got)
	}
	if got := string(oscSequence("clear")); got != "\x1b]7501;state=clear\x1b\\" {
		t.Errorf("clear = %q", got)
	}
	b := string(oscSequence("blocked"))
	if !strings.HasPrefix(b, "\x1b]7501;state=blocked:app=claude-code:msg=") || !strings.HasSuffix(b, "\x1b\\") {
		t.Errorf("blocked = %q", b)
	}
	// The message is base64: no separator, no escape can end up in it.
	msg := strings.TrimSuffix(strings.TrimPrefix(b, "\x1b]7501;state=blocked:app=claude-code:msg="), "\x1b\\")
	if strings.ContainsAny(msg, ":;\x1b\x07 ") || msg == "" {
		t.Errorf("msg = %q", msg)
	}
}

func TestOSCStateMapping(t *testing.T) {
	for status, want := range map[string]string{"running": "working", "waiting": "blocked", "idle": "idle", "interrupted": "idle", "": "idle"} {
		if got := oscState(status); got != want {
			t.Errorf("oscState(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestOSCStatusReportsChangesOnce(t *testing.T) {
	o, out := newTestOSC()
	ctx := context.Background()
	o.sync(ctx, session("running", true))
	o.sync(ctx, session("running", true))
	o.sync(ctx, session("waiting", true))
	o.sync(ctx, session("waiting", true))
	o.sync(ctx, session("idle", true))
	want := string(oscSequence("working")) + string(oscSequence("blocked")) + string(oscSequence("idle"))
	if out.String() != want {
		t.Fatalf("out = %q, want %q", out.String(), want)
	}
}

func TestOSCStatusClearsWhenTheHostCannotRunTheSession(t *testing.T) {
	o, out := newTestOSC()
	ctx := context.Background()
	o.sync(ctx, session("running", true))
	o.sync(ctx, session("running", false))
	o.sync(ctx, nil)
	o.sync(ctx, session("running", true))
	want := string(oscSequence("working")) + string(oscSequence("clear")) + string(oscSequence("working"))
	if out.String() != want {
		t.Fatalf("out = %q, want %q", out.String(), want)
	}
}

func TestOSCStatusCloseClearsOnce(t *testing.T) {
	o, out := newTestOSC()
	o.close() // nothing was reported: nothing to clear
	if out.Len() != 0 {
		t.Fatalf("out = %q", out.String())
	}
	o.sync(context.Background(), session("waiting", true))
	out.Reset()
	o.close()
	o.close()
	if out.String() != string(oscSequence("clear")) {
		t.Fatalf("out = %q", out.String())
	}
}

func TestOSCStatusPassesScreenOutputThroughUnchanged(t *testing.T) {
	o, out := newTestOSC()
	data := "plain \x1b[31mred\x1b[0m \x1b]0;title\x07 中文 \x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\\r\n"
	if n, err := o.Write([]byte(data)); err != nil || n != len(data) {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if out.String() != data {
		t.Fatalf("out = %q", out.String())
	}
}

// The status must never land inside an escape sequence or a character that
// the output cut in two: it waits for the rest.
func TestOSCStatusWaitsForTheEndOfACutSequence(t *testing.T) {
	cuts := map[string][2]string{
		"CSI":               {"abc\x1b[3", "1mdef"},
		"CSI after ESC":     {"abc\x1b", "[31mdef"},
		"OSC string":        {"abc\x1b]0;ti", "tle\x07def"},
		"OSC before ST":     {"abc\x1b]0;title\x1b", "\\def"},
		"UTF-8 2 of 3":      {"abc\xe4\xb8", "\xads"},
		"UTF-8 1 of 3":      {"abc\xe4", "\xb8\xadx"},
		"UTF-8 4-byte":      {"a\xf0\x9f", "\x98\x80b"},
		"charset selection": {"a\x1b(", "Bb"},
		"DCS":               {"a\x1bPq", "data\x1b\\b"},
	}
	for name, c := range cuts {
		o, out := newTestOSC()
		_, _ = o.Write([]byte(c[0]))
		o.sync(context.Background(), session("running", true))
		if strings.Contains(out.String(), "7501") {
			t.Errorf("%s: reported in the middle of a sequence: %q", name, out.String())
			continue
		}
		_, _ = o.Write([]byte(c[1]))
		want := c[0] + c[1] + string(oscSequence("working"))
		if out.String() != want {
			t.Errorf("%s: out = %q, want %q", name, out.String(), want)
		}
	}
}

func TestOSCStatusKeepsOnlyTheLatestWaitingState(t *testing.T) {
	o, out := newTestOSC()
	_, _ = o.Write([]byte("a\x1b[3"))
	ctx := context.Background()
	o.sync(ctx, session("running", true))
	o.sync(ctx, session("waiting", true))
	_, _ = o.Write([]byte("1m"))
	want := "a\x1b[31m" + string(oscSequence("blocked"))
	if out.String() != want {
		t.Fatalf("out = %q, want %q", out.String(), want)
	}
}

func TestOSCStatusCloseEndsACutStringBeforeClearing(t *testing.T) {
	o, out := newTestOSC()
	o.sync(context.Background(), session("running", true))
	_, _ = o.Write([]byte("\x1b]0;half a title"))
	out.Reset()
	o.close()
	if out.String() != "\x1b\\"+string(oscSequence("clear")) {
		t.Fatalf("out = %q", out.String())
	}
}

func TestOSCStatusWrapForwardsToTheNextWriter(t *testing.T) {
	o, out := newTestOSC()
	var screen bytes.Buffer
	w := o.wrap(&screen)
	_, _ = w.Write([]byte("hello"))
	o.sync(context.Background(), session("running", true))
	if screen.String() != "hello" || out.String() != string(oscSequence("working")) {
		t.Fatalf("screen = %q, out = %q", screen.String(), out.String())
	}
}

// Both backends feed the same session follower.
var (
	_ statusSink = (*oscStatus)(nil)
	_ statusSink = (*herdrBridge)(nil)
)

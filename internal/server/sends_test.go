package server

import (
	"strings"
	"testing"
	"time"
)

// A resend with the same send ID (its first reply was lost) gets the first
// answer and reaches Claude once; another ID is another message.
func TestResendIsTakenOnce(t *testing.T) {
	_, ts, _, procs := withFakeProc(t)
	x := createSession(t, ts)
	send := func(text, id string) (int, string) {
		code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": text, "sendId": id}, testToken)
		return code, string(b)
	}
	if code, b := send("one", "send-aaaa-0001"); code != 202 || strings.Contains(b, "duplicate") {
		t.Fatalf("first send %d %s", code, b)
	}
	if code, b := send("one", "send-aaaa-0001"); code != 202 || !strings.Contains(b, `"duplicate":true`) {
		t.Fatalf("resend %d %s", code, b)
	}
	if code, b := send("one", "send-aaaa-0002"); code != 202 || strings.Contains(b, "duplicate") {
		t.Fatalf("new send %d %s", code, b)
	}
	if code, _ := send("x", "bad id!"); code != 400 {
		t.Fatalf("invalid send ID: %d", code)
	}
	p := <-procs
	await(t, func() bool { return strings.Join(p.sentCopy(), ",") == "one,one" })
	time.Sleep(50 * time.Millisecond)
	if got := strings.Join(p.sentCopy(), ","); got != "one,one" {
		t.Fatalf("sent to Claude: %s", got)
	}
}

// The table forgets the oldest when full, and anything after sendMemory.
func TestSendsForget(t *testing.T) {
	m := sends{}
	for i := range maxSendsKept + 10 {
		m.remember("s", strings.Repeat("a", 8)+string(rune('A'+i%26))+strings.Repeat("b", i/26+1), "running")
	}
	if len(m) > maxSendsKept {
		t.Fatalf("%d kept", len(m))
	}
	m.remember("s", "old-send-01", "queued")
	m[sendKey("s", "old-send-01")] = sentMessage{status: "queued", at: time.Now().Add(-sendMemory - time.Minute)}
	if _, ok := m.seen("s", "old-send-01"); ok {
		t.Fatal("a send older than sendMemory is still known")
	}
	if _, ok := m.seen("other", "old-send-01"); ok {
		t.Fatal("a send ID is known across sessions")
	}
}

// A page sends sendId only to a host that lists it: an older host refuses the
// whole message for a field it does not know.
func TestInfoListsFeatures(t *testing.T) {
	_, ts, _ := setup(t, nil)
	code, b := request(t, ts, "GET", "/api/info", nil, testToken)
	if code != 200 || !strings.Contains(string(b), `"features":["sendId","mentions","codex"]`) {
		t.Fatalf("info %d %s", code, b)
	}
}

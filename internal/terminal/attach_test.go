package terminal

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// ccr sends the access token only to this machine or over HTTPS.
func TestNewClientHost(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "access-token"), []byte("token-0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_REMOTE_TOKEN", "")
	for _, host := range []string{"http://192.168.1.5:8787", "http://cc.example.com", "ftp://127.0.0.1", "http://user@127.0.0.1:8787"} {
		if _, err := newClient(host, dir); err == nil || !strings.Contains(err.Error(), "--host") {
			t.Fatalf("%s: %v", host, err)
		}
	}
	if _, err := newClient("https://cc.example.invalid", dir); err != nil {
		t.Fatalf("an HTTPS host: %v", err)
	}
}

func TestPrintable(t *testing.T) {
	if got := printable("a\x1b]52;c;ZXZpbA==\x07b‮c\r\n"); got != "a]52;c;ZXZpbA==bc" {
		t.Fatalf("printable = %q", got)
	}
}

// In a folder Claude Code does not trust, ccr asks first and sends
// trustFolder only after a yes.
func TestStartAsksTrust(t *testing.T) {
	var bodies []map[string]any
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		if body["trustFolder"] != true {
			w.WriteHeader(http.StatusPreconditionRequired)
			w.Write([]byte(`{"error":"trust","code":"trust-required"}`))
			return
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"id":"s1"}`))
	}))
	defer host.Close()
	c := &client{host: host.URL, token: "t"}
	defer func(old *bufio.Reader) { stdin = old }(stdin)
	stdin = bufio.NewReader(strings.NewReader("\n"))
	if _, err := c.start(map[string]any{"cwd": "x"}, "x"); err == nil || len(bodies) != 1 {
		t.Fatalf("Enter (no) started a session: %v, %d requests", err, len(bodies))
	}
	bodies = nil
	stdin = bufio.NewReader(strings.NewReader("y\n"))
	if x, err := c.start(map[string]any{"cwd": "x"}, "x"); err != nil || x.ID != "s1" || len(bodies) != 2 || bodies[0]["trustFolder"] != nil {
		t.Fatalf("yes: %+v %v %v", x, err, bodies)
	}
	// A host from before the code: 400 with the message only.
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["trustFolder"] != true {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"终端会话需要确认信任此目录"}`))
			return
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"id":"s2"}`))
	}))
	defer old.Close()
	stdin = bufio.NewReader(strings.NewReader("y\n"))
	if x, err := (&client{host: old.URL, token: "t"}).start(map[string]any{"cwd": "x"}, "x"); err != nil || x.ID != "s2" {
		t.Fatalf("old host: %+v %v", x, err)
	}
}

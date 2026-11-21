package terminal

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChooseProfile(t *testing.T) {
	defer func(old *bufio.Reader) { stdin = old }(stdin)
	list := []supplierProfile{{ID: "a", Name: "Work", App: "claude"}, {ID: "b", Name: "Backup", App: "claude"}}
	for _, tc := range []struct {
		input, want string
		invalid     bool
	}{{"\n", "", false}, {"0\n", "", false}, {"1\n", "a", false}, {"2\n", "b", false}, {"3\n", "", true}, {"x\n", "", true}} {
		stdin = bufio.NewReader(strings.NewReader(tc.input))
		got, err := chooseProfile(list, "a")
		if got != tc.want || (err != nil) != tc.invalid {
			t.Fatalf("%q: got %q, %v", tc.input, got, err)
		}
	}
}

func TestSelectProfile(t *testing.T) {
	defer func(old *bufio.Reader) { stdin = old }(stdin)
	requests := 0
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/profiles/usage" {
			w.WriteHeader(404)
			return
		}
		requests++
		if r.URL.Path != "/api/profiles" || r.Header.Get("Authorization") != "Bearer t" {
			t.Errorf("profile request: %s, auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"profiles":[{"id":"codex-only","name":"Codex","app":"codex"},{"id":"a","name":"Claude","app":"claude"}],"defaultProfiles":{"claude":"a"}}`))
	}))
	defer host.Close()
	c := &client{host: host.URL, token: "t"}
	if got, err := c.selectProfile(nil, false); got != "" || err != nil || requests != 0 {
		t.Fatalf("non-interactive default: %q %v, requests=%d", got, err, requests)
	}
	stdin = bufio.NewReader(strings.NewReader("1\n"))
	if got, err := c.selectProfile(nil, true); got != "a" || err != nil {
		t.Fatalf("Claude-only picker: %q %v", got, err)
	}
	if got, err := c.selectProfile("a", false); got != "a" || err != nil {
		t.Fatalf("explicit: %q %v", got, err)
	}
	if _, err := c.selectProfile("codex-only", false); err == nil {
		t.Fatal("accepted a Codex profile for Claude")
	}
}

func TestProfileOldHostAndFailures(t *testing.T) {
	status := http.StatusNotFound
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer host.Close()
	c := &client{host: host.URL, token: "t"}
	if got, err := c.selectProfile(nil, true); got != "" || err != nil {
		t.Fatalf("old host default: %q %v", got, err)
	}
	if _, err := c.selectProfile("work", false); err == nil {
		t.Fatal("old host silently ignored explicit profile")
	}
	status = http.StatusUnauthorized
	if _, err := c.selectProfile(nil, true); err == nil {
		t.Fatal("ignored profile authentication failure")
	}
}

func TestOpenAgentStartsOrAttachesWithinProvider(t *testing.T) {
	cwd := t.TempDir()
	var posts int
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode([]remoteSession{
				{ID: "claude-live", Cwd: cwd, Mode: "terminal", Provider: "claude", Online: true},
				{ID: "codex-dead", Cwd: cwd, Mode: "terminal", Provider: "codex"},
			})
		case http.MethodPost:
			posts++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["provider"] != "codex" || body["mode"] != "terminal" || body["start"] != true || body["cwd"] != cwd {
				t.Errorf("create body: %+v", body)
			}
			json.NewEncoder(w).Encode(remoteSession{ID: "codex-new", Online: true})
		}
	}))
	defer host.Close()
	c := &client{host: host.URL}
	if x, err := c.openAgent("claude", agentOptions{cwd: cwd}, false); err != nil || x.ID != "claude-live" || posts != 0 {
		t.Fatalf("Claude open: %+v %v (%d POSTs)", x, err, posts)
	}
	if x, err := c.openAgent("codex", agentOptions{cwd: cwd}, false); err != nil || x.ID != "codex-new" || posts != 1 {
		t.Fatalf("Codex open: %+v %v (%d POSTs)", x, err, posts)
	}
}

func TestAttachRequiresLiveTerminalAndUniqueID(t *testing.T) {
	all := []remoteSession{
		{ID: "abc123", Provider: "codex", Mode: "terminal", Online: true},
		{ID: "abc456", Provider: "codex", Mode: "terminal", NativeID: "saved"},
		{ID: "def123", Provider: "claude", Mode: "managed", Online: true},
	}
	if x, err := attachedSession(all, "ABC1"); err != nil || x.ID != "abc123" {
		t.Fatalf("exact unique prefix: %+v %v", x, err)
	}
	for id, hint := range map[string]string{"abc": "ambiguous", "abc456": "ccr codex resume saved", "def123": "managed", "saved": "not found"} {
		if _, err := attachedSession(all, id); err == nil || !strings.Contains(err.Error(), hint) {
			t.Errorf("attach %q: %v", id, err)
		}
	}
}

func TestAgentShortcutValidation(t *testing.T) {
	for _, args := range [][]string{
		{"--resume"},
		{"--resume="},
		{"--resume", "--new"},
		{"--resume", "native-id", "--new"},
		{"--new", "unexpected"},
	} {
		err := AgentCLI(args, "", false, t.TempDir(), func(string, string) error {
			t.Fatal("invalid syntax reached the host")
			return nil
		})
		if err == nil || strings.Contains(err.Error(), "interactive terminal") {
			t.Errorf("%q: expected an argument error, got %v", args, err)
		}
	}
}

func TestNewAgentShortcutAlwaysOpensProviderPicker(t *testing.T) {
	defer func(old *bufio.Reader) { stdin = old }(stdin)
	cwd := t.TempDir()
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != http.MethodPost || r.URL.Path != "/api/sessions" ||
			json.NewDecoder(r.Body).Decode(&body) != nil || body["provider"] != "codex" || body["cwd"] != cwd {
			t.Errorf("unexpected request: %s %s %+v", r.Method, r.URL.Path, body)
		}
		json.NewEncoder(w).Encode(remoteSession{ID: "new-codex", Online: true})
	}))
	defer host.Close()
	c := &client{host: host.URL}
	stdin = bufio.NewReader(strings.NewReader("q\n"))
	if _, err := c.openAgent("", agentOptions{fresh: true, cwd: cwd}, false); err != errPickerCancelled {
		t.Fatalf("cancel new: %v", err)
	}
	stdin = bufio.NewReader(strings.NewReader("2\n"))
	if x, err := c.openAgent("", agentOptions{fresh: true, cwd: cwd}, false); err != nil || x.ID != "new-codex" {
		t.Fatalf("open new from picker: %+v %v", x, err)
	}
}

func TestResumeNativeIDAutomatically(t *testing.T) {
	const id = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	for _, tc := range []struct {
		name, provider, want, errText string
		claude, codex                 int
		live                          bool
	}{
		{name: "Claude", provider: "claude", claude: 200, codex: 404, want: "resumed"},
		{name: "Codex", provider: "codex", claude: 404, codex: 200, want: "resumed"},
		{name: "running Claude before history exists", live: true, codex: 404, want: "live-claude"},
		{name: "both histories", claude: 200, codex: 200, errText: "ccr codex resume"},
		{name: "running Claude and Codex history", live: true, codex: 200, errText: "ccr claude resume"},
		{name: "missing", claude: 404, codex: 404, errText: "ccr session attach"},
		{name: "lookup failure", claude: 200, codex: 503, errText: "503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/sessions":
					var list []remoteSession
					if tc.live {
						list = append(list, remoteSession{ID: "live-claude", Provider: "claude", Mode: "terminal", NativeID: id, Online: true})
					}
					json.NewEncoder(w).Encode(list)
				case r.Method == http.MethodGet && r.URL.Path == "/api/native/sessions/"+id:
					status := tc.claude
					switch r.URL.Query().Get("provider") {
					case "codex":
						status = tc.codex
					case "claude":
					default:
						t.Errorf("missing provider in %s", r.URL)
					}
					if status != 200 {
						if status == 0 {
							t.Errorf("queried Claude history for a known live terminal")
							status = 404
						}
						w.WriteHeader(status)
						return
					}
					json.NewEncoder(w).Encode(map[string]remoteSession{"session": {ID: id, Cwd: cwd}})
				case r.Method == http.MethodPost && r.URL.Path == "/api/sessions":
					var body map[string]any
					if tc.provider == "" || json.NewDecoder(r.Body).Decode(&body) != nil ||
						body["provider"] != tc.provider || body["resume"] != id || body["cwd"] != cwd {
						t.Errorf("unexpected resume request: %+v", body)
					}
					json.NewEncoder(w).Encode(remoteSession{ID: "resumed", Online: true})
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					w.WriteHeader(500)
				}
			}))
			defer host.Close()
			c := &client{host: host.URL}
			x, err := c.openAgent("", agentOptions{resume: true, nativeID: id, cwd: t.TempDir()}, false)
			if tc.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errText) {
					t.Fatalf("expected error with %q, got %+v %v", tc.errText, x, err)
				}
			} else if err != nil || x.ID != tc.want {
				t.Fatalf("want %s, got %+v %v", tc.want, x, err)
			}
		})
	}
}

func TestCodexTerminalOptionsAndExplicitResume(t *testing.T) {
	cwd := t.TempDir()
	requests := 0
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Model, Cwd, Prompt, Resume string
			CodexArgs                  []string
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/sessions" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if body.Model != "" || body.Cwd != cwd || strings.Join(body.CodexArgs, "|") != "--model=test model|--no-alt-screen" {
			t.Errorf("wrong Codex options: %+v", body)
		}
		if requests == 1 && (body.Prompt != "查一下" || body.Resume != "") ||
			requests == 2 && (body.Prompt != "" || body.Resume != "native-id") {
			t.Errorf("wrong start action: %+v", body)
		}
		json.NewEncoder(w).Encode(remoteSession{ID: "remote-id", Online: true})
	}))
	defer host.Close()
	c := &client{host: host.URL}
	o := agentOptions{cwd: cwd, model: "test model", prompt: "查一下", native: []string{"--no-alt-screen"}}
	if _, err := c.openAgent("codex", o, false); err != nil {
		t.Fatal(err)
	}
	o.prompt, o.nativeID = "", "native-id"
	if _, err := c.resumeAgent("codex", o); err != nil {
		t.Fatal(err)
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

func TestResumeSelectsSupplierBeforeStarting(t *testing.T) {
	cwd, _ := os.Getwd()
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/native/sessions":
			json.NewEncoder(w).Encode([]remoteSession{{ID: "native-id", Cwd: cwd}})
		case "/api/profiles":
			w.Write([]byte(`{"profiles":[{"id":"ccswitch:work","name":"Work","app":"claude"}]}`))
		case "/api/sessions":
			if r.Method == http.MethodGet {
				w.Write([]byte("[]"))
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["resume"] != "native-id" || body["profileId"] != "work" {
				t.Errorf("wrong resume body: %v", body)
			}
			w.Write([]byte(`{"id":"remote-id"}`))
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
	}))
	defer host.Close()
	x, err := (&client{host: host.URL, token: "t"}).resumeAgent("claude", agentOptions{cwd: cwd, last: true, supplier: "work"})
	if err != nil || x.ID != "remote-id" {
		t.Fatalf("resume %v %v", x, err)
	}
}

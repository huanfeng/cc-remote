package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/store"

	"github.com/coder/websocket"
	"golang.org/x/term"
)

type codexTerminalLaunch struct {
	PID            int
	Cwd            string
	Args           []string
	KeyFingerprint string
}

func fakeCodexTerminal() {
	if old, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), old)
	}
	cwd, _ := os.Getwd()
	var fingerprint string
	if key := os.Getenv("OPENAI_API_KEY"); strings.HasPrefix(key, "codex-terminal-test-key:") {
		fingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	}
	data, _ := json.Marshal(codexTerminalLaunch{PID: os.Getpid(), Cwd: cwd, Args: os.Args[1:], KeyFingerprint: fingerprint})
	_ = os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "launch.json"), data, 0600)
	fmt.Print("\x1b[?1049h")
	var b [1]byte
	for {
		if _, err := os.Stdin.Read(b[:]); err != nil || b[0] == 3 {
			return
		}
		if b[0] == 'q' {
			fmt.Printf("CODEX KEY %c\r\n", b[0])
		}
	}
}

func TestCodexTerminalLaunchDetachAndResume(t *testing.T) {
	s, server, st := setup(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.CodexExecutable = exe
	s.cfg.CodexConfigDir = t.TempDir()
	s.cfg.DataDir = t.TempDir()
	t.Setenv("FAKE_CODEX_TERMINAL", "1")
	cwd := filepath.Join(t.TempDir(), "项目 with spaces")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	id := store.ID()
	dir := filepath.Join(s.cfg.CodexConfigDir, "sessions")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{
		"id": id, "cwd": cwd, "timestamp": "2026-10-24T00:00:00Z",
	}})
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-10-24T00-00-00-"+id+".jsonl"), append(meta, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	post := func(body map[string]any) (int, []byte) {
		body["provider"], body["mode"] = "codex", "terminal"
		return call(t, server.URL, http.MethodPost, "/api/sessions", body)
	}
	code, raw := post(map[string]any{"cwd": cwd, "codexArgs": []string{"-m", "my model"}, "prompt": "--version; 查"})
	var fresh store.Session
	if code != 201 || json.Unmarshal(raw, &fresh) != nil || !fresh.Online || fresh.Mode != "terminal" || fresh.ClaudeSessionID != "" {
		t.Fatalf("create Codex terminal: %d %s", code, raw)
	}
	launch := func(want []string) int {
		t.Helper()
		path := filepath.Join(s.cfg.CodexConfigDir, "launch.json")
		var got codexTerminalLaunch
		awaitLong(t, func() string { return fmt.Sprintf("%+v", got) }, func() bool {
			b, _ := os.ReadFile(path)
			return json.Unmarshal(b, &got) == nil && slices.Equal(got.Args, want)
		})
		if !samePathForTest(got.Cwd, cwd) {
			t.Fatalf("wrong working directory: %q", got.Cwd)
		}
		return got.PID
	}
	pid := launch([]string{"--no-daemon", "-m=my model", "--", "--version; 查"})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	attach := func() {
		t.Helper()
		ws := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/terminals/" + fresh.ID + "/stream?attach=1"
		c, _, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + testToken}}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(websocket.StatusNormalClosure, "detach")
		if err := c.Write(ctx, websocket.MessageBinary, []byte("q")); err != nil {
			t.Fatal(err)
		}
		var output strings.Builder
		for !strings.Contains(output.String(), "CODEX KEY q") {
			_, data, err := c.Read(ctx)
			if err != nil {
				t.Fatalf("Codex terminal: %v (%q)", err, output.String())
			}
			output.Write(data)
		}
	}
	attach()
	attach()
	if busy := s.busySessions(); !slices.Contains(busy, fresh.Title) {
		t.Fatalf("host update would interrupt the Codex terminal: %v", busy)
	}
	if again := launch([]string{"--no-daemon", "-m=my model", "--", "--version; 查"}); again != pid {
		t.Fatal("reattach replaced Codex process")
	}
	if code, b := post(map[string]any{"resume": id}); code != 409 || !strings.Contains(string(b), "ccr session attach "+fresh.ID) {
		t.Fatalf("resume while an unbound TUI runs: %d %s", code, b)
	}
	if code, b := call(t, server.URL, http.MethodPost, "/api/sessions/"+fresh.ID+"/messages", map[string]string{"text": "hi"}); code != 409 {
		t.Fatalf("sent a structured message to Codex TUI: %d %s", code, b)
	}
	if code, b := call(t, server.URL, http.MethodPost, "/api/sessions/"+fresh.ID+"/exit", nil); code != 200 {
		t.Fatalf("exit Codex terminal: %d %s", code, b)
	}
	awaitLong(t, func() string { return fresh.ID }, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.terminals[fresh.ID] == nil
	})
	ws := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/terminals/" + fresh.ID + "/stream?attach=1"
	if c, resp, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + testToken}}}); err == nil {
		c.CloseNow()
		t.Fatal("offline attach restarted Codex")
	} else if resp == nil || resp.StatusCode != 409 {
		t.Fatalf("offline attach: %v", err)
	}
	code, raw = post(map[string]any{"resume": strings.ToUpper(id)})
	var resumed store.Session
	if code != 201 || json.Unmarshal(raw, &resumed) != nil || !resumed.Online || resumed.ClaudeSessionID != "" {
		t.Fatalf("resume Codex terminal: %d %s", code, raw)
	}
	if stored, err := st.Get(resumed.ID); err != nil || stored.ClaudeSessionID != id {
		t.Fatalf("Codex launch identity was not stored: %+v %v", stored, err)
	}
	launch([]string{"resume", "--no-daemon", "--", id})
	if code, b := post(map[string]any{"resume": id}); code != 409 || !strings.Contains(string(b), "ccr session attach "+resumed.ID) {
		t.Fatalf("resume while a known TUI runs: %d %s", code, b)
	}
	if code, b := call(t, server.URL, http.MethodPost, "/api/native/sessions/"+id+"/import?provider=codex", map[string]bool{"confirmStopped": true}); code != 409 {
		t.Fatalf("import silently crossed terminal mode: %d %s", code, b)
	}
	if code, b := call(t, server.URL, http.MethodDelete, "/api/sessions/"+resumed.ID, nil); code != 200 {
		t.Fatalf("delete Codex terminal: %d %s", code, b)
	}
	if sessions, err := st.List(); err != nil || len(sessions) != 1 || sessions[0].ID != fresh.ID {
		t.Fatalf("unexpected sessions: %+v %v", sessions, err)
	}
}

func TestCodexTerminalSuppliers(t *testing.T) {
	const firstKey = "codex-terminal-test-key:first"
	const secondKey = "codex-terminal-test-key:second"
	const nativeKey = "codex-terminal-test-key:native"
	s, server, st := setup(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.CodexExecutable = exe
	s.cfg.CodexConfigDir = t.TempDir()
	s.cfg.DataDir = t.TempDir()
	t.Setenv("FAKE_CODEX_TERMINAL", "1")
	t.Setenv("OPENAI_API_KEY", nativeKey)
	cwd := t.TempDir()

	readConfigs := func() supplierConfigReply {
		t.Helper()
		code, raw := call(t, server.URL, http.MethodGet, "/api/supplier-configs", nil)
		var configs supplierConfigReply
		if code != 200 || json.Unmarshal(raw, &configs) != nil {
			t.Fatalf("list supplier configs: %d %s", code, raw)
		}
		return configs
	}
	add := func(name, endpoint, key string) string {
		t.Helper()
		code, raw := call(t, server.URL, http.MethodPost, "/api/supplier-configs", map[string]string{
			"app": "codex", "name": name, "baseUrl": endpoint, "credential": key,
			"model": "relay-model", "authType": "apiKey", "revision": readConfigs().Revision,
		})
		var configs supplierConfigReply
		if code != 200 || json.Unmarshal(raw, &configs) != nil {
			t.Fatalf("create supplier: %d %s", code, raw)
		}
		for _, profile := range configs.Profiles {
			if profile.Name == name {
				return profile.ID
			}
		}
		t.Fatalf("supplier %q missing: %s", name, raw)
		return ""
	}
	first := add("First relay", "https://one.example/v1", firstKey)
	second := add("Second relay", "https://two.example/v1", secondKey)
	setDefault := func(id string) {
		t.Helper()
		if code, raw := call(t, server.URL, http.MethodPut, "/api/profiles/default", map[string]string{
			"app": "codex", "profileId": id,
		}); code != 200 {
			t.Fatalf("set Codex default supplier: %d %s", code, raw)
		}
	}
	setDefault(first)

	path := filepath.Join(s.cfg.CodexConfigDir, "launch.json")
	readLaunch := func() codexTerminalLaunch {
		t.Helper()
		var launch codexTerminalLaunch
		awaitLong(t, func() string { return fmt.Sprintf("%+v", launch) }, func() bool {
			data, _ := os.ReadFile(path)
			return json.Unmarshal(data, &launch) == nil
		})
		return launch
	}
	post := func(body map[string]any) (int, []byte) {
		t.Helper()
		body["provider"], body["mode"], body["cwd"] = "codex", "terminal", cwd
		code, raw := call(t, server.URL, http.MethodPost, "/api/sessions", body)
		if strings.Contains(string(raw), "codex-terminal-test-key:") {
			t.Fatal("session API returned a supplier credential")
		}
		return code, raw
	}
	start := func(body map[string]any, status int, profile, key, provider, endpoint string) store.Session {
		t.Helper()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		code, raw := post(body)
		var session store.Session
		if code != status || json.Unmarshal(raw, &session) != nil || !session.Online || session.ProfileID != profile {
			t.Fatalf("create or resume with supplier %q: %d %s", profile, code, raw)
		}
		launch := readLaunch()
		args := strings.Join(launch.Args, " ")
		if launch.KeyFingerprint != fmt.Sprintf("%x", sha256.Sum256([]byte(key))) || strings.Contains(args, "codex-terminal-test-key:") {
			t.Fatalf("incorrect supplier credential isolation: %+v", launch)
		}
		if provider != "" {
			if !strings.Contains(args, "model_provider=") || !strings.Contains(args, provider) ||
				!strings.Contains(args, endpoint) {
				t.Fatalf("incorrect supplier config: %q", args)
			}
		} else if strings.Contains(args, "model_provider=") || strings.Contains(args, ".example/v1") {
			t.Fatalf("native launch received a CCR profile: %q", args)
		}
		if stored, err := st.Get(session.ID); err != nil || stored.ProfileID != profile ||
			strings.Contains(stored.ProfileJSON, "codex-terminal-test-key:") {
			t.Fatalf("incorrect stored supplier reference for %s: %v", session.ID, err)
		}
		return session
	}
	stop := func(session store.Session) {
		t.Helper()
		if code, raw := call(t, server.URL, http.MethodPost, "/api/sessions/"+session.ID+"/exit", nil); code != 200 {
			t.Fatalf("stop Codex terminal: %d %s", code, raw)
		}
		awaitLong(t, func() string { return session.ID }, func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.terminals[session.ID] == nil
		})
	}
	reject := func(body map[string]any) {
		t.Helper()
		before, err := st.List()
		if err != nil {
			t.Fatal(err)
		}
		if code, raw := post(body); code < 400 || code >= 500 {
			t.Fatalf("wanted supplier selection rejection: %d %s", code, raw)
		}
		s.mu.Lock()
		online := len(s.terminals)
		s.mu.Unlock()
		after, err := st.List()
		if err != nil || len(after) != len(before) || online != 0 {
			t.Fatalf("rejected supplier changed sessions: %d -> %d, live=%d, err=%v", len(before), len(after), online, err)
		}
	}

	code, raw := call(t, server.URL, http.MethodPost, "/api/sessions", map[string]string{"provider": "codex", "cwd": cwd})
	var managed store.Session
	if code != 201 || json.Unmarshal(raw, &managed) != nil || managed.ProfileID != first {
		t.Fatalf("managed Codex default: %d %s", code, raw)
	}
	firstAlias, secondAlias := codexTerminalSupplierAlias(first), codexTerminalSupplierAlias(second)
	defaultTerminal := start(map[string]any{}, 201, managed.ProfileID, firstKey, firstAlias, "https://one.example/v1")
	if launch := readLaunch(); !slices.Contains(launch.Args, `model="relay-model"`) {
		t.Fatalf("supplier model was lost: %+v", launch)
	}
	stop(defaultTerminal)
	customModel := start(map[string]any{"profileId": second, "codexArgs": []string{"--model", "cli-model"}}, 201, second, secondKey, secondAlias, "https://two.example/v1")
	if launch := readLaunch(); !slices.Contains(launch.Args, "--model=cli-model") || slices.Contains(launch.Args, `model="relay-model"`) {
		t.Fatalf("supplier model overrode the CLI model: %+v", launch)
	}
	stop(customModel)
	native := start(map[string]any{"codexArgs": []string{"--profile", "native-relay"}}, 201, "", nativeKey, "", "")
	if nativeLaunch := readLaunch(); !slices.Contains(nativeLaunch.Args, "--profile=native-relay") {
		t.Fatalf("native profile option was lost: %+v", nativeLaunch)
	}
	stop(native)
	for _, args := range [][]string{{"--profile=native-relay"}, {"--oss"}, {"--local-provider", "ollama"}} {
		reject(map[string]any{"profileId": first, "codexArgs": args})
	}

	writeHistory := func(provider string) string {
		t.Helper()
		id := store.ID()
		dir := filepath.Join(s.cfg.CodexConfigDir, "sessions")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{
			"id": id, "cwd": cwd, "timestamp": "2026-10-24T00:00:00Z", "model_provider": provider,
		}})
		if err := os.WriteFile(filepath.Join(dir, "rollout-2026-10-24T00-00-00-"+id+".jsonl"), append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		return id
	}
	setDefault(second)
	nativeID := writeHistory(firstAlias)
	reject(map[string]any{"resume": nativeID, "profileId": second})
	resumed := start(map[string]any{"resume": nativeID}, 201, first, firstKey, firstAlias, "https://one.example/v1")
	stop(resumed)
	same := start(map[string]any{"resume": nativeID}, 200, first, firstKey, firstAlias, "https://one.example/v1")
	if resumed.ID != same.ID {
		t.Fatalf("resume created another CCR session: %s != %s", resumed.ID, same.ID)
	}
	stop(same)
	stop(start(map[string]any{"resume": writeHistory("openai")}, 201, "", nativeKey, "", ""))
	reject(map[string]any{"resume": writeHistory(codexTerminalSupplierAlias("local:deleted"))})

	if code, raw := call(t, server.URL, http.MethodDelete, "/api/supplier-configs/"+first, map[string]string{
		"revision": readConfigs().Revision,
	}); code != 200 {
		t.Fatalf("delete Codex supplier: %d %s", code, raw)
	}
	reject(map[string]any{"resume": nativeID})
}

func TestClaudeCLICreateEagerAndAttachStrict(t *testing.T) {
	s, base, _, _ := setupTerminal(t)
	offline := createTerminal(t, base)
	code, raw := call(t, base, http.MethodPost, "/api/sessions", map[string]any{
		"provider": "claude", "mode": "terminal", "cwd": t.TempDir(), "trustFolder": true, "start": true,
	})
	var online store.Session
	if code != 201 || json.Unmarshal(raw, &online) != nil || !online.Online {
		t.Fatalf("eager Claude terminal: %d %s", code, raw)
	}
	header := http.Header{"Authorization": {"Bearer " + testToken}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, x := range []store.Session{offline, online} {
		ws := "ws" + strings.TrimPrefix(base, "http") + "/api/terminals/" + x.ID + "/stream?attach=1"
		c, resp, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: header})
		if x.Online {
			if err != nil {
				t.Fatal(err)
			}
			c.CloseNow()
		} else if err == nil || resp == nil || resp.StatusCode != 409 {
			if c != nil {
				c.CloseNow()
			}
			t.Fatalf("offline strict attach: %v", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminals[offline.ID] != nil || s.terminals[online.ID] == nil {
		t.Fatal("strict attach unexpectedly changed the runner set")
	}
}

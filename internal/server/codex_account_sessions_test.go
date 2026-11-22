package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

type codexAccountSessionFixture struct {
	server  *Server
	http    *httptest.Server
	store   *store.Store
	work    codexAccount
	private codexAccount
}

func newCodexAccountSessionFixture(t *testing.T) codexAccountSessionFixture {
	t.Helper()
	s, ts, st := setup(t, nil)
	s.cfg.DataDir, s.cfg.CodexConfigDir = t.TempDir(), t.TempDir()
	work := codexAccount{ID: codexAccountIDPrefix + store.ID(), Name: "Work account"}
	private := codexAccount{ID: codexAccountIDPrefix + store.ID(), Name: "Personal account"}
	if err := s.saveCodexAccounts([]codexAccount{work, private}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []codexAccount{work, private} {
		if err := os.MkdirAll(s.codexAccountHome(account.ID), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return codexAccountSessionFixture{s, ts, st, work, private}
}

func writeCodexAccountSessionHistory(t *testing.T, home, cwd string) string {
	t.Helper()
	id := store.ID()
	dir := filepath.Join(home, "sessions", "2026", "11", "22")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{
		"id": id, "cwd": cwd, "timestamp": "2026-11-22T00:00:00Z", "model_provider": "openai",
	}})
	if err != nil {
		t.Fatal(err)
	}
	data := string(meta) + "\n" + `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Check account history"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-11-22T00-00-00-"+id+".jsonl"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f codexAccountSessionFixture) setPersonalDefault(t *testing.T) {
	t.Helper()
	code, raw := request(t, f.http, http.MethodPut, "/api/profiles/default", map[string]string{
		"app": "codex", "profileId": f.private.ID,
	}, testToken)
	if code != http.StatusOK {
		t.Fatalf("set Codex account default: %d %s", code, raw)
	}
}

func TestCodexAccountTerminalSessions(t *testing.T) {
	f := newCodexAccountSessionFixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f.server.cfg.CodexExecutable = exe
	t.Setenv("FAKE_CODEX_TERMINAL", "1")
	t.Setenv("OPENAI_API_KEY", "codex-terminal-test-key:parent-should-not-leak")
	f.setPersonalDefault(t)

	code, raw := request(t, f.http, http.MethodGet, "/api/profiles", nil, testToken)
	var catalog profileFile
	if code != http.StatusOK || json.Unmarshal(raw, &catalog) != nil || catalog.DefaultProfiles["codex"] != f.private.ID {
		t.Fatalf("shared Codex supplier catalog: %d %s", code, raw)
	}
	for _, account := range []codexAccount{f.work, f.private} {
		if !slices.ContainsFunc(catalog.Profiles, func(p supplierProfile) bool {
			return p.ID == account.ID && p.Name == account.Name && p.App == "codex"
		}) {
			t.Fatalf("catalog omitted %q: %s", account.Name, raw)
		}
	}

	cwd := t.TempDir()
	workHome := f.server.codexAccountHome(f.work.ID)
	personalHome := f.server.codexAccountHome(f.private.ID)
	launchPath := filepath.Join(workHome, "launch.json")
	post := func(body map[string]any) (int, []byte) {
		t.Helper()
		body["provider"], body["mode"], body["cwd"] = "codex", "terminal", cwd
		return request(t, f.http, http.MethodPost, "/api/sessions", body, testToken)
	}
	launch := func(body map[string]any, status int, nativeID string) store.Session {
		t.Helper()
		if err := os.Remove(launchPath); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		code, raw := post(body)
		var x store.Session
		if code != status || json.Unmarshal(raw, &x) != nil || !x.Online || x.ProfileID != f.work.ID || x.ProfileName != f.work.Name {
			t.Fatalf("start Work account terminal: %d %s", code, raw)
		}
		var got codexTerminalLaunch
		awaitLong(t, func() string { return fmt.Sprintf("launch record in %s: %+v", workHome, got) }, func() bool {
			b, _ := os.ReadFile(launchPath)
			return json.Unmarshal(b, &got) == nil
		})
		if got.KeyFingerprint != "" || !samePathForTest(got.Cwd, cwd) || !slices.Contains(got.Args, "--no-daemon") {
			t.Fatalf("unexpected Codex OAuth launch: %+v", got)
		}
		if nativeID != "" && (!slices.Contains(got.Args, "resume") || !slices.Contains(got.Args, nativeID)) {
			t.Fatalf("Codex did not resume %s: %+v", nativeID, got)
		}
		return x
	}
	stop := func(x store.Session) {
		t.Helper()
		if code, b := request(t, f.http, http.MethodPost, "/api/sessions/"+x.ID+"/exit", nil, testToken); code != http.StatusOK {
			t.Fatalf("exit Codex account terminal: %d %s", code, b)
		}
		awaitLong(t, func() string { return x.ID }, func() bool {
			f.server.mu.Lock()
			defer f.server.mu.Unlock()
			return f.server.terminals[x.ID] == nil
		})
	}
	stop(launch(map[string]any{"profileId": f.work.ID}, http.StatusCreated, ""))
	workID := writeCodexAccountSessionHistory(t, workHome, cwd)
	personalID := writeCodexAccountSessionHistory(t, personalHome, cwd)
	nativeID := writeCodexAccountSessionHistory(t, f.server.cfg.CodexConfigDir, cwd)

	code, raw = request(t, f.http, http.MethodGet, "/api/native/sessions?provider=codex", nil, testToken)
	var history []claude.NativeSession
	if code != http.StatusOK || json.Unmarshal(raw, &history) != nil {
		t.Fatalf("list merged Codex history: %d %s", code, raw)
	}
	for id, profileID := range map[string]string{workID: f.work.ID, personalID: f.private.ID, nativeID: ""} {
		if !slices.ContainsFunc(history, func(x claude.NativeSession) bool { return x.ID == id && x.ProfileID == profileID }) {
			t.Fatalf("merged history omitted %s or assigned the wrong account: %s", id, raw)
		}
	}
	code, raw = request(t, f.http, http.MethodGet, "/api/native/sessions/"+workID+"?provider=codex", nil, testToken)
	if code != http.StatusOK || !strings.Contains(string(raw), "Check account history") {
		t.Fatalf("resolve account history by native ID: %d %s", code, raw)
	}
	if code, raw := post(map[string]any{"resume": workID, "profileId": f.private.ID}); code != http.StatusBadRequest && code != http.StatusConflict {
		t.Fatalf("resumed account thread under a different account: %d %s", code, raw)
	}
	if code, raw := post(map[string]any{"resume": nativeID, "profileId": f.work.ID}); code != http.StatusBadRequest && code != http.StatusConflict {
		t.Fatalf("resumed host-native thread in an unrelated account home: %d %s", code, raw)
	}
	resumed := launch(map[string]any{"resume": workID}, http.StatusCreated, workID)
	stop(resumed)
	again := launch(map[string]any{"resume": workID}, http.StatusOK, workID)
	if again.ID != resumed.ID {
		t.Fatalf("resume registered another CCR session: %s != %s", again.ID, resumed.ID)
	}
	stop(again)

	disabled := f.work
	disabled.Disabled = true
	if err := f.server.saveCodexAccounts([]codexAccount{disabled, f.private}); err != nil {
		t.Fatal(err)
	}
	code, raw = request(t, f.http, http.MethodGet, "/api/native/sessions/"+workID+"?provider=codex", nil, testToken)
	if code != http.StatusOK || !strings.Contains(string(raw), "Check account history") {
		t.Fatalf("disconnected account history was lost: %d %s", code, raw)
	}
	if code, raw := post(map[string]any{"resume": workID}); code != http.StatusBadRequest && code != http.StatusConflict {
		t.Fatalf("resumed disconnected account or fell back to a different login: %d %s", code, raw)
	}
	query := url.Values{"q": {"account history"}, "native": {"codex"}, "sessionId": {workID}}
	code, raw = request(t, f.http, http.MethodGet, "/api/search/messages?"+query.Encode(), nil, testToken)
	var results struct{ Hits []messageHit }
	if code != http.StatusOK || json.Unmarshal(raw, &results) != nil || len(results.Hits) != 1 ||
		results.Hits[0].SessionID != workID || results.Hits[0].Native != "codex" || results.Hits[0].Locator == "" {
		t.Fatalf("search disconnected account history: %d %s", code, raw)
	}
	query.Del("q")
	query.Set("locator", results.Hits[0].Locator)
	code, raw = request(t, f.http, http.MethodGet, "/api/search/context?"+query.Encode(), nil, testToken)
	var matchContext struct {
		Events   []store.Event
		MatchSeq int64
	}
	if code != http.StatusOK || json.Unmarshal(raw, &matchContext) != nil || !slices.ContainsFunc(matchContext.Events, func(e store.Event) bool {
		return e.Seq == matchContext.MatchSeq && e.Text == "Check account history"
	}) {
		t.Fatalf("open search hit in disconnected account history: %d %s", code, raw)
	}

	relative := filepath.Join("sessions", "2026", "11", "22", "rollout-2026-11-22T00-00-00-"+workID+".jsonl")
	data, err := os.ReadFile(filepath.Join(workHome, relative))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(personalHome, relative), data, 0600); err != nil {
		t.Fatal(err)
	}
	code, raw = request(t, f.http, http.MethodGet, "/api/search/messages?q=account+history&provider=codex", nil, testToken)
	if code != http.StatusOK || json.Unmarshal(raw, &results) != nil || len(results.Hits) != 2 ||
		slices.ContainsFunc(results.Hits, func(hit messageHit) bool { return hit.SessionID == workID }) {
		t.Fatalf("global search exposed an ambiguous Codex thread: %d %s", code, raw)
	}
}

func TestCodexAccountManagedHistoryLaunch(t *testing.T) {
	f := newCodexAccountSessionFixture(t)
	f.setPersonalDefault(t)
	home := f.server.codexAccountHome(f.work.ID)
	threadID := writeCodexAccountSessionHistory(t, home, t.TempDir())

	started := make(chan claude.Options, 1)
	f.server.cfg.StartCodexProcess = func(o claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		started <- o
		return runnerStarter(func(_ context.Context, _ claude.Options, emit func(claude.Event) error) error {
			return emit(claude.Event{Kind: "assistant", Text: "Codex reply"})
		})(o, h)
	}
	path := "/api/native/sessions/" + threadID + "/import?provider=codex"
	if code, raw := request(t, f.http, http.MethodPost, path, map[string]any{"confirmStopped": true, "profileId": f.private.ID}, testToken); code != http.StatusBadRequest && code != http.StatusConflict {
		t.Fatalf("imported account history under the wrong login: %d %s", code, raw)
	}
	code, raw := request(t, f.http, http.MethodPost, path, map[string]bool{
		"confirmStopped": true,
	}, testToken)
	var session store.Session
	if code != http.StatusCreated || json.Unmarshal(raw, &session) != nil || session.ProfileID != f.work.ID ||
		session.Mode != "managed" || !session.CodexAccountLocked {
		t.Fatalf("import account history as a managed Codex session: %d %s", code, raw)
	}
	if code, raw := request(t, f.http, http.MethodPost, "/api/sessions/"+session.ID+"/messages", map[string]string{"text": "continue"}, testToken); code != http.StatusAccepted {
		t.Fatalf("send to imported account session: %d %s", code, raw)
	}
	select {
	case o := <-started:
		if o.ConfigDir != home || !o.CodexAccount || !o.Resume || o.SessionID != threadID || o.Provider != "codex" {
			t.Fatalf("managed Codex resumed the wrong account or thread: %+v", o)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("managed Codex did not start")
	}
	await(t, func() bool { got, _ := f.store.Get(session.ID); return got.Status == "completed" })
}

func TestCodexAccountCannotSignOutDuringRequest(t *testing.T) {
	f := newCodexAccountSessionFixture(t)
	release, err := f.server.holdCodexAccount(f.work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := f.server.disableCodexAccount(f.work.ID, i18n.Current()); status != 409 || err == nil {
		t.Fatalf("disabled account while a request was using it: %d %v", status, err)
	}
	release()
	if status, err := f.server.disableCodexAccount(f.work.ID, i18n.Current()); status != 0 || err != nil {
		t.Fatalf("cannot disable unused account: %d %v", status, err)
	}
	if release, err := f.server.holdCodexAccount(f.work.ID); err == nil {
		release()
		t.Fatal("disabled account accepted a new request")
	}
}

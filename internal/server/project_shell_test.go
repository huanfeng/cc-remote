package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestProjectShellLifecycleAndResume(t *testing.T) {
	s, host, st := setup(t, nil)
	defer closeShellTestHost(t, s)
	s.cfg.DataDir = t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cwd := createSession(t, host).Cwd
	if code, b := request(t, host, "POST", "/api/sessions", map[string]string{"title": "same project", "cwd": cwd}, testToken); code != http.StatusCreated {
		t.Fatalf("create second session: %d %s", code, b)
	}
	start := func(host *httptest.Server, body map[string]any) projectShellView {
		t.Helper()
		code, b := request(t, host, "POST", "/api/project-shells", body, testToken)
		var view projectShellView
		if code != http.StatusOK || json.Unmarshal(b, &view) != nil || view.ID == "" {
			t.Fatalf("start: %d %s", code, b)
		}
		return view
	}
	type startReply struct {
		view projectShellView
		code int
		err  error
	}
	payload, err := json.Marshal(map[string]string{"cwd": cwd})
	if err != nil {
		t.Fatal(err)
	}
	concurrent := make(chan startReply, 2)
	for range 2 {
		go func() {
			req, err := http.NewRequestWithContext(ctx, "POST", host.URL+"/api/project-shells", bytes.NewReader(payload))
			if err != nil {
				concurrent <- startReply{err: err}
				return
			}
			req.Header.Set("Authorization", "Bearer "+testToken)
			response, err := host.Client().Do(req)
			if err != nil {
				concurrent <- startReply{err: err}
				return
			}
			defer response.Body.Close()
			var view projectShellView
			err = json.NewDecoder(response.Body).Decode(&view)
			concurrent <- startReply{view, response.StatusCode, err}
		}()
	}
	var initial projectShellView
	for range 2 {
		select {
		case reply := <-concurrent:
			if reply.err != nil || reply.code != http.StatusOK || reply.view.State != "running" || reply.view.ID == "" || reply.view.RunID == "" {
				t.Fatalf("start: %+v", reply)
			}
			if initial.ID == "" {
				initial = reply.view
			} else if reply.view.ID != initial.ID || reply.view.RunID != initial.RunID {
				t.Fatalf("reuse: %+v", reply)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	base := "/api/project-shells/" + initial.ID + "/runs/" + initial.RunID
	other := start(host, map[string]any{"cwd": createSession(t, host).Cwd})
	if other.ID == initial.ID || other.RunID == initial.RunID {
		t.Fatalf("different project shared a shell: %+v", other)
	}
	issue := func() string {
		t.Helper()
		code, b := request(t, host, "POST", base+"/ticket", nil, testToken)
		var out struct{ Ticket string }
		if code != http.StatusOK || json.Unmarshal(b, &out) != nil || out.Ticket == "" {
			t.Fatalf("ticket: %d %s", code, b)
		}
		return out.Ticket
	}
	streamURL := func(ticket string, after uint64) string {
		return "ws" + strings.TrimPrefix(host.URL, "http") + base + "/stream?ticket=" + ticket + "&after=" + strconv.FormatUint(after, 10)
	}
	otherBase := "/api/project-shells/" + other.ID + "/runs/" + other.RunID
	wrongRun := strings.Replace(streamURL(issue(), 0), base, otherBase, 1)
	if ws, got, err := websocket.Dial(ctx, wrongRun, nil); err == nil || got == nil || got.StatusCode != http.StatusUnauthorized {
		if ws != nil {
			_ = ws.CloseNow()
		}
		t.Fatalf("cross-run ticket: response=%v error=%v", got, err)
	}
	ticket := issue()
	if ws, got, err := websocket.Dial(ctx, streamURL(ticket, 0), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"https://unrelated.example"}},
	}); err == nil || got == nil || got.StatusCode != http.StatusForbidden {
		if ws != nil {
			_ = ws.CloseNow()
		}
		t.Fatalf("cross-origin dial: response=%v error=%v", got, err)
	}
	dial := func(url string) *websocket.Conn {
		t.Helper()
		ws, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ws.CloseNow() })
		return ws
	}
	first := dial(streamURL(ticket, 0))
	if ws, got, err := websocket.Dial(ctx, streamURL(ticket, 0), nil); err == nil || got == nil || got.StatusCode != http.StatusUnauthorized {
		if ws != nil {
			_ = ws.CloseNow()
		}
		t.Fatalf("reused ticket: response=%v error=%v", got, err)
	}
	readFrame := func(ws *websocket.Conn) (projectShellFrame, []byte) {
		t.Helper()
		kind, b, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.MessageBinary {
			if len(b) < 8 {
				t.Fatal("missing binary sequence")
			}
			seq := binary.BigEndian.Uint64(b)
			return projectShellFrame{Type: "output", Seq: &seq}, b[8:]
		}
		var frame projectShellFrame
		if err := json.Unmarshal(b, &frame); err != nil {
			t.Fatal(err)
		}
		return frame, nil
	}
	until := func(ws *websocket.Conn, match func(projectShellFrame, []byte) bool) projectShellFrame {
		t.Helper()
		for {
			frame, b := readFrame(ws)
			if match(frame, b) {
				return frame
			}
		}
	}
	waitType := func(ws *websocket.Conn, kind string) projectShellFrame {
		return until(ws, func(f projectShellFrame, _ []byte) bool { return f.Type == kind })
	}
	waitControl := func(ws *websocket.Conn, want bool) {
		v := waitType(ws, "control")
		if v.Held == nil || *v.Held != want {
			t.Fatalf("control: %+v, wanted %v", v, want)
		}
	}
	ready := waitType(first, "ready")
	if ready.Seq == nil {
		t.Fatal("ready missing cursor")
	}
	waitControl(first, true)
	second := dial(streamURL(issue(), *ready.Seq))
	waitType(second, "ready")
	waitControl(second, false)
	if err := second.Write(ctx, websocket.MessageText, []byte(`{"type":"take"}`)); err != nil {
		t.Fatal(err)
	}
	waitControl(second, true)
	waitControl(first, false)
	// Activity from the new owner is a synchronization point after the old owner's
	// no-op resize. The shared PTY can only publish an accepted owner's size.
	if err := first.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":77,"rows":20}`)); err != nil {
		t.Fatal(err)
	}
	if err := second.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":93,"rows":21}`)); err != nil {
		t.Fatal(err)
	}
	size := until(second, func(f projectShellFrame, _ []byte) bool { return f.Type == "size" && f.Seq != nil })
	if size.Cols != 93 || size.Rows != 21 {
		t.Fatalf("unexpected size frame: %+v", size)
	}
	_ = second.CloseNow()
	waitControl(first, true)
	_ = first.CloseNow()
	s.projectShells.mu.Lock()
	process := s.projectShells.entries[initial.ID].run.pty
	s.projectShells.mu.Unlock()
	if err := process.Write([]byte("echo ccr-project-shell-resume\r")); err != nil {
		t.Fatal(err)
	}
	third := dial(streamURL(issue(), *size.Seq))
	var replay strings.Builder
	previous := *size.Seq
	until(third, func(f projectShellFrame, b []byte) bool {
		if f.Type == "output" || (f.Type == "size" && f.Seq != nil) {
			if *f.Seq != previous+1 {
				t.Fatalf("cursor gap: %d => %d", previous, *f.Seq)
			}
			previous = *f.Seq
			replay.Write(b)
		}
		return strings.Contains(replay.String(), "ccr-project-shell-resume")
	})
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	interrupted := until(third, func(f projectShellFrame, _ []byte) bool { return f.Type == "exit" })
	if interrupted.Detail != "interrupted" {
		t.Fatalf("shutdown exit frame: %+v", interrupted)
	}
	_ = third.CloseNow()
	newServer := New(st, s.cfg)
	newHost := httptest.NewServer(newServer.Handler())
	defer newHost.Close()
	defer func() { _ = newServer.Close(ctx) }()
	if code, _ := request(t, newHost, "POST", base+"/ticket", nil, testToken); code != http.StatusGone {
		t.Fatalf("old run ticket after restart: %d", code)
	}
	restored := start(newHost, map[string]any{"shellId": initial.ID})
	if restored.ID != initial.ID || restored.Cwd != initial.Cwd || restored.RunID == initial.RunID || !restored.Recovered {
		t.Fatalf("restored shell: %+v", restored)
	}
	newBase := "/api/project-shells/" + restored.ID + "/runs/" + restored.RunID
	if code, b := request(t, newHost, "DELETE", newBase, nil, testToken); code != http.StatusNoContent {
		t.Fatalf("stop: %d %s", code, b)
	}
	if stopped := start(newHost, map[string]any{"shellId": initial.ID}); stopped.State != "stopped" || stopped.RunID != "" {
		t.Fatalf("ordinary open must not restart an explicitly stopped shell: %+v", stopped)
	}
	if reopened := start(newHost, map[string]any{"shellId": initial.ID, "restart": true}); reopened.State != "running" || reopened.RunID == "" || reopened.RunID == restored.RunID || reopened.Recovered {
		t.Fatalf("explicit reopen did not start a fresh run: %+v", reopened)
	}
}

func TestProjectShellDefaultOnAndDirectories(t *testing.T) {
	s, host, _ := setup(t, nil)
	defer closeShellTestHost(t, s)
	s.cfg.DataDir = t.TempDir()
	if code, _ := request(t, host, "GET", "/api/project-shells", nil, ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthorized: %d", code)
	}
	if code, _ := request(t, host, "GET", "/api/project-shells", nil, testToken); code != http.StatusOK {
		t.Fatalf("default availability: %d", code)
	}
	if code, _ := request(t, host, "POST", "/api/project-shells", map[string]string{"cwd": "relative"}, testToken); code != http.StatusBadRequest {
		t.Fatalf("relative directory: %d", code)
	}
	if code, b := request(t, host, "POST", "/api/project-shells", map[string]string{"cwd": t.TempDir()}, testToken); code != http.StatusOK {
		t.Fatalf("start without AI session: %d %s", code, b)
	}
}

func TestProjectShellMultipleEntriesAndLegacyDefault(t *testing.T) {
	s, host, st := setup(t, nil)
	defer closeShellTestHost(t, s)
	s.cfg.DataDir = t.TempDir()
	cwd, key, err := projectShellDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal([]map[string]string{{"id": "legacy", "projectKey": key, "cwd": cwd}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.cfg.DataDir, projectShellFile), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	const path = "/api/project-shells"
	viewRequest := func(host *httptest.Server, method, path string, body any) projectShellView {
		t.Helper()
		code, b := request(t, host, method, path, body, testToken)
		var view projectShellView
		if code != http.StatusOK || json.Unmarshal(b, &view) != nil || view.ID == "" || view.Default == nil {
			t.Fatalf("%s %s: %d %s", method, path, code, b)
		}
		return view
	}
	defaultShell := viewRequest(host, "POST", path, map[string]any{"cwd": cwd})
	named := viewRequest(host, "POST", path, map[string]any{"cwd": cwd, "create": true, "name": " Build "})
	if defaultShell.ID != "legacy" || !*defaultShell.Default || named.ID == defaultShell.ID || named.RunID == "" || named.RunID == defaultShell.RunID || *named.Default || named.ProjectKey != defaultShell.ProjectKey || named.Name != "Build" {
		t.Fatalf("default shell: %+v, named shell: %+v", defaultShell, named)
	}
	if again := viewRequest(host, "POST", path, map[string]any{"cwd": cwd, "restart": true}); again.RunID != defaultShell.RunID {
		t.Fatalf("opening by cwd replaced the default run: %+v", again)
	}
	renamed := viewRequest(host, "PATCH", path+"/"+named.ID, map[string]string{"name": "Logs"})
	if renamed.RunID != named.RunID || renamed.Name != "Logs" {
		t.Fatalf("rename changed the run or did not save its name: %+v", renamed)
	}
	if code, b := request(t, host, "PATCH", path+"/"+named.ID, map[string]string{"name": "escaped\x1b[31m"}, testToken); code != http.StatusBadRequest {
		t.Fatalf("rename accepted a terminal control character: %d %s", code, b)
	}
	filtered := path + "?" + url.Values{"cwd": {cwd}}.Encode()
	var views []projectShellView
	if code, b := request(t, host, "GET", filtered, nil, testToken); code != http.StatusOK || json.Unmarshal(b, &views) != nil || len(views) != 1 || views[0].ID != defaultShell.ID {
		t.Fatalf("legacy cwd lookup: %d %s", code, b)
	}
	if code, b := request(t, host, "GET", path, nil, testToken); code != http.StatusOK || json.Unmarshal(b, &views) != nil || len(views) != 2 || views[0].ID != defaultShell.ID || views[1].ID != named.ID {
		t.Fatalf("list project shells: %d %s", code, b)
	}
	closeShellTestHost(t, s)
	newServer := New(st, s.cfg)
	newHost := httptest.NewServer(newServer.Handler())
	defer newHost.Close()
	defer closeShellTestHost(t, newServer)
	restored := viewRequest(newHost, "POST", path, map[string]any{"shellId": named.ID})
	if restored.Name != "Logs" || *restored.Default || !restored.Recovered || restored.RunID == "" || restored.RunID == named.RunID {
		t.Fatalf("restored named shell: %+v", restored)
	}
}

func closeShellTestHost(t *testing.T, s *Server) {
	t.Helper()
	// Windows shells hold their working directory until the host closes their PTYs.
	// A defer runs before TempDir's cleanup, which was registered after setup.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Error(err)
	}
}

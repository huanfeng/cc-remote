package codex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"cc-remote/internal/claude"
)

// A fake RPC stdin keeps these tests independent of the subprocess TestMain.
type permissionsRPCWriter struct{ messages chan []byte }

func (w *permissionsRPCWriter) Write(b []byte) (int, error) {
	w.messages <- append([]byte(nil), b...)
	return len(b), nil
}
func (w *permissionsRPCWriter) Close() error { return nil }

func permissionsProcess(t *testing.T, handler http.HandlerFunc) (*Process, <-chan []byte) {
	t.Helper()
	srv := httptest.NewServer(handler)
	ctx, cancel := context.WithCancel(context.Background())
	w := &permissionsRPCWriter{messages: make(chan []byte, 16)}
	p := &Process{
		threadID: "thread-1", requests: map[string]context.CancelFunc{},
		options: claude.Options{BridgeURL: srv.URL, BridgeToken: "permissions-secret"},
		rpc:     &rpcClient{cmd: &exec.Cmd{}, ctx: ctx, cancel: cancel, stdin: w, writeGate: make(chan struct{}, 1)},
	}
	t.Cleanup(func() { p.cancelRequests(); cancel(); srv.Close() })
	return p, w.messages
}

func permissionsParams() map[string]any {
	return map[string]any{
		"threadId": "thread-1", "turnId": "turn-1", "itemId": "item-1",
		"environmentId": "env-1", "cwd": "D:/project", "startedAtMs": 1792886400000,
		"reason": "Read fixtures and fetch dependencies",
		"permissions": map[string]any{
			"network":    map[string]any{"enabled": true},
			"fileSystem": map[string]any{"read": []string{"D:/fixtures"}, "write": []string{"D:/cache"}},
		},
	}
}

func sendPermissions(t *testing.T, p *Process, params any) {
	t.Helper()
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.handle(rpcMessage{ID: json.RawMessage(`"permission-1"`), Method: "item/permissions/requestApproval", Params: b}); err != nil {
		t.Fatal(err)
	}
}

func permissionsReply(t *testing.T, messages <-chan []byte) rpcMessage {
	t.Helper()
	select {
	case b := <-messages:
		var m rpcMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		if string(m.ID) != `"permission-1"` {
			t.Fatalf("wrong response ID: %s", b)
		}
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("permission request did not resolve")
		return rpcMessage{}
	}
}

func assertPermissionsResult(t *testing.T, m rpcMessage, permissions any) {
	t.Helper()
	if m.Error != nil {
		t.Fatalf("unexpected RPC error: %v", m.Error)
	}
	want, _ := json.Marshal(map[string]any{"permissions": permissions, "scope": "turn"})
	var gotValue, wantValue any
	if err := json.Unmarshal(m.Result, &gotValue); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(want, &wantValue)
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("result = %s, want %s", m.Result, want)
	}
}

func TestPermissionsBridgeDecisions(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		allow      bool
	}{
		{"allow", `{"decision":"allow","scope":"session","permissions":{"network":{"enabled":false}}}`, 200, true},
		{"deny", `{"decision":"deny"}`, 200, false},
		{"http error", `{"decision":"allow"}`, 500, false},
		{"transport error", "", 0, false},
		{"invalid JSON", `{`, 200, false},
		{"invalid decision", `{"decision":"acceptForSession"}`, 200, false},
		{"missing decision", `{}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := permissionsParams()
			// Unknown request metadata must remain visible, without float64 loss.
			params["extra"] = json.RawMessage(`{"sequence":9007199254740993}`)
			seen := make(chan json.RawMessage, 1)
			p, messages := permissionsProcess(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer permissions-secret" {
					t.Error("incorrect bridge request")
				}
				var request struct {
					Kind     string          `json:"kind"`
					ToolName string          `json:"toolName"`
					Input    json.RawMessage `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Kind != "approval" || request.ToolName != "RequestPermissions" {
					t.Errorf("wrong bridge kind/tool: %+v", request)
				}
				seen <- request.Input
				if tc.status == 0 {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			sendPermissions(t, p, params)
			m := permissionsReply(t, messages)
			want := any(map[string]any{})
			if tc.allow {
				want = params["permissions"]
			}
			assertPermissionsResult(t, m, want)
			select {
			case input := <-seen:
				expected, _ := json.Marshal(params)
				var gotCompact, wantCompact map[string]json.RawMessage
				_ = json.Unmarshal(input, &gotCompact)
				_ = json.Unmarshal(expected, &wantCompact)
				if !reflect.DeepEqual(gotCompact, wantCompact) {
					t.Fatalf("bridge input = %s, want %s", input, expected)
				}
			default:
				t.Fatal("request bypassed human bridge")
			}
		})
	}
}

func TestPermissionsBridgeInvalidRequests(t *testing.T) {
	cases := map[string]func(map[string]any){
		"missing permissions":    func(p map[string]any) { delete(p, "permissions") },
		"null permissions":       func(p map[string]any) { p["permissions"] = nil },
		"wrong permissions type": func(p map[string]any) { p["permissions"] = []any{} },
		"unknown permission":     func(p map[string]any) { p["permissions"] = map[string]any{"futurePrivilege": true} },
		"invalid enabled": func(p map[string]any) {
			p["permissions"] = map[string]any{"network": map[string]any{"enabled": "true"}}
		},
		"unknown network": func(p map[string]any) {
			p["permissions"] = map[string]any{"network": map[string]any{"hosts": []string{"*"}}}
		},
		"invalid path list": func(p map[string]any) {
			p["permissions"] = map[string]any{"fileSystem": map[string]any{"write": []any{42}}}
		},
		"incomplete entry": func(p map[string]any) {
			p["permissions"] = map[string]any{"fileSystem": map[string]any{"entries": []any{map[string]any{"access": "write"}}}}
		},
		"unknown access": func(p map[string]any) {
			p["permissions"] = json.RawMessage(`{"fileSystem":{"entries":[{"access":"execute","path":{"type":"path","path":"D:/x"}}]}}`)
		},
		"unknown path variant": func(p map[string]any) {
			p["permissions"] = json.RawMessage(`{"fileSystem":{"entries":[{"access":"write","path":{"type":"future"}}]}}`)
		},
		"invalid depth":            func(p map[string]any) { p["permissions"] = json.RawMessage(`{"fileSystem":{"globScanMaxDepth":0}}`) },
		"null path":                func(p map[string]any) { p["permissions"] = json.RawMessage(`{"fileSystem":{"read":[null]}}`) },
		"null entry":               func(p map[string]any) { p["permissions"] = json.RawMessage(`{"fileSystem":{"entries":[null]}}`) },
		"unknown filesystem field": func(p map[string]any) { p["permissions"] = json.RawMessage(`{"fileSystem":{"execute":["D:/x"]}}`) },
		"incomplete special path": func(p map[string]any) {
			p["permissions"] = json.RawMessage(`{"fileSystem":{"entries":[{"access":"read","path":{"type":"special","value":{"kind":"unknown"}}}]}}`)
		},
		"missing cwd":          func(p map[string]any) { delete(p, "cwd") },
		"missing turn":         func(p map[string]any) { delete(p, "turnId") },
		"missing item":         func(p map[string]any) { delete(p, "itemId") },
		"missing timestamp":    func(p map[string]any) { delete(p, "startedAtMs") },
		"fractional timestamp": func(p map[string]any) { p["startedAtMs"] = 1.5 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			called := make(chan struct{}, 1)
			p, messages := permissionsProcess(t, func(w http.ResponseWriter, r *http.Request) {
				called <- struct{}{}
				_, _ = w.Write([]byte(`{"decision":"allow"}`))
			})
			params := permissionsParams()
			mutate(params)
			sendPermissions(t, p, params)
			assertPermissionsResult(t, permissionsReply(t, messages), map[string]any{})
			select {
			case <-called:
				t.Fatal("invalid schema reached approval bridge")
			default:
			}
		})
	}
}

func TestPermissionsBridgeMalformedJSON(t *testing.T) {
	p, messages := permissionsProcess(t, func(w http.ResponseWriter, r *http.Request) { t.Error("malformed request reached bridge") })
	if err := p.handle(rpcMessage{ID: json.RawMessage(`"permission-1"`), Method: "item/permissions/requestApproval", Params: json.RawMessage(`{`)}); err != nil {
		t.Fatal(err)
	}
	m := permissionsReply(t, messages)
	if m.Error == nil || m.Error.Code != -32602 || len(m.Result) != 0 {
		t.Fatalf("malformed JSON response: %+v", m)
	}
}

func TestPermissionsBridgeWrongThread(t *testing.T) {
	p, messages := permissionsProcess(t, func(w http.ResponseWriter, r *http.Request) { t.Error("wrong thread reached bridge") })
	params := permissionsParams()
	params["threadId"] = "other-thread"
	sendPermissions(t, p, params)
	if m := permissionsReply(t, messages); m.Error == nil || m.Error.Code != -32601 || len(m.Result) != 0 {
		t.Fatalf("wrong-thread response: %+v", m)
	}
}

func TestPermissionsBridgeCancellation(t *testing.T) {
	for _, mode := range []string{"cancel", "resolved", "rpc shutdown"} {
		t.Run(mode, func(t *testing.T) {
			started, disconnected, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			p, messages := permissionsProcess(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(started)
				select {
				case <-r.Context().Done():
					close(disconnected)
				case <-release:
				}
				_, _ = w.Write([]byte(`{"decision":"allow"}`))
			})
			defer close(release)
			sendPermissions(t, p, permissionsParams())
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("bridge was not called")
			}
			if mode == "resolved" {
				// A notification for another thread must not cancel this prompt.
				_ = p.handle(rpcMessage{Method: "serverRequest/resolved", Params: json.RawMessage(`{"threadId":"other-thread","requestId":"permission-1"}`)})
				p.mu.Lock()
				pending := len(p.requests)
				p.mu.Unlock()
				if pending != 1 {
					t.Fatal("wrong-thread resolved canceled request")
				}
				if err := p.handle(rpcMessage{Method: "serverRequest/resolved", Params: json.RawMessage(`{"threadId":"thread-1","requestId":"permission-1"}`)}); err != nil {
					t.Fatal(err)
				}
			} else if mode == "rpc shutdown" {
				p.rpc.cancel()
			} else {
				p.cancelRequests()
			}
			select {
			case <-disconnected:
			case <-time.After(3 * time.Second):
				t.Fatal("bridge HTTP request was not canceled")
			}
			select {
			case b := <-messages:
				t.Fatalf("canceled request produced response: %s", b)
			case <-time.After(100 * time.Millisecond):
			}
			p.mu.Lock()
			pending := len(p.requests)
			p.mu.Unlock()
			if pending != 0 {
				t.Fatal("canceled permission request was not removed")
			}
		})
	}
}

func TestPermissionsBridgeFilesystemVariants(t *testing.T) {
	for _, permissions := range []string{
		`{}`,
		`{"network":null,"fileSystem":null}`,
		`{"network":{"enabled":false},"fileSystem":{"read":null,"write":[],"entries":null,"globScanMaxDepth":null}}`,
		`{"fileSystem":{"globScanMaxDepth":2,"entries":[{"access":"read","path":{"type":"path","path":"D:/fixtures"}},{"access":"write","path":{"type":"glob_pattern","pattern":"D:/cache/**"}},{"access":"deny","path":{"type":"special","value":{"kind":"project_roots","subpath":"secret"}}}]}}`,
		`{"fileSystem":{"entries":[{"access":"read","path":{"type":"special","value":{"kind":"unknown","path":"future","subpath":null}}}]}}`,
	} {
		t.Run(permissions, func(t *testing.T) {
			p, messages := permissionsProcess(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"decision":"allow"}`)) })
			params := permissionsParams()
			params["permissions"] = json.RawMessage(permissions)
			sendPermissions(t, p, params)
			assertPermissionsResult(t, permissionsReply(t, messages), json.RawMessage(permissions))
		})
	}
}

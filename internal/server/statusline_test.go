package server

import (
	"bytes"
	"cc-remote/internal/statusline"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStatuslineLifecycle(t *testing.T) {
	s, ts, _ := setup(t, nil)
	path := filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")
	os.MkdirAll(s.cfg.ClaudeConfigDir, 0700)
	original := []byte("{\n  \"model\": \"opus\",\n  \"statusLine\": {\"type\":\"command\",\"command\":\"original\"}\n}\n")
	os.WriteFile(path, original, 0600)
	read := func() statuslineState {
		t.Helper()
		code, b := request(t, ts, "GET", "/api/statusline", nil, testToken)
		if code != 200 {
			t.Fatalf("%d %s", code, b)
		}
		var x statuslineState
		json.Unmarshal(b, &x)
		return x
	}
	initial := read()
	for _, style := range []string{"compact", "multiline", "color"} {
		x := read()
		code, b := request(t, ts, "PUT", "/api/statusline", map[string]string{"style": style, "revision": x.Revision}, testToken)
		if code != 200 {
			t.Fatalf("%d %s", code, b)
		}
		x = read()
		if !x.Installed || x.Style != style {
			t.Fatal(x)
		}
	}
	code, _ := request(t, ts, "DELETE", "/api/statusline", map[string]string{"revision": initial.Revision}, testToken)
	if code != 409 {
		t.Fatal(code)
	}
	x := read()
	code, b := request(t, ts, "DELETE", "/api/statusline", map[string]string{"revision": x.Revision}, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	raw, _ := os.ReadFile(path)
	var got, want map[string]json.RawMessage
	json.Unmarshal(raw, &got)
	json.Unmarshal(original, &want)
	if !jsonEqual(got["statusLine"], want["statusLine"]) || !bytes.Equal(got["model"], want["model"]) || len(got[statuslineBackupKey]) != 0 {
		t.Fatal(string(raw))
	}
	x = read()
	code, _ = request(t, ts, "PUT", "/api/statusline", map[string]string{"style": "compact", "revision": x.Revision}, testToken)
	if code != 200 {
		t.Fatal(code)
	}
	raw, _ = os.ReadFile(path)
	json.Unmarshal(raw, &got)
	got["statusLine"] = json.RawMessage(`{"type":"command","command":"external"}`)
	raw, _ = json.Marshal(got)
	os.WriteFile(path, raw, 0600)
	x = read()
	if x.Installed {
		t.Fatal("external config claimed")
	}
	code, _ = request(t, ts, "DELETE", "/api/statusline", map[string]string{"revision": x.Revision}, testToken)
	if code != 409 {
		t.Fatal(code)
	}
}

func TestStatuslineAbsentAndInvalid(t *testing.T) {
	s, ts, _ := setup(t, nil)
	code, b := request(t, ts, "GET", "/api/statusline", nil, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	var x statuslineState
	json.Unmarshal(b, &x)
	code, _ = request(t, ts, "PUT", "/api/statusline", map[string]string{"style": "bad", "revision": x.Revision}, testToken)
	if code != 400 {
		t.Fatal(code)
	}
	code, b = request(t, ts, "PUT", "/api/statusline", map[string]string{"style": "compact", "revision": x.Revision}, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	json.Unmarshal(b, &x)
	code, b = request(t, ts, "DELETE", "/api/statusline", map[string]string{"revision": x.Revision}, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	raw, _ := os.ReadFile(filepath.Join(s.cfg.ClaudeConfigDir, "settings.json"))
	var got map[string]json.RawMessage
	json.Unmarshal(raw, &got)
	if len(got["statusLine"]) > 0 || len(got[statuslineBackupKey]) > 0 {
		t.Fatal(string(raw))
	}
}

func TestStatuslineCustomOptions(t *testing.T) {
	_, ts, _ := setup(t, nil)
	_, body := request(t, ts, "GET", "/api/statusline", nil, testToken)
	var current statuslineState
	json.Unmarshal(body, &current)
	options := map[string]any{"order": append([]string{}, statusline.Segments...), "segments": []string{"model", "context"}, "colors": map[string]string{"model": "#aabbcc"}, "bars": true, "shortLabels": true, "warning": 75, "critical": 90, "separator": " / "}
	code, body := request(t, ts, "PUT", "/api/statusline", map[string]any{"style": "color", "revision": current.Revision, "options": options}, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	json.Unmarshal(body, &current)
	if current.Options == nil || len(current.Options.Segments) != 2 || !current.Options.Bars || !current.Capabilities["customize"] || !current.Capabilities["shortLabels"] || !current.Options.ShortLabels || len(current.Options.Order) != len(statusline.Segments) {
		t.Fatalf("%s", body)
	}
	options["critical"] = 50
	code, _ = request(t, ts, "PUT", "/api/statusline", map[string]any{"style": "color", "revision": current.Revision, "options": options}, testToken)
	if code != 400 {
		t.Fatal(code)
	}
	// An old client changing only style retains customization.
	code, body = request(t, ts, "PUT", "/api/statusline", map[string]any{"style": "compact", "revision": current.Revision}, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	json.Unmarshal(body, &current)
	if current.Options == nil || !current.Options.Bars || !current.Options.ShortLabels || len(current.Options.Order) != len(statusline.Segments) {
		t.Fatal(string(body))
	}
}

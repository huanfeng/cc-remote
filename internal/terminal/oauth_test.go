package terminal

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type fakeOAuthHost struct {
	items []oauthItem
	calls []string
	last  map[string]string
}

func (f *fakeOAuthHost) do(method, path string, body any, out any) error {
	f.calls = append(f.calls, method+" "+path)
	if b, ok := body.(map[string]string); ok {
		f.last = b
	}
	if method == "GET" {
		b, _ := json.Marshal(map[string]any{"profiles": f.items})
		return json.Unmarshal(b, out)
	}
	if method == "POST" {
		return json.Unmarshal([]byte(`{"id":"oauth:new1","name":"`+f.last["name"]+`"}`), out)
	}
	return nil
}

func TestRunOAuthCommands(t *testing.T) {
	host := &fakeOAuthHost{items: []oauthItem{{"oauth:ab12", "Team"}, {"oauth:cd34", "Personal"}}}
	token := func() (string, error) { return "sk-ant-oat01-x", nil }
	run := func(sub string, names ...string) (string, error) {
		var out bytes.Buffer
		err := runOAuth(host, &out, sub, names, token)
		return out.String(), err
	}

	if out, err := run("ls"); err != nil || !strings.Contains(out, "oauth:ab12\tTeam") || strings.Contains(out, "sk-ant") {
		t.Fatal(out, err)
	}
	if _, err := run("rm", "team"); err != nil || host.calls[len(host.calls)-1] != "DELETE /api/profiles/oauth/ab12" {
		t.Fatal("rm by name, any case:", host.calls, err)
	}
	if _, err := run("rm", "oauth:cd34"); err != nil || host.calls[len(host.calls)-1] != "DELETE /api/profiles/oauth/cd34" {
		t.Fatal("rm by id:", host.calls, err)
	}
	if _, err := run("token", "Personal"); err != nil || host.last["token"] != "sk-ant-oat01-x" || host.calls[len(host.calls)-1] != "PATCH /api/profiles/oauth/cd34" {
		t.Fatal("token:", host.calls, host.last, err)
	}
	if _, err := run("rename", "Personal", "Home"); err != nil || host.last["name"] != "Home" {
		t.Fatal("rename:", host.last, err)
	}
	if out, err := run("add", "Work"); err != nil || !strings.Contains(out, "oauth:new1") || host.last["token"] != "sk-ant-oat01-x" {
		t.Fatal("add:", out, err)
	}
	if _, err := run("rm", "missing"); err == nil {
		t.Fatal("unknown name must fail")
	}
	empty := func() (string, error) { return "", nil }
	before := len(host.calls)
	if err := runOAuth(host, &bytes.Buffer{}, "add", []string{"X"}, empty); err == nil || len(host.calls) != before+1 {
		t.Fatal("an empty token must be refused without a write request:", err, host.calls)
	}
}

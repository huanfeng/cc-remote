package terminal

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type fakeProfileHost struct {
	list  profileList
	calls []string
	last  map[string]string
}

func (f *fakeProfileHost) do(method, path string, body any, out any) error {
	f.calls = append(f.calls, method+" "+path)
	if m, ok := body.(map[string]string); ok {
		f.last = m
	}
	if method == "GET" {
		b, _ := json.Marshal(f.list)
		return json.Unmarshal(b, out)
	}
	return nil
}

func TestRunProfileDefault(t *testing.T) {
	host := &fakeProfileHost{list: profileList{
		Profiles: []profileEntry{
			{"ccswitch:one", "Relay", "claude", ""},
			{"oauth:ab12", "Team", "claude", "oauth"},
			{"ccswitch:cx", "Codex relay", "codex", ""},
		},
		DefaultProfiles:    map[string]string{"claude": "ccswitch:one"},
		ConfiguredDefaults: map[string]string{},
	}}
	run := func(sub string, names ...string) (string, error) {
		var out bytes.Buffer
		err := runProfile(host, &out, sub, "claude", names)
		return out.String(), err
	}

	out, err := run("ls")
	if err != nil || !strings.Contains(out, "*\tccswitch:one\tRelay") || !strings.Contains(out, " \toauth:ab12\tTeam\toauth") || strings.Contains(out, "Codex relay") {
		t.Fatal(out, err)
	}
	if out, err = run("default"); err != nil || !strings.Contains(out, "Relay") {
		t.Fatal(out, err)
	}
	for want, arg := range map[string]string{"oauth:ab12": "team", "ccswitch:one": "one", "@native": "native", "": "follow"} {
		if _, err = run("default", arg); err != nil || host.last["profileId"] != want || host.last["app"] != "claude" || host.calls[len(host.calls)-1] != "PUT /api/profiles/default" {
			t.Fatalf("default %q: %v %v", arg, host.last, err)
		}
	}
	if _, err = run("default", "nobody"); err == nil {
		t.Fatal("an unknown supplier must fail before any write")
	}
	if _, err = run("default", "Codex relay"); err == nil {
		t.Fatal("a supplier of another app must not match")
	}
}

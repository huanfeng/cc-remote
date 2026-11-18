package terminal

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeSupplierHost struct {
	configs           supplierCollection
	profiles          profileList
	calls             []string
	body              any
	readErr, writeErr error
}

func (f *fakeSupplierHost) do(method, path string, body any, out any) error {
	f.calls = append(f.calls, method+" "+path)
	if method != "GET" {
		f.body = body
		return f.writeErr
	}
	if f.readErr != nil {
		return f.readErr
	}
	var value any = f.configs
	if path == "/api/profiles" {
		value = f.profiles
	}
	b, _ := json.Marshal(value)
	return json.Unmarshal(b, out)
}
func supplierFixture() *fakeSupplierHost {
	return &fakeSupplierHost{
		configs:  supplierCollection{Profiles: []supplierItem{{ID: "local:one", Name: "Relay", App: "claude", BaseURL: "https://relay.example", Model: "model-a", AuthType: "apiKey"}}, Revision: "revision-one"},
		profiles: profileList{Profiles: []profileEntry{{ID: "local:one", Name: "Relay", App: "claude", Kind: "local"}, {ID: "oauth:one", Name: "Account", App: "claude", Kind: "oauth"}, {ID: "ccswitch:one", Name: "Imported", App: "claude"}}},
	}
}
func TestSupplierAdd(t *testing.T) {
	host := supplierFixture()
	var out bytes.Buffer
	opts := supplierOptions{Name: "Custom", BaseURL: "https://api.example/v1", AuthType: "authToken", Set: map[string]bool{"auth-type": true}}
	err := runSupplier(host, &out, "add", "claude", nil, opts, func(stdin bool) (string, error) {
		if stdin {
			t.Fatal("unexpected stdin")
		}
		return " secret-value ", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body := host.body.(supplierItem)
	if body.Name != "Custom" || body.Credential != "secret-value" || body.AuthType != "authToken" || body.Revision != "revision-one" || host.calls[len(host.calls)-1] != "POST /api/supplier-configs" {
		t.Fatalf("unexpected request: %+v %v", body, host.calls)
	}
	if strings.Contains(out.String(), "secret-value") {
		t.Fatal("credential leaked")
	}
}
func TestSupplierDefaultAuthentication(t *testing.T) {
	for app, want := range map[string]string{"claude": "authToken", "codex": "apiKey"} {
		t.Run(app, func(t *testing.T) {
			host := supplierFixture()
			err := runSupplier(host, &bytes.Buffer{}, "add", app, nil, supplierOptions{Name: "Custom", BaseURL: "https://api.example/v1"}, func(bool) (string, error) { return "fixture-key", nil })
			if err != nil {
				t.Fatal(err)
			}
			if got := host.body.(supplierItem).AuthType; got != want {
				t.Fatalf("authentication = %s, want %s", got, want)
			}
		})
	}
}

func TestSupplierEditPreservesFieldsAndCredential(t *testing.T) {
	t.Setenv("CC_REMOTE_SUPPLIER_CREDENTIAL", "accidental-environment-secret")
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "untouched", true: "explicit-blank"}[explicit], func(t *testing.T) {
			host := supplierFixture()
			opts := supplierOptions{Name: "Renamed", Model: "", CredentialStdin: explicit, Set: map[string]bool{"name": true, "model": true}}
			reads := 0
			err := runSupplier(host, &bytes.Buffer{}, "edit", "claude", []string{"Relay"}, opts, func(stdin bool) (string, error) {
				reads++
				if !stdin {
					t.Fatal("edit must explicitly read stdin")
				}
				return "  ", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			body := host.body.(supplierItem)
			if body.Credential != "" || body.Name != "Renamed" || body.Model != "" || body.BaseURL != "https://relay.example" || body.AuthType != "apiKey" || body.Revision != "revision-one" {
				t.Fatalf("unexpected edit: %+v", body)
			}
			if reads != map[bool]int{false: 0, true: 1}[explicit] {
				t.Fatalf("unexpected credential reads %d", reads)
			}
		})
	}
}
func TestSupplierEditExplicitCredential(t *testing.T) {
	host := supplierFixture()
	err := runSupplier(host, &bytes.Buffer{}, "edit", "claude", []string{"local:one"}, supplierOptions{CredentialStdin: true}, func(bool) (string, error) { return "replacement", nil })
	if err != nil || host.body.(supplierItem).Credential != "replacement" {
		t.Fatal(err, host.body)
	}
}
func TestSupplierRemove(t *testing.T) {
	host := supplierFixture()
	err := runSupplier(host, &bytes.Buffer{}, "rm", "claude", []string{"local:one"}, supplierOptions{}, nil)
	if err != nil || host.calls[len(host.calls)-1] != "DELETE /api/supplier-configs/local:one" || host.body.(map[string]string)["revision"] != "revision-one" {
		t.Fatal(err, host.calls, host.body)
	}
}
func TestSupplierRejectsImportedAndOAuth(t *testing.T) {
	for _, name := range []string{"oauth:one", "Account", "ccswitch:one", "Imported"} {
		host := supplierFixture()
		err := runSupplier(host, &bytes.Buffer{}, "rm", "claude", []string{name}, supplierOptions{}, nil)
		if err == nil || host.body != nil {
			t.Fatal(name, err, host.body)
		}
	}
}
func TestSupplierNamesAndExactID(t *testing.T) {
	host := supplierFixture()
	host.profiles.Profiles = append(host.profiles.Profiles, profileEntry{ID: "oauth:duplicate", Name: "Relay", App: "claude", Kind: "oauth"}, profileEntry{ID: "oauth:shadow", Name: "local:one", App: "claude", Kind: "oauth"})
	err := runSupplier(host, &bytes.Buffer{}, "rm", "claude", []string{"Relay"}, supplierOptions{}, nil)
	if err == nil || host.body != nil {
		t.Fatal("ambiguous name must not mutate", err)
	}
	err = runSupplier(host, &bytes.Buffer{}, "rm", "claude", []string{"local:one"}, supplierOptions{}, nil)
	if err != nil {
		t.Fatal("exact id must win", err)
	}
}
func TestSupplierConflictIsNotRetried(t *testing.T) {
	host := supplierFixture()
	conflict := hostError{status: 409, message: "changed"}
	host.writeErr = conflict
	err := runSupplier(host, &bytes.Buffer{}, "rm", "claude", []string{"Relay"}, supplierOptions{}, nil)
	if !errors.Is(err, conflict) || len(host.calls) != 3 {
		t.Fatal(err, host.calls)
	}
}
func TestSupplierOldHost(t *testing.T) {
	host := supplierFixture()
	host.readErr = hostError{status: 404, message: "not found"}
	err := runSupplier(host, &bytes.Buffer{}, "add", "claude", nil, supplierOptions{}, nil)
	if err == nil || (!strings.Contains(err.Error(), "update") && !strings.Contains(err.Error(), "升级")) || len(host.calls) != 1 {
		t.Fatal(err, host.calls)
	}
}
func TestSupplierInvalidAppBeforeRequest(t *testing.T) {
	host := supplierFixture()
	err := runSupplier(host, &bytes.Buffer{}, "rm", "typo", []string{"Relay"}, supplierOptions{}, nil)
	if err == nil || len(host.calls) != 0 {
		t.Fatal(err, host.calls)
	}
}
func TestSupplierCredentialEnvironment(t *testing.T) {
	t.Setenv("CC_REMOTE_SUPPLIER_CREDENTIAL", " safe-secret ")
	got, err := readSupplierCredential(false)
	if err != nil || got != "safe-secret" {
		t.Fatal("credential environment was not read")
	}
}

func TestProfileRejectsInvalidArgumentsBeforeHost(t *testing.T) {
	for _, args := range [][]string{{"ls", "--app", "typo"}, {"rm", "Relay", "--name", "ignored"}, {"edit"}, {"add", "--name", "Relay", "unexpected"}, {"default", "--model", "ignored"}} {
		err := RunProfile(args, t.TempDir(), func(string, string) error { t.Fatal("invalid arguments must not start host"); return nil })
		if err == nil {
			t.Fatal("expected error", args)
		}
	}
}

func TestSupplierShowRedactsAndAdvancedEditPreserves(t *testing.T) {
	host := supplierFixture()
	host.configs.Capabilities = map[string]bool{"advanced": true}
	host.configs.Profiles[0].Credential = "never-print"
	host.configs.Profiles[0].HasCredential = true
	host.configs.Profiles[0].Website = "https://example.com"
	host.configs.Profiles[0].ModelMappings = map[string]string{"opus": "best"}
	var out bytes.Buffer
	if e := runSupplier(host, &out, "show", "claude", []string{"Relay"}, supplierOptions{}, nil); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "never-print") || !strings.Contains(out.String(), "hasCredential") || !strings.Contains(out.String(), "best") {
		t.Fatal(out.String())
	}
	if len(host.calls) != 2 {
		t.Fatal(host.calls)
	}
	fast := "fast"
	timeout := int64(10000)
	opts := supplierOptions{Set: map[string]bool{"haiku-model": true, "timeout-ms": true}, AdvancedStrings: map[string]*string{"haiku-model": &fast}, AdvancedInts: map[string]*int64{"timeout-ms": &timeout}}
	if e := runSupplier(host, &out, "edit", "claude", []string{"Relay"}, opts, nil); e != nil {
		t.Fatal(e)
	}
	body := host.body.(supplierItem)
	if body.ModelMappings["opus"] != "best" || body.ModelMappings["haiku"] != "fast" || body.TimeoutMs != 10000 || body.Credential != "" {
		t.Fatal("advanced edit lost fields")
	}
}

func TestSupplierCLIContextDeclarations(t *testing.T) {
	host := supplierFixture()
	host.configs.Capabilities = map[string]bool{"advanced": true}
	sonnet := "balanced[1M]"
	err := runSupplier(host, &bytes.Buffer{}, "edit", "claude", []string{"local:one"}, supplierOptions{Model: "main[1M]", Set: map[string]bool{"model": true, "sonnet-model": true}, AdvancedStrings: map[string]*string{"sonnet-model": &sonnet}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := host.body.(supplierItem)
	if body.Model != "main[1M]" || body.ModelMappings["sonnet"] != "balanced[1M]" || body.Credential != "" {
		t.Fatal("context declaration or credential retention changed")
	}
}

func TestSupplierFableAndDisplayNameEdits(t *testing.T) {
	host := supplierFixture()
	host.configs.Capabilities = map[string]bool{"advanced": true, "modelNames": true}
	host.configs.Profiles[0].ModelNames = map[string]string{"sonnet": "Daily"}
	fable, name, empty := "deep[1M]", "Hard tasks", ""
	opts := supplierOptions{Set: map[string]bool{"fable-model": true, "fable-model-name": true, "sonnet-model-name": true}, AdvancedStrings: map[string]*string{"fable-model": &fable, "fable-model-name": &name, "sonnet-model-name": &empty}}
	err := runSupplier(host, &bytes.Buffer{}, "edit", "claude", []string{"local:one"}, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := host.body.(supplierItem)
	if body.ModelMappings["fable"] != fable || body.ModelNames["fable"] != name || body.ModelNames["sonnet"] != "" || body.Credential != "" {
		t.Fatal("model fields not updated")
	}
}

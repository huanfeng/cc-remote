package prefs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSaveAndConflict(t *testing.T) {
	dir := t.TempDir()
	p, rev, err := Load(dir)
	if err != nil || p != (Preferences{}) || rev != Revision(nil) {
		t.Fatalf("missing file: %+v %s %v", p, rev, err)
	}
	want := Preferences{PermissionMode: "bypassPermissions", Model: " opus ", Effort: "high"}
	saved, rev2, err := Save(dir, want, rev)
	if err != nil || saved.Model != "opus" {
		t.Fatalf("save: %+v %v", saved, err)
	}
	got, rev3, err := Load(dir)
	if err != nil || got != saved || rev3 != rev2 {
		t.Fatalf("reload: %+v %s %s %v", got, rev2, rev3, err)
	}
	if _, _, err = Save(dir, Preferences{}, rev); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if _, _, err = Save(dir, Preferences{PermissionMode: "yolo"}, rev2); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if _, _, err = Save(dir, Preferences{Effort: "huge"}, rev2); err == nil {
		t.Fatal("invalid effort accepted")
	}
	if _, _, err = Save(dir, Preferences{Model: "a\nb"}, rev2); err == nil {
		t.Fatal("control character accepted")
	}
	if err = os.WriteFile(filepath.Join(dir, fileName), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Load(dir); err == nil {
		t.Fatal("corrupt file accepted")
	}
}

func TestValidModel(t *testing.T) {
	for _, ok := range []string{"", "opus", "default", "opus[1m]", "claude-opus-4-1@20250805", "claude-sonnet-4-5-20250929", "us.anthropic.claude-3:0", "arn:aws:bedrock:us-east-1:1/x", "a"} {
		if !ValidModel(ok) {
			t.Errorf("rejected %q", ok)
		}
	}
	for _, bad := range []string{"-p", "--model", " opus", "opus ", "a b", "a\nb", "a\x00", "模型", "a;b", "a$b", ".x", strings.Repeat("a", 201)} {
		if ValidModel(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
	if !ValidModel(strings.Repeat("a", 200)) {
		t.Error("200 chars rejected")
	}
}

func TestOverAndDangerous(t *testing.T) {
	base := Preferences{PermissionMode: "acceptEdits", Model: "sonnet", Effort: "low"}
	got := base.Over(Preferences{PermissionMode: "plan"})
	if got != (Preferences{PermissionMode: "plan", Model: "sonnet", Effort: "low"}) {
		t.Fatalf("%+v", got)
	}
	for _, m := range []string{"bypassPermissions", "dontAsk", "auto"} {
		if !Dangerous(m) {
			t.Fatal(m)
		}
	}
	if Dangerous("acceptEdits") || Dangerous("") {
		t.Fatal("safe mode marked dangerous")
	}
}

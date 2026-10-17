package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cc-remote/internal/i18n"
)

func putSkill(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCustomCommandBoundaries(t *testing.T) {
	user, project := t.TempDir(), t.TempDir()
	s := &Server{cfg: Config{ClaudeConfigDir: user}}
	for _, name := range []string{"clear", "model", "compact"} {
		putSkill(t, filepath.Join(user, "commands", name+".md"), "collision")
		if _, found, _ := s.skillPrompt(i18n.EN, project, name, ""); found {
			t.Fatalf("builtin collision %s", name)
		}
	}
	if len(s.skillCommands(i18n.EN, project)) != 0 {
		t.Fatal("reserved commands exposed")
	}
	putSkill(t, filepath.Join(user, "commands", "positional.md"), "Review $ARGUMENTS[0]")
	if _, found, err := s.skillPrompt(i18n.EN, project, "positional", "a b"); !found || err == nil {
		t.Fatal("indexed argument silently expanded")
	}
	putSkill(t, filepath.Join(user, "commands", "large.md"), strings.Repeat("$ARGUMENTS", 1000))
	if _, found, err := s.skillPrompt(i18n.EN, project, "large", strings.Repeat("x", 2000)); !found || err == nil {
		t.Fatal("unbounded expansion accepted")
	}
}

func TestCustomCommandsDiscoveryPrecedenceAndExpansion(t *testing.T) {
	user, project := t.TempDir(), t.TempDir()
	s := &Server{cfg: Config{ClaudeConfigDir: user}}
	putSkill(t, filepath.Join(user, "skills", "review", "SKILL.md"), "---\ndescription: 'User review'\n---\nUSER")
	putSkill(t, filepath.Join(project, ".claude", "commands", "review.md"), "LEGACY")
	putSkill(t, filepath.Join(project, ".claude", "skills", "review", "SKILL.md"), "---\r\ndescription: \"Project review\"\r\ndisable-model-invocation: true\r\n---\r\nReview $ARGUMENTS and $ARGUMENTS")
	putSkill(t, filepath.Join(user, "commands", "git", "inspect.md"), "---\ndescription: Inspect repository\n---\nRead repository")
	putSkill(t, filepath.Join(project, ".claude", "skills", "invalid.name", "SKILL.md"), "SKIP")
	commands := s.skillCommands(i18n.EN, project)
	if len(commands) != 2 || commands[0].Name != "git:inspect" || commands[1].Name != "review" || commands[1].Description != "Project review" {
		t.Fatalf("commands %+v", commands)
	}
	prompt, ok, err := s.skillPrompt(i18n.EN, project, "review", "literal `$(echo no)`")
	if !ok || err != nil || !strings.Contains(prompt, "Review literal `$(echo no)` and literal `$(echo no)`") || strings.Contains(prompt, "disable-model-invocation") || strings.Contains(prompt, "LEGACY") || strings.Contains(prompt, "USER") {
		t.Fatalf("bad expansion: %q %v %v", prompt, ok, err)
	}
	if !strings.Contains(prompt, filepath.Base(project)) || !strings.Contains(prompt, "Skill base directory") {
		t.Fatal("missing skill resource base")
	}
	prompt, ok, err = s.skillPrompt(i18n.EN, project, "git:inspect", "extra details")
	if !ok || err != nil || !strings.Contains(prompt, "User arguments: extra details") {
		t.Fatalf("legacy args lost: %q %v %v", prompt, ok, err)
	}
	for _, name := range []string{"../review", "a/b", "git::inspect", "unknown"} {
		if _, ok, _ := s.skillPrompt(i18n.EN, project, name, ""); ok {
			t.Fatalf("unsafe or absent command found: %s", name)
		}
	}
}

func TestCustomCommandsRejectDynamicAndInvalidFiles(t *testing.T) {
	user, project := t.TempDir(), t.TempDir()
	s := &Server{cfg: Config{ClaudeConfigDir: user}}
	putSkill(t, filepath.Join(user, "skills", "dynamic", "SKILL.md"), "Current diff: !`git diff`\nReview it")
	putSkill(t, filepath.Join(user, "commands", "broken.md"), "---\ndescription: Missing closure")
	putSkill(t, filepath.Join(user, "commands", "huge.md"), strings.Repeat("x", maxSkillBytes+1))
	putSkill(t, filepath.Join(user, "commands", "empty.md"), "---\ndescription: Empty\n---\n")
	for _, name := range []string{"dynamic", "empty"} {
		if _, ok, err := s.skillPrompt(i18n.EN, project, name, ""); !ok || err == nil {
			t.Fatalf("unsupported %s accepted: %v %v", name, ok, err)
		}
	}
	for _, name := range []string{"broken", "huge"} {
		if _, ok, _ := s.skillPrompt(i18n.EN, project, name, ""); ok {
			t.Fatalf("invalid %s discovered", name)
		}
	}
}

func TestCustomCommandsDoNotFollowEscapingSymlinks(t *testing.T) {
	user, project, outside := t.TempDir(), t.TempDir(), t.TempDir()
	root := filepath.Join(user, "commands")
	putSkill(t, filepath.Join(outside, "outside.md"), "PRIVATE")
	putSkill(t, filepath.Join(root, "inside.md"), "PUBLIC")
	if err := os.Symlink(filepath.Join(outside, "outside.md"), filepath.Join(root, "escape.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := &Server{cfg: Config{ClaudeConfigDir: user}}
	if _, ok, _ := s.skillPrompt(i18n.EN, project, "escape", ""); ok {
		t.Fatal("followed link outside configured root")
	}
	if err := os.Symlink(filepath.Join(root, "inside.md"), filepath.Join(root, "alias.md")); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.skillPrompt(i18n.EN, project, "alias", ""); !ok || err != nil {
		t.Fatalf("internal link rejected %v %v", ok, err)
	}
}

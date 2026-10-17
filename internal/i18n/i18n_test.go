package i18n

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]Lang{"": Auto, "auto": Auto, "zh": ZH, "zh-CN": ZH, "zh_TW.UTF-8": ZH, "EN": EN, "en_US.UTF-8": EN} {
		if got, ok := Parse(in); !ok || got != want {
			t.Errorf("Parse(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := Parse("fr"); ok {
		t.Error("fr parsed")
	}
}

func TestMessages(t *testing.T) {
	if Current() != EN || T("on", "开") != "on" {
		t.Fatal("unset language is not English")
	}
	t.Cleanup(func() { Set(EN) })
	Set(ZH)
	if F("%d files", "%d 个文件", 3) != "3 个文件" {
		t.Fatal(F("%d files", "%d 个文件", 3))
	}
	base := errors.New("x")
	if err := Errorf("failed: %w", "失败：%w", base); !errors.Is(err, base) || err.Error() != "失败：x" {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	t.Setenv("CC_REMOTE_LANG", "zh-CN")
	if Resolve("auto") != ZH || Resolve("en") != EN {
		t.Fatal("CC_REMOTE_LANG ignored or overriding an explicit setting")
	}
}

// Package i18n picks between the English and Chinese text of a message.
//
// Messages are written inline as pairs, T("English", "中文"), so each one keeps
// both texts next to the code that shows it. The process language is set once
// at startup (Set); until then it is English, which keeps tests deterministic.
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// Lang is a supported language: "en" or "zh".
type Lang string

const (
	EN Lang = "en"
	ZH Lang = "zh"
	// Auto follows the system language (a setting, never a resolved language).
	Auto Lang = "auto"
)

var current atomic.Value // Lang

// Parse accepts auto, en and zh, also as locale names (zh-CN, en_US.UTF-8).
func Parse(s string) (Lang, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "auto" || s == "":
		return Auto, true
	case s == "zh" || strings.HasPrefix(s, "zh-") || strings.HasPrefix(s, "zh_"):
		return ZH, true
	case s == "en" || strings.HasPrefix(s, "en-") || strings.HasPrefix(s, "en_"):
		return EN, true
	}
	return "", false
}

// Resolve turns a setting into a language: Auto (or anything unknown) becomes
// CC_REMOTE_LANG when set, else the system language.
func Resolve(setting string) Lang {
	if l, ok := Parse(setting); ok && l != Auto {
		return l
	}
	if l, ok := Parse(os.Getenv("CC_REMOTE_LANG")); ok && l != Auto {
		return l
	}
	return Detect()
}

// Detect returns the system language: Chinese for a zh locale, else English.
func Detect() Lang {
	for _, name := range systemLocales() {
		if l, ok := Parse(name); ok && l != Auto {
			return l
		}
		if name != "" && name != "C" && name != "POSIX" && !strings.HasPrefix(name, "C.") {
			return EN // the first locale named decides, as for gettext
		}
	}
	return EN
}

// unixLocales lists the locale variables in gettext's order of precedence.
func unixLocales() []string {
	var out []string
	if v := os.Getenv("LANGUAGE"); v != "" {
		out = append(out, strings.Split(v, ":")[0])
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(key); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Set makes l the language of this process; Auto resolves it first.
func Set(l Lang) {
	if l != EN && l != ZH {
		l = Resolve(string(l))
	}
	current.Store(l)
}

// Current is the language of this process.
func Current() Lang {
	if l, ok := current.Load().(Lang); ok {
		return l
	}
	return EN
}

// T returns the text of the message in this process's language.
func T(en, zh string) string { return Current().T(en, zh) }

// F formats the message in this process's language.
func F(en, zh string, a ...any) string { return Current().F(en, zh, a...) }

// Errorf is fmt.Errorf with the message in this process's language (%w works).
func Errorf(en, zh string, a ...any) error { return Current().Errorf(en, zh, a...) }

// T returns the text of the message in l.
func (l Lang) T(en, zh string) string {
	if l == ZH {
		return zh
	}
	return en
}

// F formats the message in l.
func (l Lang) F(en, zh string, a ...any) string { return fmt.Sprintf(l.T(en, zh), a...) }

// Errorf is fmt.Errorf with the message in l.
func (l Lang) Errorf(en, zh string, a ...any) error { return fmt.Errorf(l.T(en, zh), a...) }

// Error is an error whose text is chosen when it is shown, so a package
// without a request at hand can still answer in the page's language.
type Error struct {
	en, zh string
	args   []any
}

// NewError returns an Error formatting en or zh with args (no %w).
func NewError(en, zh string, a ...any) *Error { return &Error{en, zh, a} }

func (e *Error) Error() string { return e.In(Current()) }

// In is the error's text in l.
func (e *Error) In(l Lang) string { return l.F(e.en, e.zh, e.args...) }

// Text is err's message in l when err chooses its text when shown (In), else err.Error().
func Text(l Lang, err error) string {
	if m, ok := err.(interface{ In(Lang) string }); ok {
		return m.In(l)
	}
	return err.Error()
}

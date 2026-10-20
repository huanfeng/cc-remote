package listen

import (
	"reflect"
	"testing"
)

func TestNormalizeOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://CCR.example.com/": "https://ccr.example.com",
		"http://127.0.0.1:5173":    "http://127.0.0.1:5173",
		"https://ccr.example:443":  "https://ccr.example",
		"http://ccr.example:80":    "http://ccr.example",
		"http://ccr.example:443":   "http://ccr.example:443",
		"ccr.example.com":          "",
		"https://x.example/app":    "",
		"ftp://x.example":          "",
		"https://u@x.example":      "",
	} {
		if got := NormalizeOrigin(in); got != want {
			t.Errorf("NormalizeOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}

// Repeated and comma separated origins come out normalized, once each; an
// empty value clears the list rather than failing.
func TestOrigins(t *testing.T) {
	got, err := Origins([]string{"https://huanfeng.github.io/, https://CCR.example.com", "https://huanfeng.github.io"})
	if err != nil || !reflect.DeepEqual(got, []string{"https://huanfeng.github.io", "https://ccr.example.com"}) {
		t.Fatalf("Origins = %v, %v", got, err)
	}
	if got, err := Origins([]string{""}); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty: %v %v", got, err)
	}
	if _, err := Origins([]string{"https://huanfeng.github.io/cc-remote"}); err == nil {
		t.Fatal("a path is not an origin")
	}
}

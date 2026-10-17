package main

import (
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestIsCCR(t *testing.T) {
	for argv0, want := range map[string]bool{
		"ccr":                          true,
		"/home/me/.local/bin/ccr":      true,
		`C:\bin\CCR.exe`:               true,
		`C:\bin\ccr.cmd`:               true,
		"cc-remote":                    false,
		`C:\bin\cc-remote.exe`:         false,
		"/usr/local/bin/ccr-something": false,
	} {
		if got := isCCR(argv0); got != want {
			t.Errorf("isCCR(%q) = %v, want %v", argv0, got, want)
		}
	}
}

func TestAddrInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, err = net.Listen("tcp", l.Addr().String())
	if err == nil {
		t.Skip("platform allowed a second bind")
	}
	if !addrInUse(fmt.Errorf("listen: %w", err)) {
		t.Fatalf("not recognized as in use: %v", err)
	}
	if addrInUse(errors.New("other")) {
		t.Fatal("false positive")
	}
}

func TestLangArg(t *testing.T) {
	for want, argv := range map[string][]string{
		"en": {"--listen", "x", "--lang", "en", "-h"},
		"zh": {"-lang=zh"},
		"":   {"--", "--lang", "en"},
	} {
		if got := langArg(argv); got != want {
			t.Errorf("langArg(%v) = %q, want %q", argv, got, want)
		}
	}
}

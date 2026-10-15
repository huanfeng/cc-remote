package listen

import (
	"net"
	"reflect"
	"testing"
)

func fakeLAN() []net.IP { return []net.IP{net.ParseIP("192.168.1.10"), net.ParseIP("10.0.0.5")} }

func TestSplit(t *testing.T) {
	got := Split([]string{"127.0.0.1:8787, 192.168.1.10:8787", "", "lan:9000"})
	want := []string{"127.0.0.1:8787", "192.168.1.10:8787", "lan:9000"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestResolveSpecialHosts(t *testing.T) {
	got, added, err := Resolve([]string{"localhost:8787", "lan:8787", "127.0.0.1:8787", "192.168.1.10:8787"}, fakeLAN)
	if err != nil || added != "" {
		t.Fatal(err)
	}
	want := []string{"127.0.0.1:8787", "192.168.1.10:8787", "10.0.0.5:8787"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestResolveWildcardWins(t *testing.T) {
	got, added, err := Resolve([]string{"127.0.0.1:8787", "all:8787", "0.0.0.0:8787", ":8787", "127.0.0.1:9000"}, fakeLAN)
	if err != nil || added != "" {
		t.Fatal(err)
	}
	want := []string{"0.0.0.0:8787", "127.0.0.1:9000"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestResolveErrors(t *testing.T) {
	if _, _, err := Resolve([]string{"127.0.0.1"}, fakeLAN); err == nil {
		t.Fatal("missing port accepted")
	}
	if _, _, err := Resolve([]string{"lan:8787"}, func() []net.IP { return nil }); err == nil {
		t.Fatal("lan without addresses accepted")
	}
}

func TestResolveAddsLoopback(t *testing.T) {
	got, added, err := Resolve([]string{"lan:8787", "192.168.1.10:9000"}, fakeLAN)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.168.1.10:8787", "10.0.0.5:8787", "192.168.1.10:9000", "127.0.0.1:8787"}
	if !reflect.DeepEqual(got, want) || added != "127.0.0.1:8787" {
		t.Fatalf("got %v added %q", got, added)
	}
	// A wildcard or IPv6 loopback already covers local clients.
	for _, specs := range [][]string{{"[::]:8787"}, {"[::1]:8787", "192.168.1.10:8787"}} {
		if _, added, _ := Resolve(specs, fakeLAN); added != "" {
			t.Errorf("%v: unexpected %q", specs, added)
		}
	}
}

func TestProbeURLsOnlyLoopback(t *testing.T) {
	// The token is sent to probe URLs, so non-loopback binds never appear.
	got := ProbeURLs([]string{"192.168.1.10:8787", "127.0.0.1:8787"})
	if want := []string{"http://127.0.0.1:8787"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if got := ProbeURLs([]string{"all:8787"}); !reflect.DeepEqual(got, []string{"http://127.0.0.1:8787"}) {
		t.Fatalf("got %v", got)
	}
	if got := ProbeURLs([]string{"192.168.1.10:9000"}); !reflect.DeepEqual(got, []string{"http://127.0.0.1:9000"}) {
		t.Fatalf("got %v", got)
	}
	if got := ProbeURLs([]string{"[::]:9000"}); !reflect.DeepEqual(got, []string{"http://[::1]:9000"}) {
		t.Fatalf("got %v", got)
	}
}

func TestInsecure(t *testing.T) {
	got := Insecure([]string{"127.0.0.1:1", "lan:2", "all:3", ":4", "0.0.0.0:5", "[::]:6", "192.168.1.2:7", "ALL:8"})
	want := []string{"all:3", ":4", "0.0.0.0:5", "[::]:6", "ALL:8"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{"127.0.0.1:1": true, "localhost:1": true, "[::1]:1": true, "0.0.0.0:1": false, "192.168.1.2:1": false, "lan:1": false} {
		if IsLoopback(addr) != want {
			t.Errorf("%s: want %v", addr, want)
		}
	}
}

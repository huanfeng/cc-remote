package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"cc-remote/internal/i18n"
	"cc-remote/internal/service"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func reachable(addr string) bool {
	c := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := c.Get("http://" + addr + "/")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// A reload adds and removes listeners while the host runs; the attach
// address stays, a failed bind changes nothing, and removing an address
// ends the connections that came in through it.
func TestListenerSetApply(t *testing.T) {
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })}
	t.Cleanup(func() { srv.Close() })
	connGrace = 10 * time.Millisecond
	errs := make(chan error, 1)
	ls := newListenerSet(srv, errs)
	a, b := freeAddr(t), freeAddr(t)
	l, err := listenOn(a)
	if err != nil {
		t.Fatal(err)
	}
	ls.add(a, l)
	ls.keep = a
	ls.serve()
	if !reachable(a) {
		t.Fatal("first listener not served")
	}

	bound, err := ls.apply([]string{a, b, b})
	if err != nil || len(bound) != 2 || !reachable(b) {
		t.Fatalf("add: %v %v", bound, err)
	}
	conn, err := net.Dial("tcp", b)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("keep-alive request: %v", err)
	}
	io.Copy(io.Discard, resp.Body)

	if bound, err = ls.apply([]string{a}); err != nil || !reflect.DeepEqual(bound, []string{a}) {
		t.Fatalf("remove: %v %v", bound, err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := r.ReadByte(); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("connection through a removed address stays open: %v", err)
	}
	if reachable(b) {
		t.Fatal("removed address still served")
	}

	if _, err = ls.apply([]string{b}); !errors.Is(err, errListenRestart) {
		t.Fatalf("dropping the attach address: %v", err)
	}

	// A failed bind leaves the other addresses alone.
	if _, err = ls.apply([]string{a, b}); err != nil {
		t.Fatal(err)
	}
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	if _, err = ls.apply([]string{a, busy.Addr().String()}); err == nil {
		t.Fatal("bind to a busy address succeeded")
	}
	if got := ls.bound(); !slices.Equal(got, []string{a, b}) || !reachable(a) || !reachable(b) {
		t.Fatalf("after a failed reload: %v", got)
	}
	select {
	case err := <-errs:
		t.Fatalf("a removed listener reported %v", err)
	default:
	}
}

func TestRestartSettings(t *testing.T) {
	started := &service.Config{Listen: []string{"127.0.0.1:8787"}, Claude: "/bin/claude", Env: map[string]string{}}
	same := *started
	same.Listen = []string{"lan:8787"}
	same.AllowInsecureRemote = true
	same.Env = nil
	if got := restartSettings(started, &same, "en"); len(got) != 0 {
		t.Fatalf("listen-only change needs a restart: %v", got)
	}
	changed := same
	changed.Claude, changed.AutoLanguage, changed.Env = "/opt/claude", "zh", map[string]string{"HTTPS_PROXY": "http://p:1"}
	if got := restartSettings(started, &changed, "en"); !reflect.DeepEqual(got, []string{"claude", "lang", "env"}) {
		t.Fatalf("restart settings = %v", got)
	}
	changed.Language = "en" // an explicit language wins over the auto one
	if got := restartSettings(started, &changed, "en"); !reflect.DeepEqual(got, []string{"claude", "env"}) {
		t.Fatalf("restart settings = %v", got)
	}
	if got := restartSettings(nil, &same, "en"); !reflect.DeepEqual(got, []string{"service.json"}) {
		t.Fatalf("without a startup config = %v", got)
	}
}

// service.json wins over the registered command line.
func TestApplyServiceConfig(t *testing.T) {
	_, o := hostFlags()
	*o.claudeBin, *o.lang, *o.configDir = "claude", "en", "/old"
	o.listen = stringList{"127.0.0.1:8787"}
	t.Setenv("CC_REMOTE_TEST_PROXY", "")
	t.Setenv("CC_REMOTE_TEST_GONE", "old")
	t.Setenv("CC_REMOTE_SERVICE_MANAGER", "schtasks")
	applyServiceConfig(o, &service.Config{Listen: []string{"lan:9000"}, Claude: "/opt/claude", AutoLanguage: "zh", AllowOrigins: []string{"https://huanfeng.github.io"}, EnvRemoved: []string{"CC_REMOTE_TEST_GONE"}, Env: map[string]string{"CC_REMOTE_TEST_PROXY": "http://p:1", "CC_REMOTE_SERVICE_MANAGER": "x"}})
	if !slices.Equal(o.listen, stringList{"lan:9000"}) || *o.claudeBin != "/opt/claude" || *o.lang != "zh" || *o.configDir != "" || !slices.Equal(o.allowOrigin, stringList{"https://huanfeng.github.io"}) {
		t.Fatalf("options: %v %q %q %q", o.listen, *o.claudeBin, *o.lang, *o.configDir)
	}
	if _, gone := os.LookupEnv("CC_REMOTE_TEST_GONE"); gone || os.Getenv("CC_REMOTE_TEST_PROXY") != "http://p:1" || os.Getenv("CC_REMOTE_SERVICE_MANAGER") != "schtasks" {
		t.Fatal("service environment not applied")
	}
}

func TestConflicts(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.0.0.0:8787", "192.168.1.5:8787", true},
		{"192.168.1.5:8787", "[::]:8787", true},
		{"0.0.0.0:8787", "0.0.0.0:9000", false},
		{"127.0.0.1:8787", "192.168.1.5:8787", false},
	} {
		if got := conflicts(c.a, c.b); got != c.want {
			t.Errorf("conflicts(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

// A reload hands the server the web frontends of service.json, with those of
// CC_REMOTE_ALLOW_ORIGINS, normalized; a bad one fails the reload.
func TestReloadSettingsAllowOrigins(t *testing.T) {
	srv := &http.Server{Handler: http.NotFoundHandler()}
	t.Cleanup(func() { srv.Close() })
	ls := newListenerSet(srv, make(chan error, 1))
	addr := freeAddr(t)
	l, err := listenOn(addr)
	if err != nil {
		t.Fatal(err)
	}
	ls.add(addr, l)
	ls.keep = addr
	ls.serve()
	dir := t.TempDir()
	write := func(origins ...string) {
		b, _ := json.Marshal(service.Config{Listen: []string{addr}, Claude: "claude", AllowOrigins: origins, Env: map[string]string{}})
		if err := os.WriteFile(filepath.Join(dir, "service.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CC_REMOTE_ALLOW_ORIGINS", "https://env.example")
	write("https://HUANFENG.github.io/")
	started := &service.Config{Listen: []string{addr}, Claude: "claude", Env: map[string]string{}}
	res, err := reloadSettings(dir, started, i18n.EN, ls)
	if err != nil || len(res.Restart) != 0 || !slices.Equal(res.AllowOrigins, []string{"https://env.example", "https://huanfeng.github.io"}) {
		t.Fatalf("reload: %+v %v", res, err)
	}
	write("https://huanfeng.github.io/cc-remote")
	if _, err = reloadSettings(dir, started, i18n.EN, ls); err == nil {
		t.Fatal("an origin with a path was accepted")
	}
}

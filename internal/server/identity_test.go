package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

// Several hosts tell apart by name and colour: the page, its icon and the
// app manifest carry them before signing in, and the settings change them.
func TestHostIdentity(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	s.cfg.Assets = fstest.MapFS{
		"index.html":           {Data: []byte(`<!doctype html><html lang="zh-CN"><head><link rel="icon" href="/icon.svg"/><title>CC Remote</title></head></html>`)},
		"icon.svg":             {Data: []byte(`<svg><rect fill="#222c27"/></svg>`)},
		"manifest.webmanifest": {Data: []byte(`{"name":"CC Remote","icons":[{"src":"/icon.svg"}]}`)},
	}
	if code, b := request(t, ts, "PUT", "/api/identity", map[string]string{"name": "pdev <1>", "color": "teal"}, testToken); code != 200 || !strings.Contains(string(b), `"shownColor":"#0f5e5c"`) {
		t.Fatalf("set %d %s", code, b)
	}
	if code, b := request(t, ts, "PUT", "/api/identity", map[string]string{"color": "chartreuse"}, testToken); code != 400 {
		t.Fatalf("bad color %d %s", code, b)
	}
	get := func(path string) string {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	page := get("/s/some-session")
	for _, want := range []string{`data-host-name="pdev &lt;1&gt;"`, `data-host-color="#0f5e5c"`, `<title>pdev &lt;1&gt; · CC Remote</title>`, `href="/host-icon.svg?c=0f5e5c"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %s: %s", want, page)
		}
	}
	if icon := get("/host-icon.svg"); !strings.Contains(icon, `fill="#0f5e5c"`) {
		t.Fatalf("icon %s", icon)
	}
	var m map[string]any
	if json.Unmarshal([]byte(get("/manifest.webmanifest")), &m) != nil || m["short_name"] != "pdev <1>" {
		t.Fatalf("manifest %v", m)
	}
	_, b := request(t, ts, "GET", "/api/info", nil, testToken)
	var info map[string]any
	if json.Unmarshal(b, &info) != nil || info["name"] != "pdev <1>" || info["color"] != "#0f5e5c" {
		t.Fatalf("info %s", b)
	}
}

// The icon is a preset glyph, text (escaped), or the logo; the page and the
// manifest ask for it with the icon in the query, so a change is fetched again.
func TestHostIcon(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	s.cfg.Assets = fstest.MapFS{
		"index.html":           {Data: []byte(`<!doctype html><html lang="zh-CN"><head><link rel="icon" href="/icon.svg"/><title>CC Remote</title></head></html>`)},
		"manifest.webmanifest": {Data: []byte(`{"name":"CC Remote","icons":[{"src":"/icon.svg"}]}`)},
		"host-icons.json":      {Data: []byte(`{"code":"<path d=\"logo\"/>","server":"<rect id=\"server\"/>"}`)},
	}
	get := func(path string) string {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if icon := get("/host-icon.svg"); !strings.Contains(icon, `<path d="logo"/>`) {
		t.Fatalf("default icon %s", icon)
	}
	for _, c := range []struct{ icon, page, svg string }{
		{"server", `href="/host-icon.svg?c=0f5e5c&amp;i=server"`, `<rect id="server"/>`},
		{"<P1>", `data-host-icon="&lt;P1&gt;"`, `font-size="50" fill="#d6ebbc">&lt;P1&gt;</text>`},
		{"👩‍💻", `data-host-icon="👩‍💻"`, `font-size="112"`},
	} {
		if code, b := request(t, ts, "PUT", "/api/identity", map[string]string{"color": "teal", "icon": c.icon}, testToken); code != 200 {
			t.Fatalf("set %q: %d %s", c.icon, code, b)
		}
		if page := get("/"); !strings.Contains(page, c.page) {
			t.Fatalf("%q: page %s", c.icon, page)
		}
		if icon := get("/host-icon.svg"); !strings.Contains(icon, c.svg) {
			t.Fatalf("%q: icon %s", c.icon, icon)
		}
	}
	if code, _ := request(t, ts, "PUT", "/api/identity", map[string]string{"icon": "too long an icon"}, testToken); code != 400 {
		t.Fatalf("long icon accepted: %d", code)
	}
	_, b := request(t, ts, "GET", "/api/info", nil, testToken)
	if !strings.Contains(string(b), `"icon":"👩‍💻"`) {
		t.Fatalf("info %s", b)
	}
}

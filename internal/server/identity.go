package server

import (
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"

	"cc-remote/internal/i18n"
	"cc-remote/internal/identity"
)

// identity is the host's name and colour as set (empty fields: defaults).
func (s *Server) identity() identity.Identity {
	if s.cfg.DataDir == "" {
		return identity.Identity{}
	}
	id, err := identity.Load(s.cfg.DataDir)
	if err != nil {
		log.Printf("identity: %v", err)
	}
	return id
}

type identityBody struct {
	identity.Identity
	// What the page shows: the setting or its default.
	ShownName  string `json:"shownName"`
	ShownColor string `json:"shownColor"`
	// Defaults of empty fields, and the colours to choose from.
	DefaultName  string          `json:"defaultName"`
	DefaultColor string          `json:"defaultColor"`
	Palette      []paletteColour `json:"palette"`
}

type paletteColour struct {
	Key string `json:"key"`
	Hex string `json:"hex"`
}

func (s *Server) identityBody(id identity.Identity) identityBody {
	b := identityBody{Identity: id, DefaultName: identity.DefaultName()}
	b.ShownName, b.ShownColor = id.Resolved()
	b.DefaultColor = identity.DefaultColor(b.ShownName)
	for _, c := range identity.Palette {
		b.Palette = append(b.Palette, paletteColour{c.Key, c.Hex})
	}
	return b
}

// identityAPI reads (GET) or sets (PUT) the host's name and colour.
func (s *Server) identityAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		respond(w, 200, s.identityBody(s.identity()))
		return
	}
	var req identity.Identity
	if !decode(w, r, &req) {
		return
	}
	if s.cfg.DataDir == "" {
		fail(w, 500, s.lang(r).T("The host has no data directory configured", "宿主没有配置数据目录"))
		return
	}
	s.configMu.Lock()
	err := identity.Save(s.cfg.DataDir, req)
	s.configMu.Unlock()
	if err != nil {
		fail(w, 400, i18n.Text(s.lang(r), err))
		return
	}
	respond(w, 200, s.identityBody(s.identity()))
}

// hostIcon draws the host's icon: its glyph (a preset of host-icons.json, the
// logo by default, or text) on its colour. web/src/hostIcon.ts draws the same.
func (s *Server) hostIcon(hex, icon string) string {
	var presets map[string]string
	if b, err := fs.ReadFile(s.cfg.Assets, "host-icons.json"); err == nil {
		_ = json.Unmarshal(b, &presets)
	}
	glyph, ok := presets[icon]
	if icon == "" || ok {
		if glyph == "" {
			glyph = presets["code"]
		}
		if glyph == "" {
			glyph = logoGlyph
		}
		glyph = `<g fill="none" stroke="` + glyphColor + `" stroke-width="12" stroke-linecap="round" stroke-linejoin="round">` + glyph + `</g>`
	} else {
		glyph = fmt.Sprintf(`<text x="96" y="100" text-anchor="middle" dominant-baseline="central" font-family="system-ui,-apple-system,'Segoe UI',sans-serif" font-weight="700" font-size="%d" fill="%s">%s</text>`, iconFontSize(icon), glyphColor, html.EscapeString(icon))
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 192 192"><rect width="192" height="192" rx="44" fill="` + hex + `"/>` + glyph + `</svg>`
}

const (
	glyphColor = "#d6ebbc"
	logoGlyph  = `<path d="M76 60 40 96l36 36M116 60l36 36-36 36"/>`
)

// iconFontSize fits a text icon: the more characters, the smaller. Variation
// selectors, skin tones and what a joiner adds to an emoji take no room of their own.
func iconFontSize(text string) int {
	n, joined := 0, false
	for _, r := range text {
		if r == 0x200d {
			joined = true
		} else if !joined && r != 0xfe0f && r != 0xfe0e && (r < 0x1f3fb || r > 0x1f3ff) {
			n++
		} else {
			joined = false
		}
	}
	switch {
	case n <= 1:
		return 112
	case n == 2:
		return 84
	case n == 3:
		return 64
	}
	return 50
}

// iconQuery is what /host-icon.svg is asked with: it changes with the colour
// and icon, so a browser asks again after they change.
func iconQuery(hex, icon string) string {
	q := "c=" + strings.TrimPrefix(hex, "#")
	if icon != "" {
		q += "&i=" + url.QueryEscape(icon)
	}
	return q
}

// identityAsset serves the files that carry the host's name, colour and icon
// before anyone signs in: the page, its icon and the app manifest.
func (s *Server) identityAsset(w http.ResponseWriter, r *http.Request) bool {
	id := s.identity()
	name, hex := id.Resolved()
	var body, kind string
	switch r.URL.Path {
	case "/", "/index.html":
		b, err := fs.ReadFile(s.cfg.Assets, "index.html")
		if err != nil {
			return false
		}
		lang := map[i18n.Lang]string{i18n.ZH: "zh-CN", i18n.EN: "en"}[s.language()]
		body = strings.NewReplacer(
			`<html lang="zh-CN">`, `<html lang="`+lang+`" data-host-name="`+html.EscapeString(name)+`" data-host-color="`+hex+`" data-host-icon="`+html.EscapeString(id.Icon)+`">`,
			`<title>CC Remote</title>`, `<title>`+html.EscapeString(name)+` · CC Remote</title>`,
			`href="/icon.svg"`, `href="/host-icon.svg?`+html.EscapeString(iconQuery(hex, id.Icon))+`"`,
		).Replace(string(b))
		kind = "text/html; charset=utf-8"
	case "/host-icon.svg":
		body, kind = s.hostIcon(hex, id.Icon), "image/svg+xml"
	case "/manifest.webmanifest":
		b, err := fs.ReadFile(s.cfg.Assets, "manifest.webmanifest")
		if err != nil {
			return false
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			return false
		}
		m["name"], m["short_name"] = name+" · CC Remote", name
		m["lang"] = map[i18n.Lang]string{i18n.ZH: "zh-CN", i18n.EN: "en"}[s.language()]
		if icons, ok := m["icons"].([]any); ok && len(icons) > 0 {
			if icon, ok := icons[0].(map[string]any); ok {
				icon["src"] = "/host-icon.svg?" + iconQuery(hex, id.Icon)
			}
		}
		out, _ := json.Marshal(m)
		body, kind = string(out), "application/manifest+json"
	default:
		return false
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
	return true
}

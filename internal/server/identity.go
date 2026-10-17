package server

import (
	"encoding/json"
	"html"
	"io/fs"
	"log"
	"net/http"
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

// identityAsset serves the files that carry the host's name and colour
// before anyone signs in: the page, its icon and the app manifest.
func (s *Server) identityAsset(w http.ResponseWriter, r *http.Request) bool {
	name, hex := s.identity().Resolved()
	var body, kind string
	switch r.URL.Path {
	case "/", "/index.html":
		b, err := fs.ReadFile(s.cfg.Assets, "index.html")
		if err != nil {
			return false
		}
		lang := map[i18n.Lang]string{i18n.ZH: "zh-CN", i18n.EN: "en"}[s.language()]
		body = strings.NewReplacer(
			`<html lang="zh-CN">`, `<html lang="`+lang+`" data-host-name="`+html.EscapeString(name)+`" data-host-color="`+hex+`">`,
			`<title>CC Remote</title>`, `<title>`+html.EscapeString(name)+` · CC Remote</title>`,
			`href="/icon.svg"`, `href="/host-icon.svg?c=`+strings.TrimPrefix(hex, "#")+`"`,
		).Replace(string(b))
		kind = "text/html; charset=utf-8"
	case "/host-icon.svg":
		b, err := fs.ReadFile(s.cfg.Assets, "icon.svg")
		if err != nil {
			return false
		}
		body, kind = strings.Replace(string(b), `fill="#222c27"`, `fill="`+hex+`"`, 1), "image/svg+xml"
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
				icon["src"] = "/host-icon.svg?c=" + strings.TrimPrefix(hex, "#")
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

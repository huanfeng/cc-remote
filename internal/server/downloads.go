package server

import (
	"crypto/rand"
	"encoding/hex"
	"mime"
	"net/http"
	"path/filepath"
	"sync"
	"time"
)

// Downloads save a file the session named the way the browser saves any file:
// streamed, of any size, resumable. A browser download cannot send the access
// token, so the web first asks for a link (with the token): a random ticket,
// good for downloadTTL, for that one file. Every request checks the file
// against the session's directories again.

const (
	downloadTTL        = 5 * time.Minute
	maxDownloadTickets = 256
)

type downloadTicket struct {
	session, path string
	expires       time.Time
}

type downloadTickets struct {
	mu sync.Mutex
	m  map[string]downloadTicket
}

func (d *downloadTickets) add(t downloadTicket) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = map[string]downloadTicket{}
	}
	now := time.Now()
	for k, v := range d.m {
		if now.After(v.expires) {
			delete(d.m, k)
		}
	}
	for k := range d.m { // still full: drop any
		if len(d.m) < maxDownloadTickets {
			break
		}
		delete(d.m, k)
	}
	d.m[id] = t
	return id
}

// take is get for a ticket good only once.
func (d *downloadTickets) take(id string) (downloadTicket, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.m[id]
	delete(d.m, id)
	if !ok || time.Now().After(t.expires) {
		return downloadTicket{}, false
	}
	return t, true
}

func (d *downloadTickets) get(id string) (downloadTicket, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.m[id]
	if !ok || time.Now().After(t.expires) {
		return downloadTicket{}, false
	}
	return t, true
}

// fileLink is POST /api/sessions/{id}/file/link?path=…&base=…: a download
// link for a file the viewer may open.
func (s *Server) fileLink(w http.ResponseWriter, r *http.Request) {
	f, path, info, ok := s.requestedFile(w, r)
	if !ok {
		return
	}
	f.Close()
	ticket := s.downloads.add(downloadTicket{session: r.PathValue("id"), path: path, expires: time.Now().Add(downloadTTL)})
	respond(w, 200, map[string]any{"url": "/api/download/" + ticket, "name": filepath.Base(path), "size": info.Size()})
}

// download is GET /api/download/{ticket}, without the access token: the
// ticket in the path is the credential.
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	t, ok := s.downloads.get(r.PathValue("ticket"))
	if !ok {
		fail(w, 404, s.lang(r).T("The download link has expired; download the file again from the page", "下载链接已过期，请在页面上重新下载"))
		return
	}
	sess, err := s.store.Get(t.session)
	if err != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	f, path, info, ok := s.sessionFile(w, r, sess, t.path, nil)
	if !ok {
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(path)}))
	http.ServeContent(w, r, "", info.ModTime(), f)
}

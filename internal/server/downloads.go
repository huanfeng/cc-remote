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
// token, so the web first asks for a link (with the token): a random ticket
// for that one file, good for downloadTTL until a download starts. It stays
// good while a download runs, however slow, and for downloadIdle after the
// last one ends, so a download the network broke resumes from where it was;
// none starts after downloadMaxLife, so a leaked link cannot be kept alive.
// Every request checks the file against the session's directories again.

const (
	downloadTTL        = 5 * time.Minute
	downloadIdle       = 30 * time.Minute
	downloadMaxLife    = 24 * time.Hour
	maxDownloadTickets = 256
)

type downloadTicket struct {
	session, path string
	expires       time.Time
	deadline      time.Time // zero: none (the terminal's stream tickets)
	active        int       // downloads running on the ticket
}

func (t downloadTicket) expired(now time.Time) bool {
	return t.active == 0 && now.After(t.expires) || !t.deadline.IsZero() && now.After(t.deadline)
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
		if v.expired(now) {
			delete(d.m, k)
		}
	}
	for k, v := range d.m { // still full: drop any not in use
		if len(d.m) < maxDownloadTickets {
			break
		}
		if v.active == 0 {
			delete(d.m, k)
		}
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
	if !ok || t.expired(time.Now()) {
		return downloadTicket{}, false
	}
	return t, true
}

// begin marks a download running on the ticket until end.
func (d *downloadTickets) begin(id string) (downloadTicket, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.m[id]
	if !ok || t.expired(time.Now()) {
		return downloadTicket{}, false
	}
	t.active++
	d.m[id] = t
	return t, true
}

func (d *downloadTickets) end(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.m[id]
	if !ok {
		return
	}
	t.active--
	if idle := time.Now().Add(downloadIdle); idle.After(t.expires) {
		t.expires = idle
	}
	d.m[id] = t
}

// fileLink is POST /api/sessions/{id}/file/link?path=…&base=…: a download
// link for a file the viewer may open.
func (s *Server) fileLink(w http.ResponseWriter, r *http.Request) {
	f, path, info, ok := s.requestedFile(w, r)
	if !ok {
		return
	}
	f.Close()
	now := time.Now()
	ticket := s.downloads.add(downloadTicket{session: r.PathValue("id"), path: path, expires: now.Add(downloadTTL), deadline: now.Add(downloadMaxLife)})
	respond(w, 200, map[string]any{"url": "/api/download/" + ticket, "name": filepath.Base(path), "size": info.Size()})
}

// download is GET /api/download/{ticket}, without the access token: the
// ticket in the path is the credential.
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("ticket")
	t, ok := s.downloads.begin(id)
	if !ok {
		fail(w, 404, s.lang(r).T("The download link has expired; download the file again from the page", "下载链接已过期，请在页面上重新下载"))
		return
	}
	defer s.downloads.end(id)
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

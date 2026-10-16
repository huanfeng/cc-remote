package server

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"
)

// restartPoll is how often a pending restart checks whether the host is idle.
var restartPoll = 3 * time.Second

// localAdmin allows a request only from this machine, directly: a leaked
// token used remotely (or through a local reverse proxy) cannot stop the host.
func localAdmin(w http.ResponseWriter, r *http.Request, action string) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		fail(w, 403, "仅允许本机"+action)
		return false
	}
	for name := range r.Header {
		if slices.Contains(proxyHeaders, name) || strings.HasPrefix(name, "X-Forwarded-") {
			fail(w, 403, "仅允许本机直接"+action+"，不接受经反向代理转发的请求")
			return false
		}
	}
	return true
}

// busySessions names the sessions a restart would interrupt: a turn running
// or waiting for an answer, or background tasks still running.
func (s *Server) busySessions() []string {
	list, err := s.store.List()
	if err != nil {
		return []string{"（无法读取会话）"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var busy []string
	for _, x := range list {
		tasks := false
		if rt := s.procs[x.ID]; rt != nil && rt.proc != nil {
			tasks = rt.proc.BackgroundTasks() > 0
		}
		if x.Status == "running" || x.Status == "waiting" || tasks {
			busy = append(busy, x.Title)
		}
	}
	return busy
}

func (s *Server) pendingRestart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restartPending
}

// restart is POST /api/admin/restart {"when":"idle"|"now"}: the host exits
// with a restart code, and the service manager starts it again (running a
// newly installed binary). With "idle" it first waits until no session is busy.
func (s *Server) restart(w http.ResponseWriter, r *http.Request) {
	if !localAdmin(w, r, "重启宿主") {
		return
	}
	if s.cfg.Restart == nil {
		fail(w, 501, "当前宿主不是作为服务运行，无法自动重启")
		return
	}
	var req struct {
		When string `json:"when"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
	busy := s.busySessions()
	s.mu.Lock()
	already := s.restartPending
	s.restartPending = true
	s.mu.Unlock()
	if req.When == "now" || len(busy) == 0 {
		respond(w, 202, map[string]any{"status": "restarting", "busy": busy})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		log.Printf("Restart requested")
		go s.cfg.Restart()
		return
	}
	respond(w, 202, map[string]any{"status": "waiting", "busy": busy})
	if already {
		return
	}
	log.Printf("Restart requested once %d busy session(s) are idle", len(busy))
	go func() {
		for {
			time.Sleep(restartPoll)
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			if closing {
				return
			}
			if len(s.busySessions()) == 0 {
				log.Printf("All sessions idle; restarting for the update")
				s.cfg.Restart()
				return
			}
		}
	}()
}

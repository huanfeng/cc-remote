package server

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/store"
)

type claudeVersion struct {
	Executable string `json:"executable"`
	Path       string `json:"path"`
	Version    string `json:"version"`
	Error      string `json:"error,omitempty"`
	checked    time.Time
}

// The status panel polls every few seconds; spawning claude --version each time is too costly.
// This mutex is never s.mu: a slow first query only delays other status readers.
var versionCache struct {
	sync.Mutex
	value claudeVersion
}

func (s *Server) claudeVersion() claudeVersion {
	versionCache.Lock()
	defer versionCache.Unlock()
	v := versionCache.value
	if v.Executable == s.cfg.ClaudeExecutable && time.Since(v.checked) < 5*time.Minute {
		return v
	}
	v = claudeVersion{Executable: s.cfg.ClaudeExecutable, checked: time.Now()}
	path, err := exec.LookPath(s.cfg.ClaudeExecutable)
	if err == nil {
		v.Path = path
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var b []byte
		b, err = exec.CommandContext(ctx, path, "--version").Output()
		cancel()
		v.Version = strings.TrimSpace(string(b))
	}
	if err != nil {
		v.Error = err.Error()
	}
	versionCache.value = v
	return v
}

// Insecure marks a non-loopback listener: the host serves plain HTTP, so the
// access token crosses the network in clear text.
type listener struct {
	Address  string `json:"address"`
	Loopback bool   `json:"loopback"`
	Insecure bool   `json:"insecure"`
}

func listeners(raw string) ([]string, []listener) {
	addrs, out := []string{}, []listener{}
	for _, a := range strings.Split(raw, ",") {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		host, _, err := net.SplitHostPort(a)
		if err != nil {
			host = a
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		addrs = append(addrs, a)
		loopback := strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
		out = append(out, listener{a, loopback, !loopback})
	}
	return addrs, out
}

func fileSize(path string) int64 {
	if f, err := os.Stat(path); err == nil && f.Mode().IsRegular() {
		return f.Size()
	}
	return 0
}
func dirSize(root string) int64 {
	var n int64
	if root == "" {
		return 0
	}
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func (s *Server) logPath() string {
	if s.cfg.LogFile != "" {
		return s.cfg.LogFile
	}
	if s.cfg.DataDir != "" {
		return filepath.Join(s.cfg.DataDir, "host.log")
	}
	return ""
}

var secretPattern = regexp.MustCompile(`(?i)(bearer\s+)\S+|\b[0-9a-f]{64}\b`)

// logTail reads at most the last 64 KiB and never returns the access token.
func logTail(path, token string, lines int) []string {
	out := []string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return out
	}
	start := max(0, info.Size()-64<<10)
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return out
	}
	b, _ := io.ReadAll(io.LimitReader(f, 64<<10))
	if start > 0 {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if token != "" {
		text = strings.ReplaceAll(text, token, "[redacted]")
	}
	text = secretPattern.ReplaceAllStringFunc(text, func(m string) string {
		if p := secretPattern.FindStringSubmatch(m); p[1] != "" {
			return p[1] + "[redacted]"
		}
		return "[redacted]"
	})
	all := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(all) == 1 && all[0] == "" {
		return out
	}
	return append(out, all[max(0, len(all)-lines):]...)
}

type sessionStatus struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Mode   string `json:"mode"`
	Status string `json:"status"`
	Online bool   `json:"online"`
	Cwd    string `json:"cwd"`
	store.SessionStats
}

const statusTTL = 5 * time.Second

// statusCache keeps the expensive parts of /api/status (usage aggregation and the
// uploads directory walk) for statusTTL, so polling clients cost one scan per window.
type statusCache struct {
	mu           sync.Mutex
	at           time.Time
	stats        []store.SessionStats
	daily        []store.DailyStats
	uploadsBytes int64
}

func (s *Server) cachedStatus() ([]store.SessionStats, []store.DailyStats, int64, error) {
	c := &s.statusCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < statusTTL {
		return c.stats, c.daily, c.uploadsBytes, nil
	}
	stats, err := s.store.AllSessionStats()
	if err != nil {
		return nil, nil, 0, err
	}
	daily, err := s.store.DailyStats(14, time.Now())
	if err != nil {
		return nil, nil, 0, err
	}
	c.stats, c.daily, c.uploadsBytes, c.at = stats, daily, dirSize(s.cfg.UploadDir), time.Now()
	return c.stats, c.daily, c.uploadsBytes, nil
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.List()
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not read sessions", "无法读取会话"))
		return
	}
	stats, daily, uploadsBytes, err := s.cachedStatus()
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not count sessions", "无法统计会话"))
		return
	}
	byID := map[string]store.SessionStats{}
	for _, x := range stats {
		byID[x.SessionID] = x
	}
	counts := map[string]int{"total": len(sessions), "managed": 0, "terminal": 0, "running": 0, "waiting": 0, "idle": 0}
	rows := make([]sessionStatus, 0, len(sessions))
	totals := store.SessionStats{}
	s.mu.Lock()
	pendingRequests, activeRuns := len(s.requests), len(s.runs)
	for i := range sessions {
		s.decorate(&sessions[i])
	}
	s.mu.Unlock()
	for _, x := range sessions {
		if x.Mode == "terminal" {
			counts["terminal"]++
		} else {
			counts["managed"]++
		}
		switch x.Status {
		case "running", "waiting":
			counts[x.Status]++
		default:
			counts["idle"]++
		}
		st, ok := byID[x.ID]
		if !ok {
			st = store.SessionStats{SessionID: x.ID}
		}
		if st.LastActivityAt == "" {
			st.LastActivityAt = x.UpdatedAt
		}
		rows = append(rows, sessionStatus{x.ID, x.Title, x.Mode, x.Status, x.Online, x.Cwd, st})
		totals.Turns += st.Turns
		totals.Results += st.Results
		totals.CostUSD += st.CostUSD
		totals.CostKnown = totals.CostKnown || st.CostKnown
		totals.InputTokens += st.InputTokens
		totals.OutputTokens += st.OutputTokens
		totals.CacheReadTokens += st.CacheReadTokens
		totals.CacheCreationTokens += st.CacheCreationTokens
		totals.DurationMs += st.DurationMs
		totals.ToolCalls += st.ToolCalls
		totals.Errors += st.Errors
		if st.LastActivityAt > totals.LastActivityAt {
			totals.LastActivityAt = st.LastActivityAt
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].LastActivityAt > rows[j].LastActivityAt })
	db := filepath.Join(s.cfg.DataDir, "cc-remote.sqlite")
	dbBytes := int64(0)
	if s.cfg.DataDir != "" {
		dbBytes = fileSize(db) + fileSize(db+"-wal") + fileSize(db+"-shm")
	}
	logFile := s.logPath()
	s.mu.Lock()
	bound := s.listen
	s.mu.Unlock()
	addrs, ls := listeners(bound)
	manager := os.Getenv("CC_REMOTE_SERVICE_MANAGER")
	uptime := int64(0)
	startedAt := ""
	if !s.cfg.StartedAt.IsZero() {
		uptime = int64(time.Since(s.cfg.StartedAt).Seconds())
		startedAt = s.cfg.StartedAt.UTC().Format(time.RFC3339)
	}
	respond(w, 200, map[string]any{
		"version": s.cfg.Version, "pid": os.Getpid(), "startedAt": startedAt, "uptimeSeconds": uptime,
		"listen": bound, "listenAddresses": addrs, "listeners": ls,
		"dataDir": s.cfg.DataDir, "claudeConfigDir": s.cfg.ClaudeConfigDir, "logFile": logFile,
		"goVersion": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"claude":          s.claudeVersion(),
		"service":         map[string]any{"managed": manager != "", "manager": manager},
		"sessions":        counts,
		"pendingRequests": pendingRequests, "activeRuns": activeRuns,
		"storage": map[string]int64{"databaseBytes": dbBytes, "uploadsBytes": uploadsBytes, "logBytes": fileSize(logFile)},
		"totals":  totals, "daily": daily, "sessionStats": rows,
		"logTail": logTail(logFile, s.cfg.Token, 80),
	})
}

func (s *Server) sessionStats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.Get(id); err != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	x, err := s.store.SessionStatsFor(id)
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not count sessions", "无法统计会话"))
		return
	}
	respond(w, 200, x)
}

// proxyHeaders indicate a request relayed by a (possibly local) reverse proxy,
// whose loopback RemoteAddr says nothing about the real client.
// Canonical header names; any X-Forwarded-* header is rejected as well.
var proxyHeaders = []string{"Forwarded", "X-Real-Ip", "Via"}

// shutdown is loopback-only in addition to bearer authentication, so a leaked
// token used remotely cannot stop the host.
func (s *Server) shutdown(w http.ResponseWriter, r *http.Request) {
	if l := s.lang(r); !localAdmin(w, r, l, l.T("shut down the host", "关闭宿主")) {
		return
	}
	if s.cfg.Shutdown == nil {
		fail(w, 501, s.lang(r).T("Remote shutdown is not enabled on this host", "当前宿主未启用远程关闭"))
		return
	}
	respond(w, 202, map[string]string{"status": "shutting down"})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go s.cfg.Shutdown()
}

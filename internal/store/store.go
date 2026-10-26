package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"cc-remote/internal/i18n"

	_ "modernc.org/sqlite"
)

type Session struct {
	Provider                  string `json:"provider"`
	ProfileID                 string `json:"profileId"`
	ProfileName               string `json:"profileName"`
	ProfileJSON               string `json:"-"`
	ID                        string `json:"id"`
	Title                     string `json:"title"`
	Cwd                       string `json:"cwd"`
	ClaudeSessionID           string `json:"claudeSessionId"`
	Status                    string `json:"status"`
	CreatedAt                 string `json:"createdAt"`
	UpdatedAt                 string `json:"updatedAt"`
	LastConversationAt        string `json:"lastConversationAt,omitempty"`
	ConversationTimeEstimated bool   `json:"conversationTimeEstimated,omitempty"`
	Resume                    bool   `json:"-"`
	Mode                      string `json:"mode"`
	Online                    bool   `json:"online"`
	// Per-session launch overrides; empty follows cc-remote preferences.
	PermissionMode string `json:"permissionMode"`
	Model          string `json:"model"`
	Effort         string `json:"effort"`
	// ClaudeArgs are Claude Code options a terminal session starts with (ccr [claude options]).
	ClaudeArgs []string `json:"claudeArgs,omitempty"`
	// Queued holds web messages waiting for Claude (filled in by the server).
	Queued []QueuedMessage `json:"queued,omitempty"`
	// Attached counts the terminals attached to a terminal session (filled in by the server).
	Attached int `json:"attached,omitempty"`
	// Live is a terminal session's status line data (filled in by the server).
	Live *LiveStatus `json:"live,omitempty"`
	// ActiveTasks counts background tasks (subagents, background commands)
	// still running, also after the turn that started them ended (filled in
	// by the server).
	ActiveTasks int `json:"activeTasks,omitempty"`
}

// LiveStatus is what Claude Code reports to its status line, and the spinner
// line while it works.
type LiveStatus struct {
	Model          string  `json:"model,omitempty"`
	ModelID        string  `json:"modelId,omitempty"`
	Effort         string  `json:"effort,omitempty"`
	CostUSD        float64 `json:"costUSD"`
	DurationMS     int64   `json:"durationMs"`
	ContextTokens  int     `json:"contextTokens"`
	ContextSize    int     `json:"contextSize"`
	ContextPercent float64 `json:"contextPercent"`
	Activity       string  `json:"activity,omitempty"`
	Suggestion     string  `json:"suggestion,omitempty"` // Claude Code's suggested next prompt
}

// QueuedMessage is a sent message Claude has not started yet; it becomes a
// "user" event with the same text and payload when its turn starts.
type QueuedMessage struct {
	Text    string          `json:"text"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Event struct {
	Seq       int64           `json:"seq"`
	SessionID string          `json:"sessionId"`
	Kind      string          `json:"kind"`
	Text      string          `json:"text"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"createdAt"`
	Truncated bool            `json:"truncated,omitempty"` // long tool text was cut for a listing
}

type Interaction struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	Kind      string          `json:"kind"`
	ToolName  string          `json:"toolName"`
	Input     json.RawMessage `json:"input"`
	Status    string          `json:"status"`
	CreatedAt string          `json:"createdAt"`
}

type Store struct{ db *sql.DB }

// ResetContext retains events while starting a fresh managed Claude conversation.
func (s *Store) ResetContext(id string) error {
	r, err := s.db.Exec(`UPDATE sessions SET claude_id=?,resume=0,status='idle',updated_at=? WHERE id=? AND mode='managed'`, ID(), now(), id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return errors.New(i18n.T("managed session not found", "找不到托管会话"))
	}
	return err
}

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

const sessionSchema = `id TEXT PRIMARY KEY,title TEXT NOT NULL,cwd TEXT NOT NULL,claude_id TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,resume INTEGER NOT NULL DEFAULT 0,mode TEXT NOT NULL DEFAULT 'managed',permission_mode TEXT NOT NULL DEFAULT '',model TEXT NOT NULL DEFAULT '',effort TEXT NOT NULL DEFAULT '',claude_args TEXT NOT NULL DEFAULT '[]',provider TEXT NOT NULL DEFAULT 'claude',profile_id TEXT NOT NULL DEFAULT '',profile_json TEXT NOT NULL DEFAULT '',UNIQUE(provider,claude_id)`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS sessions(` + sessionSchema + `);
CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT,session_id TEXT NOT NULL REFERENCES sessions(id),kind TEXT NOT NULL,text TEXT NOT NULL,payload TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS events_session ON events(session_id,seq);
CREATE INDEX IF NOT EXISTS events_tasks ON events(session_id,seq) WHERE kind='status' AND text LIKE 'task_%';
CREATE TABLE IF NOT EXISTS interactions(id TEXT PRIMARY KEY,session_id TEXT NOT NULL REFERENCES sessions(id),kind TEXT NOT NULL,tool_name TEXT NOT NULL,input TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS interactions_session ON interactions(session_id,status);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	for _, c := range []struct{ name, def string }{{"mode", "'managed'"}, {"permission_mode", "''"}, {"model", "''"}, {"effort", "''"}, {"claude_args", "'[]'"}, {"provider", "'claude'"}, {"profile_id", "''"}, {"profile_json", "''"}} {
		var n int
		if err = db.QueryRow(`SELECT count(*) FROM pragma_table_info('sessions') WHERE name=?`, c.name).Scan(&n); err == nil && n == 0 {
			_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN ` + c.name + ` TEXT NOT NULL DEFAULT ` + c.def)
		}
		if err != nil {
			db.Close()
			return nil, err
		}
	}
	if err = migrateNativeIdentity(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS usage_samples(at TEXT PRIMARY KEY,data TEXT NOT NULL)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS channel_transcript(session_id TEXT NOT NULL REFERENCES sessions(id),event_key TEXT NOT NULL,PRIMARY KEY(session_id,event_key))`); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateSharedSessions(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateConversationActivity(db); err != nil {
		db.Close()
		return nil, err
	}
	// Sessions synced before transcripts carried task and teammate events
	// are flagged once, so their first sync does not append those of the
	// whole history (see TranscriptFlag).
	if _, err = db.Exec(`INSERT OR IGNORE INTO channel_transcript SELECT DISTINCT session_id,? FROM channel_transcript c WHERE NOT EXISTS (SELECT 1 FROM channel_transcript v WHERE v.session_id=c.session_id AND v.event_key=?)`, LegacyTasksKey, TranscriptV2Key); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`INSERT OR IGNORE INTO channel_transcript SELECT DISTINCT session_id,? FROM channel_transcript`, TranscriptV2Key); err != nil {
		db.Close()
		return nil, err
	}
	// Process-local execution cannot survive a host restart. Never replay it automatically.
	note := i18n.T("The host restarted and interrupted the last turn. Check the workspace before continuing.", "宿主已重启，上一轮执行中断。请检查工作区后再继续。")
	_, err = db.Exec(`INSERT INTO events(session_id,kind,text,payload,created_at) SELECT id,'error',?,'null',? FROM sessions WHERE status IN ('running','waiting')`, note, now())
	if err == nil {
		_, err = db.Exec(`UPDATE sessions SET status='interrupted' WHERE status IN ('running','waiting'); UPDATE interactions SET status='expired' WHERE status='pending';`)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrateNativeIdentity replaces the old inline UNIQUE(claude_id) constraint.
// Open runs without concurrent users on one connection. Disable foreign keys
// before the transaction so replacing the parent preserves its child tables.
func migrateNativeIdentity(db *sql.DB) (err error) {
	var oldUnique int
	if err = db.QueryRow(`SELECT count(*) FROM pragma_index_list('sessions') AS i
WHERE i."unique"=1 AND (SELECT count(*) FROM pragma_index_info(i.name))=1
AND EXISTS (SELECT 1 FROM pragma_index_info(i.name) WHERE name='claude_id')`).Scan(&oldUnique); err != nil || oldUnique == 0 {
		return err
	}
	if _, err = db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer func() {
		_, enableErr := db.Exec(`PRAGMA foreign_keys=ON`)
		err = errors.Join(err, enableErr)
	}()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE sessions_native_identity(` + sessionSchema + `);
INSERT INTO sessions_native_identity (` + sessionColumns + `) SELECT ` + sessionColumns + ` FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_native_identity RENAME TO sessions;`); err != nil {
		return err
	}
	var table, parent string
	var rowID sql.NullInt64
	var key int
	if err = tx.QueryRow(`PRAGMA foreign_key_check`).Scan(&table, &rowID, &parent, &key); err == nil {
		return errors.New("foreign key violation while migrating native session identity")
	} else if err != sql.ErrNoRows {
		return err
	}
	return tx.Commit()
}

// migrateSharedSessions turns sessions of the retired shared mode ("channel")
// into stopped terminal sessions, which resume the same Claude session. Their
// recorded transcript keys get the terminal sessions' Claude session ID prefix,
// so the history is not recorded a second time.
func migrateSharedSessions(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE channel_transcript SET event_key=(SELECT lower(claude_id) FROM sessions WHERE sessions.id=channel_transcript.session_id)||':'||event_key WHERE session_id IN (SELECT id FROM sessions WHERE mode='channel')`); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE sessions SET mode='terminal',status='idle' WHERE mode='channel'`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Create(title, cwd, claudeID string, resume bool) (Session, error) {
	return s.CreateProvider(title, cwd, claudeID, resume, "claude")
}

func (s *Store) CreateProvider(title, cwd, nativeID string, resume bool, provider string) (Session, error) {
	if provider != "claude" && provider != "codex" {
		return Session{}, errors.New("invalid session provider")
	}
	x := Session{ID: ID(), Title: title, Cwd: cwd, ClaudeSessionID: nativeID, Status: "idle", CreatedAt: now(), Resume: resume, Mode: "managed", Provider: provider}
	x.UpdatedAt = x.CreatedAt
	_, err := s.db.Exec(`INSERT INTO sessions(id,title,cwd,claude_id,status,created_at,updated_at,resume,provider) VALUES(?,?,?,?,?,?,?,?,?)`, x.ID, x.Title, x.Cwd, x.ClaudeSessionID, x.Status, x.CreatedAt, x.UpdatedAt, x.Resume, x.Provider)
	return x, err
}

const sessionColumns = `id,title,cwd,claude_id,status,created_at,updated_at,resume,mode,permission_mode,model,effort,claude_args,provider,profile_id,profile_json`

const sessionReadColumns = sessionColumns + `,COALESCE((SELECT last_at FROM session_activity WHERE session_id=sessions.id),''),COALESCE((SELECT estimated FROM session_activity WHERE session_id=sessions.id),0)`

func scanSession(r interface{ Scan(...any) error }) (Session, error) {
	var x Session
	var args string
	err := r.Scan(&x.ID, &x.Title, &x.Cwd, &x.ClaudeSessionID, &x.Status, &x.CreatedAt, &x.UpdatedAt, &x.Resume, &x.Mode, &x.PermissionMode, &x.Model, &x.Effort, &args, &x.Provider, &x.ProfileID, &x.ProfileJSON, &x.LastConversationAt, &x.ConversationTimeEstimated)
	if err == nil {
		_ = json.Unmarshal([]byte(args), &x.ClaudeArgs)
		var profile struct {
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(x.ProfileJSON), &profile) == nil {
			x.ProfileName = profile.Name
		}
	}
	return x, err
}

func (s *Store) Get(id string) (Session, error) {
	return scanSession(s.db.QueryRow(`SELECT `+sessionReadColumns+` FROM sessions WHERE id=?`, id))
}
func (s *Store) FindClaude(id string) (Session, error) {
	return s.FindNative("claude", id)
}

func (s *Store) FindNative(provider, id string) (Session, error) {
	return scanSession(s.db.QueryRow(`SELECT `+sessionReadColumns+` FROM sessions WHERE provider=? AND claude_id=?`, provider, id))
}

func (s *Store) List() ([]Session, error) {
	r, err := s.db.Query(`SELECT ` + sessionReadColumns + ` FROM sessions ORDER BY julianday(COALESCE(NULLIF((SELECT last_at FROM session_activity WHERE session_id=sessions.id),''),created_at)) DESC,id`)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := []Session{}
	for r.Next() {
		x, e := scanSession(r)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, r.Err()
}

func (s *Store) SetStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE sessions SET status=?,updated_at=? WHERE id=?`, status, now(), id)
	return err
}
func (s *Store) SetResume(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET resume=1 WHERE id=?`, id)
	return err
}

// SetClaudeID follows a session to a new Claude session (/clear, /resume). It
// keeps updated_at, which the web reads as "the status was set again".
func (s *Store) SetClaudeID(id, claudeID string) error {
	_, err := s.db.Exec(`UPDATE sessions SET claude_id=? WHERE id=?`, claudeID, id)
	return err
}

// SetClaudeArgs stores the Claude Code options a terminal session starts with.
func (s *Store) SetClaudeArgs(id string, args []string) error {
	if args == nil {
		args = []string{}
	}
	b, _ := json.Marshal(args)
	_, err := s.db.Exec(`UPDATE sessions SET claude_args=? WHERE id=?`, string(b), id)
	return err
}

func (s *Store) SetMode(id, mode string) error {
	_, err := s.db.Exec(`UPDATE sessions SET mode=?,updated_at=? WHERE id=?`, mode, now(), id)
	return err
}

// SetOverrides stores per-session launch overrides used by the next managed turn.
func (s *Store) SetOverrides(id, permissionMode, model, effort string) error {
	_, err := s.db.Exec(`UPDATE sessions SET permission_mode=?,model=?,effort=? WHERE id=?`, permissionMode, model, effort, id)
	return err
}
func (s *Store) SetTitle(id, title string) error {
	_, err := s.db.Exec(`UPDATE sessions SET title=? WHERE id=?`, title, id)
	return err
}

// Delete removes a session with its events, interactions and transcript keys in
// one transaction. It returns sql.ErrNoRows when the session does not exist.
func (s *Store) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"events", "interactions", "channel_transcript", "session_activity"} {
		if _, err = tx.Exec(`DELETE FROM `+table+` WHERE session_id=?`, id); err != nil {
			return err
		}
	}
	r, err := tx.Exec(`DELETE FROM sessions WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, err := r.RowsAffected(); err != nil || n == 0 {
		if err == nil {
			err = sql.ErrNoRows
		}
		return err
	}
	return tx.Commit()
}

// RecordTranscript keeps repeated transcript scans idempotent.
func (s *Store) RecordTranscript(id, key, kind, text string, payload json.RawMessage) (bool, error) {
	return s.RecordTranscriptAt(id, key, kind, text, payload, now())
}

// RecordTranscriptAt preserves the source clock; missing timestamps stay unknown.
func (s *Store) RecordTranscriptAt(id, key, kind, text string, payload json.RawMessage, at string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`INSERT OR IGNORE INTO channel_transcript VALUES(?,?)`, id, key)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`null`)
	}
	sourceAt := conversationTimestamp(at)
	storedAt := sourceAt
	if storedAt == "" {
		storedAt = now()
	}
	_, err = tx.Exec(`INSERT INTO events(session_id,kind,text,payload,created_at) VALUES(?,?,?,?,?)`, id, kind, text, string(payload), storedAt)
	if err != nil {
		return false, err
	}
	if err = recordConversation(tx, id, kind, text, sourceAt); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Transcript flags live among a session's recorded transcript keys, which
// never start with "!". TranscriptV2Key marks a session synced by a host that
// records task and teammate events; LegacyTasksKey one synced before, whose
// next sync only registers those events (they are its old history).
const (
	TranscriptV2Key = "!transcript-v2"
	LegacyTasksKey  = "!legacy-tasks"
)

// MarkTranscript registers a transcript key without recording an event.
func (s *Store) MarkTranscript(id, key string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO channel_transcript VALUES(?,?)`, id, key)
	return err
}

// TranscriptFlag reports whether a session has the flag key.
func (s *Store) TranscriptFlag(id, key string) bool {
	var n int
	_ = s.db.QueryRow(`SELECT count(*) FROM channel_transcript WHERE session_id=? AND event_key=?`, id, key).Scan(&n)
	return n > 0
}

// ClearTranscriptFlag removes a flag key.
func (s *Store) ClearTranscriptFlag(id, key string) error {
	_, err := s.db.Exec(`DELETE FROM channel_transcript WHERE session_id=? AND event_key=?`, id, key)
	return err
}

func (s *Store) Append(id, kind, text string, payload json.RawMessage) (Event, error) {
	return s.AppendAt(id, kind, text, payload, now())
}

// AppendAt records native history without treating import time as conversation time.
func (s *Store) AppendAt(id, kind, text string, payload json.RawMessage, at string) (Event, error) {
	if len(payload) == 0 || !json.Valid(payload) {
		payload = json.RawMessage(`null`)
	}
	sourceAt := conversationTimestamp(at)
	storedAt := sourceAt
	if storedAt == "" {
		storedAt = now()
	}
	x := Event{SessionID: id, Kind: kind, Text: text, Payload: payload, CreatedAt: storedAt}
	tx, err := s.db.Begin()
	if err != nil {
		return x, err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`INSERT INTO events(session_id,kind,text,payload,created_at) VALUES(?,?,?,?,?)`, id, kind, text, string(payload), x.CreatedAt)
	if err != nil {
		return x, err
	}
	x.Seq, err = r.LastInsertId()
	if err != nil {
		return x, err
	}
	if err = recordConversation(tx, id, kind, text, sourceAt); err != nil {
		return x, err
	}
	return x, tx.Commit()
}

// LatestPayload returns the payload of the newest event of kind whose payload
// contains marker, in session id or, when id is empty, in any session.
func (s *Store) LatestPayload(id, kind, marker string) (json.RawMessage, bool, error) {
	q := `SELECT payload FROM events WHERE kind=? AND instr(payload,?)>0`
	args := []any{kind, marker}
	if id != "" {
		q += ` AND session_id=?`
		args = append(args, id)
	}
	var raw string
	err := s.db.QueryRow(q+` ORDER BY seq DESC LIMIT 1`, args...).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(raw), true, nil
}

// TaskEvents returns a session's task_* system events (subtype in text),
// oldest first. The condition matches the events_tasks partial index.
func (s *Store) TaskEvents(id string) ([]Event, error) {
	r, err := s.db.Query(`SELECT seq,session_id,kind,text,payload,created_at FROM events WHERE session_id=? AND kind='status' AND text LIKE 'task_%' ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	return scanEvents(r)
}

func (s *Store) Events(id string, after int64) ([]Event, error) {
	r, err := s.db.Query(`SELECT seq,session_id,kind,text,payload,created_at FROM events WHERE session_id=? AND seq>? ORDER BY seq LIMIT 10000`, id, after)
	if err != nil {
		return nil, err
	}
	return scanEvents(r)
}

// EventsPage returns about limit of the newest events before seq `before`
// (0: the latest), and whether older ones exist. Within `reach` events further
// back the page starts at a user message, or else at the nearest event that is
// not a tool result, so a tool call and its result stay on one page.
func (s *Store) EventsPage(id string, before int64, limit, reach int) ([]Event, bool, error) {
	if before <= 0 {
		before = math.MaxInt64
	}
	nth := func(offset int) (int64, error) {
		var seq int64
		err := s.db.QueryRow(`SELECT seq FROM events WHERE session_id=? AND seq<? ORDER BY seq DESC LIMIT 1 OFFSET ?`, id, before, offset).Scan(&seq)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return seq, err
	}
	start, err := nth(limit - 1)
	if err != nil {
		return nil, false, err
	}
	if start > 0 {
		floor, err := nth(limit - 1 + reach)
		if err != nil {
			return nil, false, err
		}
		var user, call sql.NullInt64
		if err := s.db.QueryRow(`SELECT MAX(CASE WHEN kind='user' THEN seq END), MAX(CASE WHEN kind<>'tool_result' THEN seq END) FROM events WHERE session_id=? AND seq<=? AND seq>=?`, id, start, floor).Scan(&user, &call); err != nil {
			return nil, false, err
		}
		if user.Valid {
			start = user.Int64
		} else if call.Valid {
			start = call.Int64
		}
	}
	r, err := s.db.Query(`SELECT seq,session_id,kind,text,payload,created_at FROM events WHERE session_id=? AND seq>=? AND seq<? ORDER BY seq`, id, start, before)
	if err != nil {
		return nil, false, err
	}
	out, err := scanEvents(r)
	if err != nil || start == 0 {
		return out, false, err
	}
	var older bool
	err = s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM events WHERE session_id=? AND seq<?)`, id, start).Scan(&older)
	return out, older, err
}

// Event returns one event of a session.
func (s *Store) Event(id string, seq int64) (Event, error) {
	r, err := s.db.Query(`SELECT seq,session_id,kind,text,payload,created_at FROM events WHERE session_id=? AND seq=?`, id, seq)
	if err != nil {
		return Event{}, err
	}
	out, err := scanEvents(r)
	if err != nil {
		return Event{}, err
	}
	if len(out) == 0 {
		return Event{}, sql.ErrNoRows
	}
	return out[0], nil
}

func scanEvents(r *sql.Rows) ([]Event, error) {
	defer r.Close()
	out := []Event{}
	for r.Next() {
		var x Event
		var raw string
		if err := r.Scan(&x.Seq, &x.SessionID, &x.Kind, &x.Text, &raw, &x.CreatedAt); err != nil {
			return nil, err
		}
		x.Payload = json.RawMessage(raw)
		out = append(out, x)
	}
	return out, r.Err()
}

func (s *Store) AddInteraction(x Interaction) error {
	_, err := s.db.Exec(`INSERT INTO interactions VALUES(?,?,?,?,?,?,?)`, x.ID, x.SessionID, x.Kind, x.ToolName, string(x.Input), "pending", now())
	return err
}
func (s *Store) Resolve(id, status string) error {
	r, err := s.db.Exec(`UPDATE interactions SET status=? WHERE id=? AND status='pending'`, status, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return i18n.NewError("request already resolved", "该请求已处理")
	}
	return nil
}
func (s *Store) Pending(session string) ([]Interaction, error) {
	q := `SELECT * FROM interactions WHERE status='pending'`
	args := []any{}
	if session != "" {
		q += ` AND session_id=?`
		args = append(args, session)
	}
	q += ` ORDER BY created_at`
	r, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := []Interaction{}
	for r.Next() {
		var x Interaction
		var raw string
		if err := r.Scan(&x.ID, &x.SessionID, &x.Kind, &x.ToolName, &raw, &x.Status, &x.CreatedAt); err != nil {
			return nil, err
		}
		x.Input = json.RawMessage(raw)
		out = append(out, x)
	}
	return out, r.Err()
}

// UsageSample is the subscription usage read at one time, as JSON.
type UsageSample struct {
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

// AddUsageSample records a usage reading and drops those older than keep.
func (s *Store) AddUsageSample(at time.Time, data json.RawMessage, keep time.Duration) error {
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO usage_samples(at,data) VALUES(?,?)`, at.UTC().Format(time.RFC3339Nano), string(data)); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM usage_samples WHERE at<?`, at.Add(-keep).UTC().Format(time.RFC3339Nano))
	return err
}

// UsageSamples lists the readings since a time, oldest first.
func (s *Store) UsageSamples(since time.Time) ([]UsageSample, error) {
	rows, err := s.db.Query(`SELECT at,data FROM usage_samples WHERE at>=? ORDER BY at`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageSample
	for rows.Next() {
		var at, data string
		if err := rows.Scan(&at, &data); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			continue
		}
		out = append(out, UsageSample{At: t, Data: json.RawMessage(data)})
	}
	return out, rows.Err()
}

func (s *Store) SetProfile(id, profileID, snapshot string) error {
	result, err := s.db.Exec("UPDATE sessions SET profile_id=?,profile_json=? WHERE id=?", profileID, snapshot, id)
	if err == nil {
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return sql.ErrNoRows
		}
	}
	return err
}

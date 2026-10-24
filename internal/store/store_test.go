package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeSessionIdentityIsProviderScoped(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	nativeID := ID()
	claude, err := s.Create("Claude", "/tmp", nativeID, true)
	if err != nil {
		t.Fatal(err)
	}
	codex, err := s.CreateProvider("Codex", "/tmp", nativeID, true, "codex")
	if err != nil {
		t.Fatal(err)
	}
	for provider, want := range map[string]Session{"claude": claude, "codex": codex} {
		got, err := s.FindNative(provider, nativeID)
		if err != nil || got.ID != want.ID || got.Provider != provider {
			t.Fatalf("FindNative(%q): %+v %v", provider, got, err)
		}
		if _, err := s.CreateProvider("Duplicate", "/tmp", nativeID, false, provider); err == nil {
			t.Fatalf("duplicate %s native ID accepted", provider)
		}
	}
	if got, err := s.FindClaude(nativeID); err != nil || got.ID != claude.ID {
		t.Fatalf("FindClaude: %+v %v", got, err)
	}
	if _, err := s.FindNative("unknown", nativeID); err != sql.ErrNoRows {
		t.Fatalf("unknown provider: %v", err)
	}
}

func TestMigratesNativeIdentityPreservingChildren(t *testing.T) {
	for _, withProvider := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "provider_column"}[withProvider], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "old.sqlite")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(`PRAGMA foreign_keys=ON;
CREATE TABLE sessions(id TEXT PRIMARY KEY,title TEXT NOT NULL,cwd TEXT NOT NULL,claude_id TEXT NOT NULL UNIQUE,status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,resume INTEGER NOT NULL DEFAULT 0);
INSERT INTO sessions VALUES('old','Old','/tmp','native-id','idle','created','updated',1);
CREATE TABLE events(seq INTEGER PRIMARY KEY AUTOINCREMENT,session_id TEXT NOT NULL REFERENCES sessions(id),kind TEXT NOT NULL,text TEXT NOT NULL,payload TEXT NOT NULL,created_at TEXT NOT NULL);
INSERT INTO events VALUES(41,'old','assistant','saved message','{"saved":true}','event-time');
CREATE TABLE interactions(id TEXT PRIMARY KEY,session_id TEXT NOT NULL REFERENCES sessions(id),kind TEXT NOT NULL,tool_name TEXT NOT NULL,input TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL);
INSERT INTO interactions VALUES('approval','old','approval','Bash','{"command":"pwd"}','resolved','interaction-time');
CREATE TABLE channel_transcript(session_id TEXT NOT NULL REFERENCES sessions(id),event_key TEXT NOT NULL,PRIMARY KEY(session_id,event_key));
INSERT INTO channel_transcript VALUES('old','saved-key');`)
			if err == nil && withProvider {
				_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude';
ALTER TABLE sessions ADD COLUMN mode TEXT NOT NULL DEFAULT 'managed';
ALTER TABLE sessions ADD COLUMN permission_mode TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN claude_args TEXT NOT NULL DEFAULT '[]';
UPDATE sessions SET mode='terminal',permission_mode='acceptEdits',model='opus',effort='high',claude_args='["--verbose"]';
INSERT INTO sessions(id,title,cwd,claude_id,status,created_at,updated_at,resume,provider) VALUES('codex','Codex','/tmp','codex-native','idle','c','u',1,'codex');`)
			}
			closeErr := db.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("fixture: %v %v", err, closeErr)
			}
			var first Session
			for attempt := range 2 {
				s, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				func() {
					defer s.Close()
					x, err := s.FindClaude("native-id")
					if err != nil || x.ID != "old" || x.Provider != "claude" || x.Title != "Old" || x.Cwd != "/tmp" || x.Status != "idle" || x.CreatedAt != "created" || x.UpdatedAt != "updated" || !x.Resume {
						t.Fatalf("session: %+v %v", x, err)
					}
					if withProvider && (x.Mode != "terminal" || x.PermissionMode != "acceptEdits" || x.Model != "opus" || x.Effort != "high" || !reflect.DeepEqual(x.ClaudeArgs, []string{"--verbose"})) {
						t.Fatalf("overrides lost: %+v", x)
					}
					if attempt == 0 {
						first = x
						if _, err := s.CreateProvider("Same ID", "/tmp", "native-id", true, "codex"); err != nil {
							t.Fatal(err)
						}
					} else if !reflect.DeepEqual(first, x) {
						t.Fatalf("reopen changed session: %+v -> %+v", first, x)
					}
					if withProvider {
						if got, err := s.FindNative("codex", "codex-native"); err != nil || got.ID != "codex" {
							t.Fatalf("existing Codex row: %+v %v", got, err)
						}
					}
					events, err := s.Events("old", 0)
					if err != nil || len(events) != 1 || events[0].Seq != 41 || events[0].Kind != "assistant" || events[0].Text != "saved message" || string(events[0].Payload) != `{"saved":true}` || events[0].CreatedAt != "event-time" {
						t.Fatalf("events: %+v %v", events, err)
					}
					var kind, tool, input, status, created string
					if err := s.db.QueryRow(`SELECT kind,tool_name,input,status,created_at FROM interactions WHERE id='approval' AND session_id='old'`).Scan(&kind, &tool, &input, &status, &created); err != nil || kind != "approval" || tool != "Bash" || input != `{"command":"pwd"}` || status != "resolved" || created != "interaction-time" {
						t.Fatalf("interaction: %s %s %s %s %s %v", kind, tool, input, status, created, err)
					}
					if !s.TranscriptFlag("old", "saved-key") {
						t.Fatal("transcript key lost")
					}
					var enabled int
					if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
						t.Fatalf("foreign_keys: %d %v", enabled, err)
					}
					rows, err := s.db.Query(`PRAGMA foreign_key_check`)
					if err != nil {
						t.Fatal(err)
					}
					invalid := rows.Next()
					rowsErr := rows.Err()
					rows.Close()
					if invalid || rowsErr != nil {
						t.Fatalf("foreign key violation: %v %v", invalid, rowsErr)
					}
					for _, table := range []string{"events", "interactions", "channel_transcript"} {
						rows, err := s.db.Query(`PRAGMA foreign_key_list(` + table + `)`)
						if err != nil {
							t.Fatal(err)
						}
						var id, seq int
						var parent, from, to, update, delete, match string
						if !rows.Next() {
							rows.Close()
							t.Fatalf("%s lost foreign key", table)
						}
						err = rows.Scan(&id, &seq, &parent, &from, &to, &update, &delete, &match)
						rows.Close()
						if err != nil || parent != "sessions" || from != "session_id" || to != "id" {
							t.Fatalf("%s foreign key: %s %s %s %v", table, parent, from, to, err)
						}
					}
					if _, err := s.Append("missing", "user", "orphan", nil); err == nil {
						t.Fatal("foreign key enforcement lost")
					}
				}()
			}
		})
	}
}

func TestNativeIdentityMigrationFailureRollsBack(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "invalid.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	legacySchema := strings.Replace(sessionSchema, "UNIQUE(provider,claude_id)", "UNIQUE(claude_id)", 1)
	if _, err := db.Exec(`CREATE TABLE sessions(` + legacySchema + `);
INSERT INTO sessions(id,title,cwd,claude_id,status,created_at,updated_at) VALUES('old','Old','/tmp','native','idle','c','u');
CREATE TABLE child(session_id TEXT NOT NULL REFERENCES sessions(id));
INSERT INTO child VALUES('missing');
PRAGMA foreign_keys=ON;`); err != nil {
		t.Fatal(err)
	}
	if err := migrateNativeIdentity(db); err == nil {
		t.Fatal("migration accepted broken foreign key")
	}
	var enabled, sessions, children, temporary int
	for query, target := range map[string]*int{
		`PRAGMA foreign_keys`: &enabled,
		`SELECT count(*) FROM sessions WHERE id='old' AND claude_id='native'`:      &sessions,
		`SELECT count(*) FROM child WHERE session_id='missing'`:                    &children,
		`SELECT count(*) FROM sqlite_schema WHERE name='sessions_native_identity'`: &temporary,
	} {
		if err := db.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if enabled != 1 || sessions != 1 || children != 1 || temporary != 0 {
		t.Fatalf("rollback: foreign_keys=%d sessions=%d children=%d temporary=%d", enabled, sessions, children, temporary)
	}
	if _, err := db.Exec(`INSERT INTO sessions(id,title,cwd,claude_id,status,created_at,updated_at,provider) VALUES('new','New','/tmp','native','idle','c','u','codex')`); err == nil {
		t.Fatal("rollback did not restore original uniqueness constraint")
	}
}

func TestDurableEventsAndInterruptedRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	x, err := s.Create("Session", t.TempDir(), ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.Append(x.ID, "assistant_delta", "你好", json.RawMessage(`{"type":"stream_event"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetResume(x.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.SetStatus(x.ID, "waiting"); err != nil {
		t.Fatal(err)
	}
	req := Interaction{ID: ID(), SessionID: x.ID, Kind: "approval", ToolName: "Bash", Input: json.RawMessage(`{"command":"git status"}`)}
	if err = s.AddInteraction(req); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err = s.Get(x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if x.Status != "interrupted" || !x.Resume {
		t.Fatalf("invalid recovered session: %+v", x)
	}
	events, err := s.Events(x.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Text != "你好" || events[1].Kind != "error" {
		t.Fatalf("events: %+v", events)
	}
	after, err := s.Events(x.ID, e.Seq)
	if err != nil || len(after) != 1 {
		t.Fatalf("cursor: %+v %v", after, err)
	}
	pending, err := s.Pending(x.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending survived restart: %+v %v", pending, err)
	}
}

func TestApprovalExactlyOnce(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err := s.Create("One", t.TempDir(), ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	r := Interaction{ID: ID(), SessionID: x.ID, Kind: "question", ToolName: "AskUserQuestion", Input: json.RawMessage(`{}`)}
	if err = s.AddInteraction(r); err != nil {
		t.Fatal(err)
	}
	if err = s.Resolve(r.ID, "resolved"); err != nil {
		t.Fatal(err)
	}
	if err = s.Resolve(r.ID, "resolved"); err == nil {
		t.Fatal("second resolution accepted")
	}
}

func TestDeleteRemovesOnlyThatSession(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var ids []string
	for _, title := range []string{"gone", "kept"} {
		x, err := s.Create(title, t.TempDir(), ID(), false)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, x.ID)
		if _, err = s.Append(x.ID, "user", title, nil); err != nil {
			t.Fatal(err)
		}
		if err = s.AddInteraction(Interaction{ID: ID(), SessionID: x.ID, Kind: "approval", ToolName: "Bash", Input: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.RecordTranscript(x.ID, "k:1", "assistant", "hi", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Delete(ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ids[0]); err != sql.ErrNoRows {
		t.Fatalf("session survived: %v", err)
	}
	for table, want := range map[string]int{"events": 2, "interactions": 1, "channel_transcript": 1} {
		var gone, kept int
		_ = s.db.QueryRow(`SELECT count(*) FROM `+table+` WHERE session_id=?`, ids[0]).Scan(&gone)
		_ = s.db.QueryRow(`SELECT count(*) FROM `+table+` WHERE session_id=?`, ids[1]).Scan(&kept)
		if gone != 0 || kept != want {
			t.Fatalf("%s: deleted session rows %d, other session rows %d", table, gone, kept)
		}
	}
	if err = s.Delete(ids[0]); err != sql.ErrNoRows {
		t.Fatalf("second delete: %v", err)
	}
}

func TestMigratesOldSessionSchemaWithOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE sessions(id TEXT PRIMARY KEY,title TEXT NOT NULL,cwd TEXT NOT NULL,claude_id TEXT NOT NULL UNIQUE,status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,resume INTEGER NOT NULL DEFAULT 0);
INSERT INTO sessions VALUES('old','Old','/tmp','abcd1234-1234-1234-1234-123456789abc','idle','t','t',1);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err := s.Get("old")
	if err != nil || x.Provider != "claude" || x.Mode != "managed" || x.PermissionMode != "" || x.Model != "" || x.Effort != "" || !x.Resume {
		t.Fatalf("migrated %+v %v", x, err)
	}
	if err = s.SetOverrides("old", "bypassPermissions", "opus", "high"); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].PermissionMode != "bypassPermissions" || list[0].Model != "opus" || list[0].Effort != "high" {
		t.Fatalf("overrides %+v %v", list, err)
	}
}

// Sessions of the retired shared mode reopen as stopped terminal sessions whose
// recorded transcript is not recorded again; the migration is idempotent.
func TestMigratesSharedSessionsToTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := s.Create("Shared", "/tmp", "ABCD1234-1234-1234-1234-123456789ABC", true)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := s.Create("Managed", "/tmp", ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetMode(shared.ID, "channel"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetStatus(shared.ID, "offline"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordTranscript(shared.ID, "hash:1", "assistant", "hello", nil); err != nil {
		t.Fatal(err)
	}
	s.Close()
	for range 2 {
		if s, err = Open(path); err != nil {
			t.Fatal(err)
		}
		x, err := s.Get(shared.ID)
		if err != nil || x.Mode != "terminal" || x.Status != "idle" || x.ClaudeSessionID != shared.ClaudeSessionID || !x.Resume {
			t.Fatalf("migrated %+v %v", x, err)
		}
		if added, err := s.RecordTranscript(shared.ID, "abcd1234-1234-1234-1234-123456789abc:hash:1", "assistant", "hello", nil); err != nil || added {
			t.Fatalf("transcript recorded again: %v %v", added, err)
		}
		if y, err := s.Get(managed.ID); err != nil || y.Mode != "managed" {
			t.Fatalf("managed session changed %+v %v", y, err)
		}
		s.Close()
	}
}

// Task events come from the partial index, not a scan of the session's events.
func TestTaskEventsUsePartialIndex(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	x, _ := s.Create("Session", t.TempDir(), ID(), false)
	_, _ = s.Append(x.ID, "status", "task_started", json.RawMessage(`{"task_id":"a"}`))
	_, _ = s.Append(x.ID, "status", "requesting", json.RawMessage(`{}`))
	_, _ = s.Append(x.ID, "assistant", "task_ looks alike", nil)
	events, err := s.TaskEvents(x.ID)
	if err != nil || len(events) != 1 || events[0].Text != "task_started" {
		t.Fatal(events, err)
	}
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT seq,session_id,kind,text,payload,created_at FROM events WHERE session_id=? AND kind='status' AND text LIKE 'task_%' ORDER BY seq`, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var id, parent, unused int
		var detail string
		_ = rows.Scan(&id, &parent, &unused, &detail)
		plan += detail + "; "
	}
	if !strings.Contains(plan, "events_tasks") {
		t.Fatalf("plan does not use the partial index: %s", plan)
	}
}

// A session synced by a host without task events is flagged once; one synced
// by this host (TranscriptV2Key) never is.
func TestLegacyTranscriptFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := s.Create("Old", "/tmp", ID(), true)
	current, _ := s.Create("Current", "/tmp", ID(), true)
	if _, err = s.RecordTranscript(old.ID, "abc:hash:1", "assistant", "hello", nil); err != nil {
		t.Fatal(err)
	}
	_ = s.MarkTranscript(current.ID, TranscriptV2Key)
	_, _ = s.RecordTranscript(current.ID, "abc:hash:2", "assistant", "hi", nil)
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	if !s.TranscriptFlag(old.ID, LegacyTasksKey) || !s.TranscriptFlag(old.ID, TranscriptV2Key) || s.TranscriptFlag(current.ID, LegacyTasksKey) {
		t.Fatal("flags after the upgrade")
	}
	if err = s.ClearTranscriptFlag(old.ID, LegacyTasksKey); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.TranscriptFlag(old.ID, LegacyTasksKey) {
		t.Fatal("flagged again on the next start")
	}
}

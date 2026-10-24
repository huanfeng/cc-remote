package store

import (
	"database/sql"
	"time"
)

// A separate keyed table keeps the legacy sessions migration and notification
// timestamp contract intact. Event inserts update activity atomically and
// never counts connection, settings or restart administration as conversation.
func migrateConversationActivity(db *sql.DB) error {
	var exists int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='session_activity'`).Scan(&exists); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS session_activity(session_id TEXT PRIMARY KEY REFERENCES sessions(id),last_at TEXT NOT NULL,estimated INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return err
	}
	if exists == 0 {
		// Old events may have been ingested later than their original activity.
		// Preserve that uncertainty until a source transcript provides its clock.
		_, err = tx.Exec(`INSERT INTO session_activity
SELECT session_id,strftime('%Y-%m-%dT%H:%M:%f000000Z',MAX(julianday(created_at))),1 FROM events
WHERE (kind IN ('user','assistant','tool','tool_result','result') OR (kind='status' AND text IN ('task_started','task_progress','task_updated','task_notification'))) AND julianday(created_at) IS NOT NULL GROUP BY session_id`)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func recordConversation(tx *sql.Tx, id, kind, text, at string) error {
	if at == "" {
		return nil
	}
	switch kind {
	case "user", "assistant", "tool", "tool_result", "result":
	case "status":
		switch text {
		case "task_started", "task_progress", "task_updated", "task_notification":
		default:
			return nil
		}
	default:
		return nil
	}
	_, err := tx.Exec(`INSERT INTO session_activity VALUES(?,?,0)
 ON CONFLICT(session_id) DO UPDATE SET last_at=excluded.last_at,estimated=0
 WHERE excluded.last_at>=session_activity.last_at`, id, at)
	return err
}

func conversationTimestamp(at string) string {
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

// SetConversationTime accepts authoritative source time even when it corrects
// an older estimate, but never replaces newer known activity with an old scan.
func (s *Store) SetConversationTime(id, at string, estimated bool) error {
	at = conversationTimestamp(at)
	if at == "" {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO session_activity VALUES(?,?,?)
ON CONFLICT(session_id) DO UPDATE SET last_at=excluded.last_at,estimated=excluded.estimated
WHERE (session_activity.estimated=1 AND excluded.estimated=0)
 OR (session_activity.estimated=excluded.estimated AND excluded.last_at>session_activity.last_at)`, id, at, estimated)
	return err
}

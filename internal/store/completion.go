package store

import "database/sql"

func migrateCompletionRead(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS session_completion(
session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
completed_at TEXT NOT NULL,event_seq INTEGER NOT NULL,read_completed_at TEXT NOT NULL DEFAULT '')`); err != nil {
		return err
	}
	var existing int
	if err := tx.QueryRow(`SELECT count(*) FROM pragma_table_info('session_completion') WHERE name='read_completed_at'`).Scan(&existing); err != nil {
		return err
	}
	// Older hosts have no shared read receipts. Establish a baseline once so
	// upgrading does not repopulate Done with the host's entire history.
	if existing == 0 {
		if _, err := tx.Exec(`ALTER TABLE session_completion ADD COLUMN read_completed_at TEXT NOT NULL DEFAULT '';
UPDATE session_completion SET read_completed_at=completed_at`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MarkCompletionRead acknowledges the observed completion only. A delayed or
// duplicate receipt is a no-op once another completion has replaced it.
func (s *Store) MarkCompletionRead(sessionID, completedAt string) error {
	_, err := s.db.Exec(`UPDATE session_completion SET read_completed_at=completed_at
WHERE session_id=? AND completed_at=? AND read_completed_at<>completed_at`, sessionID, completedAt)
	return err
}

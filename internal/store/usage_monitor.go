package store

import "time"

// PruneMonitorUsage also expires archived accounts which receive no new samples.
func (s *Store) PruneMonitorUsage(since time.Time) error {
	_, err := s.db.Exec(`DELETE FROM account_usage_samples WHERE (source LIKE 'monitor:%' OR source LIKE 'monitor-cache:%') AND at<?`, since.UTC().Format(time.RFC3339Nano))
	return err
}

// MoveAccountUsage adopts only a credential's own isolated samples after the
// server has confirmed its identity. Unknown historical profile data is not guessed.
func (s *Store) MoveAccountUsage(from, to string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT OR IGNORE INTO account_usage_samples(source,at,data) SELECT ?,at,data FROM account_usage_samples WHERE source=?`, "monitor:"+to, "monitor:"+from); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM account_usage_samples WHERE source=?`, "monitor:"+from); err != nil {
		return err
	}
	return tx.Commit()
}

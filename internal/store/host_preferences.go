package store

const supplierSwitchWarning = "supplier_switch_warning"

// SupplierSwitchWarning defaults to enabled on hosts without a saved choice.
func (s *Store) SupplierSwitchWarning() (bool, error) {
	var enabled bool
	err := s.db.QueryRow("SELECT COALESCE((SELECT value FROM host_preferences WHERE key=?),1)", supplierSwitchWarning).Scan(&enabled)
	return enabled, err
}

func (s *Store) SetSupplierSwitchWarning(enabled bool) error {
	_, err := s.db.Exec(`INSERT INTO host_preferences(key,value) VALUES(?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, supplierSwitchWarning, enabled)
	return err
}

const uploadLimitMiB = "upload_limit_mib"

func (s *Store) UploadLimitMiB(defaultMiB int) (int, error) {
	var limit int
	err := s.db.QueryRow("SELECT COALESCE((SELECT value FROM host_preferences WHERE key=?),?)", uploadLimitMiB, defaultMiB).Scan(&limit)
	return limit, err
}

func (s *Store) SetUploadLimitMiB(limit int) error {
	_, err := s.db.Exec(`INSERT INTO host_preferences(key,value) VALUES(?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, uploadLimitMiB, limit)
	return err
}

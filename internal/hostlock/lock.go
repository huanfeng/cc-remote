package hostlock

import (
	"os"
	"path/filepath"

	"cc-remote/internal/i18n"
)

// Acquire holds an OS-owned lock for the lifetime of a host process. A stale
// lock file is harmless: the operating system releases locks after a crash.
func Acquire(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lock(f); err != nil {
		f.Close()
		return nil, i18n.Errorf("this data directory is in use by another host: %w", "此数据目录已被另一个宿主使用：%w", err)
	}
	return func() { unlock(f); f.Close() }, nil
}

// Held reports whether a host holds dir's lock now. It never creates the
// lock file, and only takes the lock (for a moment) when no host holds it.
func Held(dir string) bool {
	f, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	if lock(f) != nil {
		return true
	}
	unlock(f)
	return false
}

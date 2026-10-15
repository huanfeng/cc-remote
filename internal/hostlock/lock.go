package hostlock

import (
	"fmt"
	"os"
	"path/filepath"
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
		return nil, fmt.Errorf("此数据目录已被另一个宿主使用：%w", err)
	}
	return func() { unlock(f); f.Close() }, nil
}

// Package logfile provides an append-only log file with single-backup size rotation.
package logfile

import (
	"os"
	"runtime/debug"
	"sync"
)

// DefaultMaxSize is the size at which host.log is rotated to host.log.1.
const DefaultMaxSize = 10 << 20

// Writer is safe for concurrent use.
type Writer struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	file    *os.File
	size    int64
	crash   bool
}

// Open appends to path, rotating to path+".1" once maxSize would be exceeded.
func Open(path string, maxSize int64) (*Writer, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	w := &Writer{path: path, maxSize: maxSize}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxSize {
		w.rotate()
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// SetCrashOutput makes the Go runtime write fatal errors and unrecovered
// panics to the log file as well (debug.SetCrashOutput). The duplicated
// handle is released before each rotation so renaming works on Windows, and
// re-attached to the new file afterwards.
func (w *Writer) SetCrashOutput() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.crash = true
	return w.attachCrash()
}

func (w *Writer) attachCrash() error {
	if !w.crash || w.file == nil || w.file == os.Stderr {
		return nil
	}
	return debug.SetCrashOutput(w.file, debug.CrashOptions{})
}

// rotate keeps one backup. If another process holds the file open and the
// rename fails (Windows), the current file is truncated instead.
func (w *Writer) rotate() {
	if w.crash {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	}
	w.file.Close()
	w.file = nil
	backup := w.path + ".1"
	_ = os.Remove(backup)
	if err := os.Rename(w.path, backup); err != nil {
		_ = os.Truncate(w.path, 0)
	}
	if err := w.open(); err != nil {
		// Keep writing somewhere rather than losing the host's output.
		w.file, w.size = os.Stderr, 0
	}
	_ = w.attachCrash()
}

// Close closes the underlying file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil || w.file == os.Stderr {
		return nil
	}
	if w.crash {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	}
	err := w.file.Close()
	w.file = nil
	return err
}

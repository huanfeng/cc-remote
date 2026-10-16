package terminal

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on escape-sequence processing for a Windows console so the
// session's screen renders; it returns a function restoring the old mode.
func enableVT(f *os.File) func() {
	h := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return func() {}
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	return func() { _ = windows.SetConsoleMode(h, mode) }
}

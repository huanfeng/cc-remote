package terminal

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on escape-sequence processing for a Windows console so the
// session's screen renders; it returns a function restoring the old mode.
// A bare LF must only move down, as in any terminal: the host's ConPTY sends
// one to start the next row at the same column, and a console that also
// returns the carriage puts that row's text at the left edge, where later
// updates (which skip cells they think blank) leave it behind.
func enableVT(f *os.File) func() {
	h := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return func() {}
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN)
	return func() { _ = windows.SetConsoleMode(h, mode) }
}

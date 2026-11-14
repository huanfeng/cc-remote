package terminal

import (
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var readPickerConsoleInput = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReadConsoleInputW")

// Reading console records avoids a background stdin reader stealing later PTY
// input, and distinguishes a lone Escape from arrow keys without a timeout.
func readPickerKey(timeout time.Duration) (string, error) {
	h := windows.Handle(os.Stdin.Fd())
	until := time.Now().Add(timeout)
	for time.Now().Before(until) {
		state, err := windows.WaitForSingleObject(h, uint32(max(1, time.Until(until).Milliseconds())))
		if err != nil {
			return "", err
		}
		if state == uint32(windows.WAIT_TIMEOUT) {
			return "", nil
		}
		var record struct {
			Kind                    uint16
			Pad                     uint16
			Down                    uint32
			Repeat, Key, Scan, Char uint16
			Control                 uint32
		}
		var n uint32
		ok, _, e := readPickerConsoleInput.Call(uintptr(h), uintptr(unsafe.Pointer(&record)), 1, uintptr(unsafe.Pointer(&n)))
		if ok == 0 {
			return "", e
		}
		if n == 0 || record.Kind != 1 || record.Down == 0 {
			continue
		}
		switch record.Key {
		case 0x26:
			return "up", nil
		case 0x28:
			return "down", nil
		case 0x1b:
			return "escape", nil
		case 0x0d:
			return "enter", nil
		case 0x08:
			return "backspace", nil
		}
		if record.Char == 3 {
			return "ctrl-c", nil
		}
		if record.Char >= 32 {
			return string(rune(record.Char)), nil
		}
	}
	return "", nil
}

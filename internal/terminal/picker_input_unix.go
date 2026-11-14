//go:build !windows

package terminal

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func makePickerRaw(fd int) (*term.State, error) { return term.MakeRaw(fd) }

func pickerInputReady(timeout time.Duration) (bool, error) {
	if stdin.Buffered() > 0 {
		return true, nil
	}
	fds := []unix.PollFd{{Fd: int32(os.Stdin.Fd()), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, int(timeout.Milliseconds()))
	return n > 0, err
}
func readPickerKey(timeout time.Duration) (string, error) {
	ready, err := pickerInputReady(timeout)
	if err != nil || !ready {
		return "", err
	}
	r, _, err := stdin.ReadRune()
	if err != nil {
		return "", err
	}
	switch r {
	case 3:
		return "ctrl-c", nil
	case '\r', '\n':
		return "enter", nil
	case 8, 127:
		return "backspace", nil
	case 27:
		ready, err = pickerInputReady(35 * time.Millisecond)
		if err != nil || !ready {
			return "escape", err
		}
		b, err := stdin.ReadByte()
		if err != nil {
			return "", err
		}
		if b != '[' && b != 'O' {
			_ = stdin.UnreadByte()
			return "escape", nil
		}
		ready, err = pickerInputReady(35 * time.Millisecond)
		if err != nil || !ready {
			return "escape", err
		}
		b, err = stdin.ReadByte()
		if err != nil {
			return "", err
		}
		if b == 'A' {
			return "up", nil
		}
		if b == 'B' {
			return "down", nil
		}
		return "", nil
	}
	if r >= 32 {
		return string(r), nil
	}
	return "", nil
}

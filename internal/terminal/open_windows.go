package terminal

import "golang.org/x/sys/windows"

// openFile opens a file with its default program, as a double-click in Explorer does.
func openFile(path string) {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	_ = windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), file, nil, nil, windows.SW_SHOWNORMAL)
}

//go:build windows

package service

import (
	"fmt"
	"io"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func defaultAliasDir() (string, error) { return BinDir() }

func readUserPath() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if err == registry.ErrNotExist {
		return "", nil
	}
	return v, err
}

func writeUserPath(v string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err = k.SetExpandStringValue("Path", v); err != nil {
		return err
	}
	broadcastEnvironmentChange()
	return nil
}

// broadcastEnvironmentChange tells Explorer to reload the user environment so
// newly opened terminals see the PATH change.
func broadcastEnvironmentChange() {
	env, _ := windows.UTF16PtrFromString("Environment")
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xFFFF, 0x001A, 0x0002
	var result uintptr
	proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}

func ensureOnPath(dir string, dryRun bool, out io.Writer) error {
	current, err := readUserPath()
	if err != nil {
		return fmt.Errorf("read HKCU\\Environment Path: %w", err)
	}
	merged, changed := MergePath(current, dir, ";")
	if !changed {
		fmt.Fprintf(out, "%s is already on the user PATH\n", dir)
		return nil
	}
	if dryRun {
		fmt.Fprintf(out, "[dry-run] set HKCU\\Environment Path (REG_EXPAND_SZ):\n  before: %s\n  after:  %s\n[dry-run] broadcast WM_SETTINGCHANGE\n", current, merged)
		return nil
	}
	if err = writeUserPath(merged); err != nil {
		return fmt.Errorf("update user PATH: %w", err)
	}
	fmt.Fprintf(out, "Added %s to the user PATH; open a new terminal (cmd or PowerShell) to use ccr\n", dir)
	return nil
}

func removeFromPath(dir string, dryRun bool, out io.Writer) error {
	current, err := readUserPath()
	if err != nil {
		return err
	}
	updated, changed := RemovePath(current, dir, ";")
	if !changed {
		return nil
	}
	if dryRun {
		fmt.Fprintf(out, "[dry-run] set HKCU\\Environment Path:\n  before: %s\n  after:  %s\n", current, updated)
		return nil
	}
	if err = writeUserPath(updated); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed %s from the user PATH\n", dir)
	return nil
}

func pathStatus(dir string, out io.Writer) {
	current, err := readUserPath()
	if err != nil {
		fmt.Fprintf(out, "User PATH: %v\n", err)
		return
	}
	fmt.Fprintf(out, "Directory on user PATH (registry): %s\n", yesNo(PathContains(current, dir, ";")))
}

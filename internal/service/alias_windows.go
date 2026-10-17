//go:build windows

package service

import (
	"fmt"
	"io"
	"unsafe"

	"cc-remote/internal/i18n"

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
		return i18n.Errorf("read HKCU\\Environment Path: %w", "读取 HKCU\\Environment Path 失败：%w", err)
	}
	merged, changed := MergePath(current, dir, ";")
	if !changed {
		fmt.Fprint(out, i18n.F("%s is already on the user PATH\n", "%s 已在用户 PATH 中\n", dir))
		return nil
	}
	if dryRun {
		fmt.Fprint(out, i18n.F("[dry-run] set HKCU\\Environment Path (REG_EXPAND_SZ):\n  before: %s\n  after:  %s\n[dry-run] broadcast WM_SETTINGCHANGE\n", "[dry-run] 设置 HKCU\\Environment Path（REG_EXPAND_SZ）：\n  修改前：%s\n  修改后：%s\n[dry-run] 广播 WM_SETTINGCHANGE\n", current, merged))
		return nil
	}
	if err = writeUserPath(merged); err != nil {
		return i18n.Errorf("update user PATH: %w", "更新用户 PATH 失败：%w", err)
	}
	fmt.Fprint(out, i18n.F("Added %s to the user PATH; open a new terminal (cmd or PowerShell) to use ccr\n", "已把 %s 加入用户 PATH；打开新的终端（cmd 或 PowerShell）即可使用 ccr\n", dir))
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
		fmt.Fprint(out, i18n.F("[dry-run] set HKCU\\Environment Path:\n  before: %s\n  after:  %s\n", "[dry-run] 设置 HKCU\\Environment Path：\n  修改前：%s\n  修改后：%s\n", current, updated))
		return nil
	}
	if err = writeUserPath(updated); err != nil {
		return err
	}
	fmt.Fprint(out, i18n.F("Removed %s from the user PATH\n", "已从用户 PATH 中移除 %s\n", dir))
	return nil
}

func pathStatus(dir string, out io.Writer) {
	current, err := readUserPath()
	if err != nil {
		fmt.Fprint(out, i18n.F("User PATH: %v\n", "用户 PATH：%v\n", err))
		return
	}
	fmt.Fprint(out, i18n.F("Directory on user PATH (registry): %s\n", "目录在用户 PATH 中（注册表）：%s\n", yesNo(PathContains(current, dir, ";"))))
}

package i18n

import "golang.org/x/sys/windows"

// systemLocales is the Windows display language. Locale variables (as Git Bash
// sets LANG) come only after it: they rarely reflect the user's choice there.
func systemLocales() []string {
	if names, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil && len(names) > 0 {
		return names[:1]
	}
	return unixLocales()
}

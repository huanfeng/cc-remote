//go:build !windows

package i18n

func systemLocales() []string { return unixLocales() }

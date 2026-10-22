package service

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

// TestMain points InstallRoot at a temp dir for the whole package. SaveConfig
// mirrors service.json there, and a reinstall or update prefers that mirror:
// a test that forgot isolateInstallRoot once rewrote the real service to run
// from its temp data dir and port.
func TestMain(m *testing.M) {
	base, err := os.MkdirTemp("", "cc-remote-service-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("LOCALAPPDATA", base)  // windows
	os.Setenv("XDG_DATA_HOME", base) // linux
	os.Setenv("HOME", base)          // darwin
	managerDisabled = errors.New("tests do not run the service manager")
	code := m.Run()
	os.RemoveAll(base)
	os.Exit(code)
}

//go:build !windows && !linux && !darwin

package service

import (
	"io"

	"cc-remote/internal/i18n"
)

const managerName = "unsupported"

type unsupportedError struct{}

func (unsupportedError) Error() string {
	return i18n.T("service management is not supported on this platform", "此平台不支持服务管理")
}

var errUnsupported error = unsupportedError{}

func installBinaries(string, bool, io.Writer) error { return errUnsupported }
func register(*Config, bool, io.Writer) error       { return errUnsupported }
func unregister(bool, io.Writer) error              { return errUnsupported }
func updateDefinition(*Config) error                { return errUnsupported }
func pathHint(*Config, io.Writer)                   {}
func schedulerStart() error                         { return errUnsupported }
func schedulerStop() error                          { return errUnsupported }
func schedulerState() schedState                    { return schedState{} }
func firewallHint([]string, io.Writer)              {}

// Supervise is only used by the Windows scheduled task.
func Supervise([]string) error { return errUnsupported }

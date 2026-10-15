//go:build !windows && !linux && !darwin

package service

import (
	"errors"
	"io"
)

const managerName = "unsupported"

var errUnsupported = errors.New("service management is not supported on this platform")

func installBinaries(string, bool, io.Writer) error { return errUnsupported }
func register(*Config, bool, io.Writer) error       { return errUnsupported }
func unregister(bool, io.Writer) error              { return errUnsupported }
func schedulerStart() error                         { return errUnsupported }
func schedulerStop() error                          { return errUnsupported }
func schedulerState() schedState                    { return schedState{} }
func firewallHint([]string, io.Writer)              {}

// Supervise is only used by the Windows scheduled task.
func Supervise([]string) error { return errUnsupported }

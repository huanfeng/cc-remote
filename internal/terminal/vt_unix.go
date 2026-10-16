//go:build !windows

package terminal

import "os"

// enableVT is a no-op: Unix terminals process escape sequences already.
func enableVT(*os.File) func() { return func() {} }

//go:build unix

package app

import (
	"io/fs"
	"syscall"
)

// currentUmask reads the process umask (setting it back at once: the CLI creates no files
// concurrently).
func currentUmask() fs.FileMode {
	m := syscall.Umask(0)
	syscall.Umask(m)
	return fs.FileMode(m)
}

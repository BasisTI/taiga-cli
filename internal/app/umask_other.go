//go:build !unix

package app

import "io/fs"

// currentUmask has no umask to read outside Unix: the usual 022.
func currentUmask() fs.FileMode { return 0o022 }

//go:build unix

package nodejs

import "io/fs"

// isExecutable reports whether the mode carries any execute bit.
func isExecutable(mode fs.FileMode) bool {
	return mode.Perm()&0o111 != 0
}

// nodeBinaryName is the runtime's executable name on this platform.
const nodeBinaryName = "node"

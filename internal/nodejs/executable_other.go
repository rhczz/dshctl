//go:build !unix

package nodejs

import "io/fs"

// isExecutable reports true: Windows has no execute bit, so a regular file is
// the whole test.
func isExecutable(_ fs.FileMode) bool { return true }

// nodeBinaryName is the runtime's executable name on Windows.
const nodeBinaryName = "node.exe"

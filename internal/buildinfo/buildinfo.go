// Package buildinfo reports the platform the running binary targets.
//
// It exists so that version reporting and the platform-specific host layer agree
// on one answer, and so that tests can substitute a platform without rebuilding.
package buildinfo

import "runtime"

// Platform returns the GOOS/GOARCH pair of the running binary, for example
// "darwin/arm64".
func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// GOOS reports the operating system the binary targets.
func GOOS() string { return runtime.GOOS }

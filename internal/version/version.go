// Package version carries the build metadata injected with -ldflags.
package version

import (
	"fmt"

	"github.com/rhczz/dshctl/internal/buildinfo"
)

// Build metadata. Release builds replace these with -ldflags -X.
var (
	// Version is the release version or "dev".
	Version = "dev"
	// Commit is the source revision the binary was built from.
	Commit = "none"
	// BuildDate is the UTC build timestamp.
	BuildDate = "unknown"
)

// Info describes the running build.
type Info struct {
	// Version is the release version or "dev".
	Version string `json:"version"`
	// Commit is the source revision the binary was built from.
	Commit string `json:"commit"`
	// BuildDate is the UTC build timestamp.
	BuildDate string `json:"buildDate"`
	// Platform is the GOOS/GOARCH pair the binary targets.
	Platform string `json:"platform"`
}

// Get returns the build metadata compiled into this binary.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		Platform:  buildinfo.Platform(),
	}
}

// String renders the metadata on one line.
func (i Info) String() string {
	return fmt.Sprintf("dshctl %s (%s, commit %s, built %s)",
		i.Version, i.Platform, i.Commit, i.BuildDate)
}

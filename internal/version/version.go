// Package version carries the build metadata injected with -ldflags.
package version

import (
	"fmt"
	"runtime/debug"

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
	// GoVersion is the toolchain that compiled this binary, and Module is the
	// module path it was compiled from. They are not in the one-line rendering:
	// they are what a bug report needs, and a report is read as JSON.
	GoVersion string `json:"goVersion,omitempty"`
	Module    string `json:"module,omitempty"`
}

// Get returns the build metadata compiled into this binary.
func Get() Info {
	goVersion, module := buildFacts()
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		Platform:  buildinfo.Platform(),
		GoVersion: goVersion,
		Module:    module,
	}
}

// buildFacts reads what the toolchain recorded about this build.
//
// A binary built outside module mode reports nothing, which stays visible as
// empty fields rather than as a guess: the point of carrying them is that a bug
// report can name the toolchain, and a wrong name is worse than no name.
func buildFacts() (string, string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	return info.GoVersion, info.Main.Path
}

// String renders the metadata on one line.
func (i Info) String() string {
	return fmt.Sprintf("dshctl %s (%s, commit %s, built %s)",
		i.Version, i.Platform, i.Commit, i.BuildDate)
}

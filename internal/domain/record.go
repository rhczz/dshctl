package domain

import (
	"fmt"
	"strings"
	"time"
)

// Phase names what dshctl was doing when it wrote the record.
type Phase string

const (
	// PhaseRunning means the server answered on its port and is expected there.
	//
	// A record exists only for a server that reached that point: it is written
	// after the port answers, so there is no separate "starting" phase to record.
	// (A start that is interrupted writes a wrapper record first so the process
	// it launched is not left unowned; see the kernel.)
	PhaseRunning Phase = "running"
)

// Record is the runtime state of the server dshctl started.
//
// It is the model's answer to "which process did I start, and is this still it":
// the pid alone cannot say, which is why StartedAt is the fingerprint the
// identity rule compares. The bytes live in internal/state; the meaning lives
// here, so a front-end that never reads the file can still name an instance.
type Record struct {
	// PID is the process holding the port: the server itself, not the wrapper
	// dshctl launched. pnpm runs package scripts in a child process, so the pid
	// dshctl spawns is usually not the one that listens, and the listener is what
	// ownership is about.
	PID int `json:"pid"`
	// StartedAt is that process's start time in Unix seconds. It is the
	// fingerprint that makes PID reuse detectable.
	StartedAt int64 `json:"startedAt"`
	// Port is the loopback port the server was asked to bind.
	Port int `json:"port"`
	// URL is the token-carrying address the server announced, when known.
	URL string `json:"url,omitempty"`
	// SpawnedPID is the process dshctl started, which is the leader of the
	// process group the server lives in. It is kept so the whole group can be
	// ended as a unit; it is not what ownership is decided from.
	SpawnedPID int `json:"spawnedPid,omitempty"`
	// NodeVersion is the Node release the server was started with, when known.
	// It is recorded because --node can differ from the settings document, so
	// the record is the only place that says what this instance runs.
	NodeVersion string `json:"nodeVersion,omitempty"`
	// NodePath is the node binary the server was started with, when known.
	NodePath string `json:"nodePath,omitempty"`
	// RepoDir is the checkout the server was started from, when known. The
	// checkout can differ from the configured one — `--repo` applies to one
	// invocation, and the checkout may move while a server keeps running — so
	// the record is the only place that says which tree this instance serves.
	// It is what lets a reporting command describe the running service instead
	// of the configuration, and what lets a start that finds the service already
	// running close the gap between the two.
	RepoDir string `json:"repoDir,omitempty"`
	// Phase is what dshctl observed the last time it wrote the record.
	Phase Phase `json:"phase"`
	// UpdatedAt is when the record was last written, in Unix seconds.
	UpdatedAt int64 `json:"updatedAt"`
}

// Describe renders the record in one line for diagnostics.
func (r Record) Describe() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "pid=%d, started=%s, port=%d, phase=%s",
		r.PID, time.Unix(r.StartedAt, 0).Format(time.RFC3339), r.Port, r.Phase)
	if r.URL != "" {
		builder.WriteString(", url=")
		builder.WriteString(r.URL)
	}
	if r.NodeVersion != "" {
		builder.WriteString(", node=")
		builder.WriteString(r.NodeVersion)
	}
	if r.RepoDir != "" {
		builder.WriteString(", repo=")
		builder.WriteString(r.RepoDir)
	}
	return builder.String()
}

package domain

// State is what an instance of the managed server looks like from outside.
type State string

const (
	// StateRunning means the server dshctl started owns the port.
	StateRunning State = "running"
	// StateStarting means dshctl's process lives but the port is not answering.
	StateStarting State = "starting"
	// StateStopped means nothing is listening and no record survives.
	StateStopped State = "stopped"
	// StateForeign means the port is owned by a process that is positively not
	// the server this dshctl manages: another program, or a DeepSeek Harness
	// server of a different checkout.
	StateForeign State = "port-foreign"
	// StateOrphan means something owns the port that dshctl cannot classify,
	// because the runtime record is missing or contradicts the live process.
	// An orphan is reported and never signalled directly; a mutating command
	// adopts it first when group evidence proves it is this dshctl's own
	// server left behind by an interrupted start.
	StateOrphan State = "port-unmanaged"
	// StateUnobservable means a discovered instance could not be looked at at
	// all, because the port probe failed. It is deliberately not "stopped": the
	// command does not know, and claiming it does is how a running server gets
	// forgotten. Only instances other than the one a command names can end up
	// here; a failed probe of the configured port is reported as an error.
	StateUnobservable State = "unobservable"
)

// Status is the observed state of the managed service.
type Status struct {
	// State is one of the State* constants.
	State State `json:"state"`
	// URL is the configured loopback address, without a token.
	URL string `json:"url"`
	// Port is the configured loopback port.
	Port int `json:"port"`
	// Ready reports whether the port accepted a connection.
	Ready bool `json:"ready"`
	// ListenerPID is the process holding the port, or 0.
	ListenerPID int `json:"listenerPid,omitempty"`
	// ListenerCommand identifies the listener when the platform can describe it.
	ListenerCommand string `json:"listenerCommand,omitempty"`
	// RecordedPID is the pid in the runtime record, or 0.
	RecordedPID int `json:"recordedPid,omitempty"`
	// RecordedPhase is the phase in the runtime record, or empty.
	RecordedPhase string `json:"recordedPhase,omitempty"`
	// RecordedNodeVersion is the release the recorded server was started with,
	// or empty when the record predates the field.
	RecordedNodeVersion string `json:"recordedNodeVersion,omitempty"`
	// RecordedNodePath is the binary that server was started with.
	RecordedNodePath string `json:"recordedNodePath,omitempty"`
	// RecordedRepoDir is the checkout the recorded server was started from, or
	// empty when the record predates the field. It is a fact about the running
	// instance, which the configured checkout is not: `--repo` applies to one
	// invocation, so the two can differ while the server keeps serving.
	RecordedRepoDir string `json:"recordedRepoDir,omitempty"`
	// RecordLive reports that the record names a process that is alive and
	// whose start time still matches: a server this dshctl started and can
	// still end, whether or not it holds the port.
	RecordLive bool `json:"recordLive"`
	// RecordStale reports that the record exists but names a pid that is gone or
	// has been recycled, so it describes nothing. It is never set for a record
	// whose process is still alive: that record is the only handle on a running
	// server and is kept until the server is ended.
	RecordStale bool `json:"recordStale"`
	// StaleRecord is the record itself when RecordStale is set, so a caller can
	// say which pid and phase the stale record named. It is not part of the wire
	// shape: the facts a front-end needs are the fields above.
	StaleRecord *Record `json:"-"`
	// Survivor reports that a server of ours holds the port although the record
	// does not name it: the shape an interrupted start leaves behind, when
	// dshctl was killed between writing the wrapper record and the port
	// answering. A mutating command adopts it and manages it again.
	Survivor bool `json:"survivorService,omitempty"`
	// URLFromRecord is the token-carrying address the server announced.
	URLFromRecord string `json:"urlWithToken,omitempty"`
	// RepoDir is the managed checkout.
	RepoDir string `json:"repoDir"`
	// RepoReady reports whether the checkout is a DeepSeek Harness checkout.
	RepoReady bool `json:"repoReady"`
	// BuildReady reports whether the checkout is installed and built.
	BuildReady bool `json:"buildReady"`
	// LogPath is where server, build and update output accumulates.
	LogPath string `json:"logPath"`
	// LockHeld reports whether another dshctl operation holds the lock.
	LockHeld bool `json:"lockHeld"`
	// LockHolder is the pid holding the lock, or 0.
	LockHolder int `json:"lockHolder,omitempty"`
	// LockUnreadable reports that the lock exists but could not be inspected.
	LockUnreadable bool `json:"lockUnreadable,omitempty"`
	// ProbeError explains why a discovered instance could not be looked at. It
	// is set only with StateUnobservable, and it is what keeps that state from
	// being read as a claim about the server.
	ProbeError string `json:"probeError,omitempty"`
}

// Owning reports whether the state describes a server this dshctl owns.
func (s Status) Owning() bool {
	return s.State == StateRunning || s.State == StateStarting
}

// Occupant reports whether the port is held by something no operation may act
// on: another program, or a process whose ownership dshctl cannot establish. A
// survivor is not an occupant — it is ours, and the operation adopts it first,
// so by the time this matters the only orphans left are ones nobody can claim.
//
// This is the rule every mutating verb reads, and the reason it lives here is
// that the answer must not depend on which verb asked.
func (s State) Occupant(survivor bool) bool {
	return s == StateForeign || (s == StateOrphan && !survivor)
}

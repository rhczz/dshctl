package service

import (
	"context"

	"github.com/rhczz/dshctl/internal/domain"
)

// This file holds the single answers to the two questions every lifecycle
// command asks: "is a server of ours running?" and "is the process on the port
// one of ours?". They live together because keeping them in one place is what
// stops `stop`, `update`, `build` and the cross-port guard from disagreeing
// about the same state.

// servingPort is one running server of ours.
type servingPort struct {
	port int
	pid  int
}

// servingPorts is a list of running servers.
type servingPorts []servingPort

// ports lists the ports.
func (s servingPorts) ports() []int {
	ports := make([]int, 0, len(s))
	for _, entry := range s {
		ports = append(ports, entry.port)
	}
	return ports
}

// pids lists the processes.
func (s servingPorts) pids() []int {
	pids := make([]int, 0, len(s))
	for _, entry := range s {
		pids = append(pids, entry.pid)
	}
	return pids
}

// recordServes reports whether the server a record describes is running now,
// whatever pid the record happens to name.
//
// It is the one predicate behind every "is a service of ours running here"
// decision, so no two commands can disagree about the same record:
//
//   - the record names a live process whose start time still matches; or
//   - the record names a start whose wrapper is gone, but the port the record
//     carries is served by a process descending from that wrapper — the
//     survivor of a start that was interrupted before it could record the
//     listener.
func (s *Service) recordServes(ctx context.Context, record domain.Record) bool {
	if record.PID > 0 && s.RecordMatches(ctx, record, record.PID) {
		return true
	}
	if record.SpawnedPID > 0 && record.Port > 0 {
		result, err := s.Host.Listening(ctx, record.Port)
		if err == nil && result.Listening && result.PID > 0 {
			return s.descendsFromSpawned(record.SpawnedPID, result.PID)
		}
	}
	return false
}

// runningPID reports the pid of the server this port's observation describes,
// when one is running. It reads the same facts the state machine does, so a
// caller cannot act on a server the reported state does not mention.
//
// "Running" here is the lenient question — it is what the guards need, and a
// record whose process is alive counts even when its identity could not be
// confirmed. Deciding whether that process may be signalled is a stricter
// question; see stopTarget.
func (s *Service) runningPID(ctx context.Context, observed observed) (int, bool) {
	switch {
	case observed.status.Owning():
		return observed.status.ListenerPID, true
	case observed.status.Survivor:
		return observed.status.ListenerPID, true
	case observed.hasRecord && s.recordServes(ctx, observed.record):
		return observed.record.PID, true
	}
	return 0, false
}

// stopTarget reports the process a stop may actually signal.
//
// This is the strict question, and it is deliberately narrower than "is a
// server running": a signal is only delivered to a process whose identity
// dshctl has verified, because a wrong signal cannot be taken back.
//
//   - the recorded process holds the port and is the one this dshctl started
//     (the port is the identity);
//   - the listener descends from the wrapper the record names (an interrupted
//     start's survivor, which the caller adopts before signalling);
//   - the record names a live process whose start time matches a *known*
//     recorded start time (the fingerprint is the identity).
//
// A record that only matches because the platform could not report either
// start time does not authorize a signal: on such a host the port check is the
// only evidence, and a recorded pid that is not the listener is left alone.
func (s *Service) stopTarget(ctx context.Context, observed observed) (pid int, ok bool) {
	switch {
	case observed.status.Owning():
		return observed.status.ListenerPID, true
	case observed.status.Survivor:
		return observed.status.ListenerPID, true
	case observed.hasRecord && s.recordIdentityVerified(ctx, observed.record):
		return observed.record.PID, true
	}
	return 0, false
}

// recordIdentityVerified reports whether a signal decision may rest on the
// record's fingerprint.
//
// Both sides of the comparison must be readable: the record's start time *and*
// the live process's. When the platform cannot report one of them, state.Match
// deliberately treats the pair as matching — the right answer for reporting,
// and the wrong one for signalling, because it would authorize a signal on no
// evidence at all. On such a host only the port can identify the process.
func (s *Service) recordIdentityVerified(ctx context.Context, record domain.Record) bool {
	if record.PID <= 0 || record.StartedAt <= 0 {
		return false
	}
	facts := s.Host.Inspect(ctx, record.PID)
	if !facts.Alive || facts.StartedAt <= 0 {
		return false
	}
	return domain.Matches(record.StartedAt, facts.StartedAt, fingerprintTolerance)
}

// serversUsingCheckout lists every server of ours that is running from this
// checkout: the one this port's record describes and every other port's, found
// through the same predicate. The checkout is shared, so building or updating
// it replaces artifacts under all of them.
func (s *Service) serversUsingCheckout(ctx context.Context, observed observed) (servingPorts, error) {
	var serving servingPorts
	if pid, ok := s.runningPID(ctx, observed); ok {
		serving = append(serving, servingPort{port: s.Settings.Port, pid: pid})
	}
	others, err := s.otherPortsServing(ctx)
	if err != nil {
		return nil, err
	}
	return append(serving, others...), nil
}

// strangerOnPort reports whether the port is held by a process that is not part
// of the tree the record's start created.
//
// It is asked after our own server has been ended, to tell "the server did not
// release the port" — a failed stop — from "the server is gone and somebody
// else took the port", which is a stop that worked.
func (s *Service) strangerOnPort(ctx context.Context, record domain.Record) (int, bool) {
	result, err := s.Host.Listening(ctx, s.Settings.Port)
	if err != nil || !result.Listening || result.PID <= 0 {
		return 0, false
	}
	if result.PID == record.PID {
		return 0, false
	}
	if record.SpawnedPID > 0 && s.descendsFromSpawned(record.SpawnedPID, result.PID) {
		return 0, false
	}
	return result.PID, true
}

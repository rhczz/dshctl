package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/state"
)

// StopResult describes what a stop found and did.
type StopResult struct {
	// Status is the service state after the call.
	Status domain.Status `json:"status"`
	// Unverifiable reports that a process dshctl cannot vouch for owns the port,
	// so the caller cannot be told the port is free. For one named port that is
	// the whole answer; a multi-instance stop reads it as incomplete only when a
	// record of ours was there to be ended (see StopAllResult).
	Unverifiable bool `json:"unverifiable,omitempty"`
}

// Stop ends the managed server.
//
// The runtime record decides what is stopped, never the port: a recorded
// process that is alive and whose start time still matches is ended even when
// the port is held by somebody else or by nobody. A process the record does not
// vouch for is reported and left alone.
func (s *Service) Stop(ctx context.Context) (StopResult, error) {
	return withLockValue(ctx, s, func() (StopResult, error) { return s.stopLocked(ctx) })
}

// stopLocked performs the stop while the caller holds the lock.
func (s *Service) stopLocked(ctx context.Context) (StopResult, error) {
	observed, err := s.observe(ctx)
	if err != nil {
		return StopResult{}, err
	}

	// A survivor of an interrupted start is adopted first, so the process that
	// is actually serving the port becomes the one the record names and the
	// stop ends it like any other managed server.
	verdict, observed, err := s.admitSurvivor(ctx, observed)
	if err != nil {
		return StopResult{}, err
	}
	if verdict == adoptFailed {
		return StopResult{}, exitcode.New(exitcode.Preflight, i18nLine(MsgStopAdoptFailed),
			observed.status.ListenerPID)
	}
	if verdict == adoptDone {
		s.narrate(i18nLine(MsgStopAdopted))
	}

	// Whatever the port looks like, the command's job is to end the server this
	// state directory manages — as long as its identity is verified. The port's
	// occupant is a separate fact and is reported after the stop.
	if pid, ok := s.stopTarget(ctx, observed); ok {
		return s.shutdown(ctx, observed, pid)
	}

	return s.reportNothingToStop(ctx, observed)
}

// reportNothingToStop reports the port as it is and retires a record that
// describes nothing.
//
// "Unverifiable" means the caller cannot be told the port is free: something
// holds it that dshctl did not establish a right to end. Both states below report
// it, and they answer differently one level up — a single named port treats it as
// the complete answer to the question asked, while a stop that covered more
// counts it as incomplete only when a record of ours was there to be ended (see
// StopAllResult).
func (s *Service) reportNothingToStop(ctx context.Context, observed observed) (StopResult, error) {
	unverifiable := false
	switch observed.status.State {
	case domain.StateForeign:
		s.failure(i18nLine(MsgStopForeignSkipped, s.boundPort(), observed.status.ListenerPID, observed.status.ListenerCommand))
		unverifiable = true
	case domain.StateOrphan:
		// Something serves the port that dshctl cannot call its own. Stopping it
		// would mean guessing, and guessing here means killing a stranger.
		s.failure(i18nLine(MsgStopUnclaimedSkipped, s.boundPort(), observed.status.ListenerPID))
		s.failure(i18nLine(MsgStopUnclaimedTip))
		unverifiable = true
	default:
		s.narrate(i18nLine(MsgStopNotRunning))
	}

	s.clearStaleRecord(ctx, observed)
	final, err := s.observe(ctx)
	if err != nil {
		return StopResult{}, err
	}
	return StopResult{Status: final.status, Unverifiable: unverifiable}, nil
}

// shutdown terminates the server the record describes and verifies that it is
// gone.
//
// The pid is passed in because the server that runs is not always the pid the
// record names (a survivor is adopted before this call, so the two agree here).
func (s *Service) shutdown(ctx context.Context, observed observed, pid int) (StopResult, error) {
	s.narrate(i18nLine(MsgStopStopping, s.boundPort(), pid))

	// terminate is the single verified-signal path: it re-reads the record and
	// the process start time before every signal, so a pid recycled during the
	// grace period is reported instead of being force-killed.
	if err := s.terminate(ctx, pid); err != nil {
		return StopResult{}, exitcode.Wrap(exitcode.Failure, err)
	}
	if err := s.waitForStopped(ctx, s.Settings.StopTimeout); err != nil {
		// The recorded server is gone, but the port is still held. That is
		// only a failed stop when the holder is part of the tree the record
		// describes; a stranger that took the port is a note, not a failure.
		if stranger, ok := s.strangerOnPort(ctx, observed.record); ok {
			s.warning(i18nLine(MsgStopStrangerOnPort, s.boundPort(), stranger))
		} else {
			return StopResult{}, err
		}
	}
	if err := s.Record.Remove(); err != nil {
		s.warning(fmt.Sprintf("%v", err))
	}
	// The server is the tree dshctl created, not only the process that held the
	// port: a wrapper or a child left behind would be an unmanaged process. The
	// same rule cleanupFailedStart applies, applied here for the same reason.
	if spawned := observed.record.SpawnedPID; spawned > 0 {
		if err := s.endGroup(ctx, spawned); err != nil {
			s.warning(fmt.Sprintf("%v", err))
		}
	}
	final, err := s.observe(ctx)
	if err != nil {
		return StopResult{}, err
	}
	s.narrate(i18nLine(MsgStopStopped))
	return StopResult{Status: final.status}, nil
}

// terminate asks a process dshctl started to exit, forcing it after the grace
// period.
//
// The pid is verified before every signal: the record must name it and the
// process start time must still match, so a recycled pid is never signalled.
func (s *Service) terminate(ctx context.Context, pid int) error {
	record, hasRecord, err := s.Record.Load()
	if err != nil && !errors.Is(err, state.ErrCorrupt) {
		return err
	}
	facts := s.Host.Inspect(ctx, pid)
	if !facts.Alive {
		return nil
	}
	if hasRecord && record.PID == pid && !domain.Matches(record.StartedAt, facts.StartedAt, fingerprintTolerance) {
		return fmt.Errorf("%s", i18nLine(MsgStopRecycled, pid))
	}
	if err := s.Host.Signal(pid, hostGraceful); err != nil {
		s.warning(fmt.Sprintf("%v", err))
	}
	if s.waitForExit(ctx, pid, s.grace) {
		return nil
	}
	// Verify again before the force signal: the grace period is long enough for
	// a pid to be released and handed to something else, and an unverified
	// SIGKILL is exactly what the fingerprint exists to prevent.
	facts = s.Host.Inspect(ctx, pid)
	if !facts.Alive {
		return nil
	}
	if hasRecord && record.PID == pid && !domain.Matches(record.StartedAt, facts.StartedAt, fingerprintTolerance) {
		return fmt.Errorf("%s", i18nLine(MsgStopRecycledForce, pid))
	}
	if err := s.Host.Signal(pid, hostForce); err != nil {
		return err
	}
	if !s.waitForExit(ctx, pid, s.grace) {
		return fmt.Errorf("%s", i18nLine(MsgStopSurvivedForce, pid))
	}
	return nil
}

// waitForExit polls until pid no longer exists.
//
// Existence is asked with signal 0, which a zombie still answers: a process our
// own reaper has not collected yet counts as running here. The window is short
// (the detached child is reaped by its own goroutine) and erring this way is
// safe — the caller waits, then reports honestly instead of claiming a process
// is gone. Identity and "has it exited" are decided by Inspect, which reads the
// process state itself.
func (s *Service) waitForExit(ctx context.Context, pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !s.Host.Alive(ctx, pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// clearStaleRecord removes a record that describes nothing usable.
//
// "Stale" is narrower than "not currently the listener". A record whose process
// is alive and whose fingerprint still matches describes a server dshctl started,
// and deleting it would throw away the only handle on that process: the operator
// would have no way to stop it later. Only a record naming a process that is gone
// or has been recycled is retired here.
//
// A record that cannot be parsed at all is retired here as well. There is no pid
// to check and nothing usable to keep, and doctor tells the operator that the
// next start or stop rebuilds it — a promise that only holds when the mutating
// command actually removes the bytes it could not read.
func (s *Service) clearStaleRecord(ctx context.Context, observed observed) {
	if observed.hasRecord {
		record := observed.record
		if s.Host.Alive(ctx, record.PID) && s.RecordMatches(ctx, record, record.PID) {
			s.failure(i18nLine(MsgStopRecordKept, record.PID))
			return
		}
	} else if !observed.corrupt {
		// No record at all: there is nothing to retire.
		return
	}
	if err := s.Record.Remove(); err != nil {
		s.warning(fmt.Sprintf("%v", err))
	}
}

// Restart stops and then starts the server under one lock, so no other dshctl
// operation can interleave between the two halves.
//
// It refuses before stopping anything when the port is not ours: a restart that
// ended the server and then failed to start it again would be a half operation
// with nothing gained. The same precondition update states.
func (s *Service) Restart(ctx context.Context) (StartResult, error) {
	return withLockValue(ctx, s, func() (StartResult, error) {
		observed, err := s.observe(ctx)
		if err != nil {
			return StartResult{}, err
		}
		if observed.occupant() {
			return StartResult{}, exitcode.New(exitcode.Preflight,
				i18nLine(MsgRestartOccupant),
				s.boundPort(), observed.status.ListenerPID, observed.status.ListenerCommand)
		}
		if _, err := s.stopLocked(ctx); err != nil {
			return StartResult{}, err
		}
		return s.startLocked(ctx)
	})
}

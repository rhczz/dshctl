package service

import (
	"context"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
)

// This file holds the multi-instance half of the lifecycle: which servers a
// command acts on, and how one command covers all of them.
//
// One state directory manages every server dshctl started, one record per port.
// The port is therefore a *selector*: naming one on the command line means "just
// that instance", and leaving it out means "every instance this directory
// knows". Both halves matter. Naming a port is how an operator acts on one
// server while another keeps working; omitting it is what makes a server that
// was started with `--port` still visible and still stoppable, which is the
// failure this model replaced — a second start used to leave the first server
// serving with nothing in dshctl able to name it again.

// Statuses observes every instance the command was asked about.
//
// The configured port is always first and is observed without discovery, so the
// port a command names is reported even when nothing was ever started on it.
// Every instance this state directory holds a record for follows, in ascending
// port order.
//
// A discovered port that cannot be probed does not fail the report: its record
// is a fact on disk, and saying so is more useful than refusing to describe the
// instance the operator actually asked about. The failure is reported on that
// entry as its state, and the ports that could be looked at are reported as
// themselves. The configured port is the exception — see domain.Status, which keeps its
// "cannot look is not a fact" guarantee for the port the command is about.
//
// Returns:
//   - one status per selected port, in selection order.
//   - an error only when the configured port itself cannot be observed, or when
//     discovery cannot search the state directory.
func (s *Service) Statuses(ctx context.Context) ([]domain.Status, error) {
	selection, err := s.selection()
	if err != nil {
		return nil, err
	}
	statuses := make([]domain.Status, 0, len(selection.Ports))
	for _, port := range selection.Ports {
		status, err := s.Status(ctx, port)
		if err != nil {
			if port == s.boundPort() {
				return nil, err
			}
			// The instance could not be looked at. Reported as its own state
			// rather than folded into "stopped", which would claim knowledge
			// this command does not have.
			statuses = append(statuses, s.unobservable(port, err))
			continue
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// unobservable describes a discovered port that could not be probed.
func (s *Service) unobservable(port int, cause error) domain.Status {
	status := s.atPort(port).baseStatus()
	status.State = domain.StateUnobservable
	status.ProbeError = cause.Error()
	// The record is still read: it is what says whether a server was ever
	// started here, and a report that hid it would leave the operator with a
	// port number and nothing else.
	if record, ok, err := s.atPort(port).Record.Load(); err == nil && ok {
		status.RecordedPID = record.PID
		status.RecordedPhase = string(record.Phase)
		status.RecordedNodeVersion = record.NodeVersion
		status.RecordedNodePath = record.NodePath
		status.RecordedRepoDir = record.RepoDir
		status.URLFromRecord = record.URL
	}
	return status
}

// StopAllResult is what a multi-instance stop did, one entry per instance.
type StopAllResult struct {
	// Results is one outcome per selected instance, in selection order.
	Results []StopResult `json:"results"`
	// Unverifiable reports that a *multi-instance* stop could not cover
	// everything it was asked to: something dshctl cannot vouch for holds one of
	// the ports. It is not about a named port, where leaving the occupant alone
	// is the answer the operator asked for and the report already says so.
	Unverifiable bool `json:"unverifiable,omitempty"`
}

// StopAll ends every instance the command was asked about: one port when the
// operator named one, every instance of this state directory otherwise.
//
// The instances are ended one after another under a single lock, so no other
// dshctl operation can interleave between them, and a server that refuses to die
// is reported without leaving the remaining instances untouched.
//
// Returns:
//   - the outcome of every instance, in selection order.
//   - the error of the instance that could not be ended.
func (s *Service) StopAll(ctx context.Context) (StopAllResult, error) {
	return stickyLock(ctx, s, func() (StopAllResult, error) {
		selection, err := s.selection()
		if err != nil {
			return StopAllResult{}, err
		}
		var result StopAllResult
		for _, port := range selection.Ports {
			stopped, err := s.stopOneLocked(ctx, port)
			if err != nil {
				return result, err
			}
			// Only a selection that covers more than the named instance can be
			// *incomplete*: a single port that turned out to belong to somebody
			// else is a complete answer, and the exit code says so.
			if stopped.Unverifiable && len(selection.Ports) > 1 {
				result.Unverifiable = true
			}
			result.Results = append(result.Results, stopped)
		}
		return result, nil
	})
}

// stopOneLocked ends one instance while the caller holds the lock.
//
// The lock is held once for a whole multi-instance operation rather than per
// port: the operations are separate servers but one state directory, and letting
// another command interleave between them is how a status ends up describing
// half of a stop.
func (s *Service) stopOneLocked(ctx context.Context, port int) (StopResult, error) {
	return s.atPort(port).stopLocked(ctx)
}

// RestartAll restarts every instance the command was asked about.
//
// Every precondition is checked before anything is ended: a port this dshctl
// cannot claim blocks the whole restart rather than leaving the machine with one
// server stopped and one untouched. The instances that were running come back
// one by one; an instance that was already down is not started, because a bare
// restart must not turn one running server into several.
//
// Returns:
//   - one start result per instance that was running, in selection order.
//   - the error that stopped the sequence.
func (s *Service) RestartAll(ctx context.Context) ([]StartResult, error) {
	return stickyLock(ctx, s, func() ([]StartResult, error) {
		selection, err := s.selection()
		if err != nil {
			return nil, err
		}
		for _, port := range selection.Ports {
			observed, err := s.atPort(port).observe(ctx)
			if err != nil {
				return nil, err
			}
			if observed.occupant() {
				return nil, exitcode.New(exitcode.Preflight,
					"端口 %d 被 dshctl 无法确认归属的进程占用 (pid=%d): %s\n提示: 先确认并处理它,再执行重启",
					port, observed.status.ListenerPID, observed.status.ListenerCommand)
			}
		}
		running, err := s.runningSelection(ctx, selection.Ports)
		if err != nil {
			return nil, err
		}
		if len(running) == 0 {
			// Nothing was serving, so a restart is a start — exactly what the
			// single-port restart does with no server running.
			started, err := s.startLocked(ctx)
			if err != nil {
				return nil, err
			}
			return []StartResult{started}, nil
		}
		for _, port := range running {
			if _, err := s.stopOneLocked(ctx, port); err != nil {
				return nil, err
			}
		}
		results := make([]StartResult, 0, len(running))
		for _, port := range running {
			started, err := s.atPort(port).startLocked(ctx)
			if err != nil {
				return results, err
			}
			results = append(results, started)
		}
		return results, nil
	})
}

// runningSelection reports which of the selected instances are serving now and
// may be replaced by a restart.
func (s *Service) runningSelection(ctx context.Context, ports []int) ([]int, error) {
	var running []int
	for _, port := range ports {
		observed, err := s.atPort(port).observe(ctx)
		if err != nil {
			return nil, err
		}
		// The lenient predicate, like the single-port restart: an instance whose
		// process is alive but whose port it lost is still a server of ours, and
		// a restart is exactly what brings it back. What the caller may *signal*
		// is the stricter question, and stopTarget answers it port by port.
		if _, ok := s.atPort(port).runningPID(ctx, observed); ok {
			running = append(running, port)
		}
	}
	return running, nil
}

// URLReport is what `url` found: the addresses of the running instances, the
// observation of each instance, and the instance the command was about.
type URLReport struct {
	// Statuses is every observed instance: the one the command was about first,
	// then the rest in ascending port order. It is what lets the caller report
	// the instances that have no address yet instead of printing fewer lines and
	// saying nothing about why.
	Statuses []domain.Status
	// Addresses maps a port to its token-carrying address.
	Addresses map[int]string
	// Status is the instance the command was about: the configured port, or the
	// one that was named. Its exit code answers the question that was asked.
	Status domain.Status
}

// URLReport reports the token-carrying address of every instance, together with
// what was observed of each one.
//
// An instance that is running but has no address yet is reported as a status
// without an address rather than as a failure — unless it is the instance the
// command was about, because then the operator asked a question that has no
// answer yet and has to hear it. A port that could not be probed never becomes
// "there is no address": it stays in Statuses as unobservable.
func (s *Service) URLReport(ctx context.Context) (URLReport, error) {
	statuses, err := s.Statuses(ctx)
	if err != nil {
		return URLReport{}, err
	}
	report := URLReport{
		Statuses:  statuses,
		Addresses: make(map[int]string, len(statuses)),
	}
	for _, status := range statuses {
		if status.Port == s.boundPort() {
			report.Status = status
		}
		if !status.Owning() && !status.Survivor {
			continue
		}
		address, err := s.atPort(status.Port).observedAddress(status)
		if err != nil {
			if status.Port == s.boundPort() {
				return URLReport{}, err
			}
			continue
		}
		report.Addresses[status.Port] = address
	}
	return report, nil
}

// WebURLs reports the address of every running instance, keyed by port.
func (s *Service) WebURLs(ctx context.Context) (map[int]string, error) {
	report, err := s.URLReport(ctx)
	if err != nil {
		return nil, err
	}
	return report.Addresses, nil
}

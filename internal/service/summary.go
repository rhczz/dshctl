package service

import (
	"fmt"

	"github.com/rhczz/dshctl/internal/domain"
)

// StatusSummary explains an unactionable state in one line.
//
// It is the one-line explanation a command prints, and the reason `status`,
// `stop` and `url` describe the same observation in the same words instead of
// each spelling the states its own way.
func StatusSummary(status domain.Status) string {
	switch status.State {
	case domain.StateRunning:
		return "running"
	case domain.StateStarting:
		return "starting"
	case domain.StateForeign:
		return fmt.Sprintf("port %d is held by another program (pid=%d)", status.Port, status.ListenerPID)
	case domain.StateOrphan:
		if status.Survivor {
			return fmt.Sprintf("port %d is served by a survivor of an interrupted start (pid=%d)", status.Port, status.ListenerPID)
		}
		return fmt.Sprintf("port %d is held by a process dshctl cannot claim (pid=%d); the runtime record is missing or contradicts it", status.Port, status.ListenerPID)
	case domain.StateUnobservable:
		return fmt.Sprintf("port %d cannot be probed: %s", status.Port, status.ProbeError)
	default:
		if status.RecordLive {
			return fmt.Sprintf("not listening on port %d (the recorded pid %d is still alive)", status.Port, status.RecordedPID)
		}
		return "not running"
	}
}

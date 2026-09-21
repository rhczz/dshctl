package kernel

import (
	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/i18n"
)

// StatusSummary explains an unactionable state in one line.
//
// It is the one-line explanation a command prints, and the reason `status`,
// `stop` and `url` describe the same observation in the same words instead of
// each spelling the states its own way. The words themselves live in the i18n
// catalog: this function decides which message an observation is, and nothing
// about which language it is read in.
func StatusSummary(status domain.Status) string {
	switch status.State {
	case domain.StateRunning:
		return i18n.T(MsgStateRunning)
	case domain.StateStarting:
		return i18n.T(MsgStateStarting)
	case domain.StateForeign:
		return i18n.T(MsgStateOccupied, status.Port, status.ListenerPID)
	case domain.StateOrphan:
		if status.Survivor {
			return i18n.T(MsgStateSurvivor, status.Port, status.ListenerPID)
		}
		return i18n.T(MsgStateUnmanaged, status.Port, status.ListenerPID)
	case domain.StateUnobservable:
		return i18n.T(MsgStateUnobservable, status.Port, status.ProbeError)
	default:
		if status.RecordLive {
			return i18n.T(MsgStateNotListening, status.Port, status.RecordedPID)
		}
		return i18n.T(MsgStateStopped)
	}
}

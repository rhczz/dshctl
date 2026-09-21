package kernel

import (
	"context"

	"github.com/rhczz/dshctl/internal/domain"
)

// This file is the one place a mutating verb decides what an observed state
// means for it.
//
// Before it, start, stop, restart, update and the cross-port guard each asked
// the same two questions with their own copy of the logic — "this survivor is
// ours, adopt it" and "that occupant is not ours, refuse it" — so the copies
// could drift apart while the same state was read two ways by two commands.
//
// The wording stays per verb on purpose. A message is the operator's interface
// and says what this command was about to do; the verdicts here are the model
// those messages are produced from.

// adoption is what became of the survivor of an interrupted start.
type adoption int

const (
	// adoptNothing means the observation did not describe a survivor.
	adoptNothing adoption = iota
	// adoptDone means the serving process is now the record's server, and the
	// returned observation describes it.
	adoptDone
	// adoptFailed means a survivor holds the port but the record could not be
	// rebuilt, so nothing may signal it. Only a human can end it.
	adoptFailed
)

// admitSurvivor turns "a server of ours holds the port although the record does
// not name it" into "the record names it", so the verb that is about to act sees
// an ordinary managed instance.
//
// It is asked before anything else, and it re-observes after a successful
// adoption: the caller must act on the state the record now describes, not on
// the state that made adoption necessary.
func (s *Service) admitSurvivor(ctx context.Context, observed observed) (adoption, observed, error) {
	if !observed.status.Survivor {
		return adoptNothing, observed, nil
	}
	if _, ok := s.adoptSurvivor(ctx, observed); !ok {
		return adoptFailed, observed, nil
	}
	final, err := s.observe(ctx)
	if err != nil {
		return adoptFailed, observed, err
	}
	return adoptDone, final, nil
}

// occupant reports whether the port is held by something dshctl may not act
// on: another program, or a process whose ownership it cannot establish. A
// survivor is not an occupant — it is ours, and the verb adopts it first, so by
// the time this is asked the only orphans left are ones nobody can claim.
func (o observed) occupant() bool {
	return o.status.State == domain.StateForeign ||
		(o.status.State == domain.StateOrphan && !o.status.Survivor)
}

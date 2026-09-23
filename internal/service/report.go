package service

import "github.com/rhczz/dshctl/internal/domain"

// StatusReport is what `status` observed.
//
// One state directory may manage several servers, so the report carries the
// instance the command was about — the configured port, or the one named on the
// command line — beside every instance this state directory holds a record for.
// The two are the same list for a single-instance installation, and the shape
// stays an object because that is what every existing consumer of `--json`
// reads.
type StatusReport struct {
	// Status is the instance the command was about.
	Status domain.Status `json:"status"`
	// Ports lists every observed instance: the instance the command was about
	// first, then the rest in ascending port order. It is what makes a server
	// started with another port visible instead of orphaned.
	Ports []domain.Status `json:"ports,omitempty"`
	// Others is the instances worth naming beside the one above: the ones that
	// are serving, or that need attention. An instance that is simply not running
	// is left out, because a report is not a roll call of everything that is off.
	Others []domain.Status `json:"-"`
}

// NewStatusReport assembles the report from the observed instances.
//
// The instance the command was about is always first: the selection puts the
// configured port there, or the one that was named, so a report of a single
// instance is that instance.
func NewStatusReport(statuses []domain.Status) StatusReport {
	report := StatusReport{Ports: statuses}
	if len(statuses) == 0 {
		return report
	}
	report.Status = statuses[0]
	report.Others = make([]domain.Status, 0, len(statuses)-1)
	for _, status := range statuses[1:] {
		if status.Port == report.Status.Port || status.State == domain.StateStopped {
			continue
		}
		report.Others = append(report.Others, status)
	}
	return report
}

// Package cli parses the command line and dispatches to the service layer.
//
// This file is the shell's rendering of a read-only command's result: the
// status block, the address list, the doctor rows and the timeline. The service
// layer returns the values; the front-end decides how they read — as text here,
// as JSON through `--json`, as frames in a future front-end.
package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/service"
)

// serveExitCode maps an observed state onto the process exit code, so a shell
// condition can ask whether the service is up.
//
// Running and starting both report success: the service exists and is being
// managed. Everything else reports "not running".
func serveExitCode(status domain.Status) int {
	if status.Owning() {
		return exitcode.OK
	}
	return exitcode.NotRunning
}

// printStatuses writes the human-readable status report for every observed
// instance, and the token-carrying address of the extra ones to extra.
//
// The first block is the instance the command was about and follows the shape
// `status` has always had. When this state directory manages more, each of them
// is named rather than left out: an operator who started a server with `--port`
// has to be able to see that it is still running, and where to reach it. Those
// lines go to extra — standard error for the command — because they are notes
// about other instances, while standard output stays the report of the instance
// that was asked about.
func printStatuses(w, extra io.Writer, report service.StatusReport) error {
	if err := printStatus(w, report.Status); err != nil {
		return err
	}
	for _, status := range report.Others {
		if _, err := fmt.Fprintf(extra, "\n"+i18nLine(MsgPortHeading)+"\n", status.Port); err != nil {
			return err
		}
		if err := printStatus(extra, status); err != nil {
			return err
		}
	}
	return nil
}

// printURLs writes one token-carrying address per line, in ascending port order.
//
// Every address that was found is printed, because an operator asking for "the
// address" of an installation that runs two servers needs both; a script that
// wants one address names its port. What was *not* found is not silently
// missing either: an instance that is running but has no address yet is named on
// standard error, and the caller turns an empty report into "not running".
func printURLs(w io.Writer, extra io.Writer, report service.URLReport) error {
	ports := make([]int, 0, len(report.Addresses))
	for port := range report.Addresses {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	for _, port := range ports {
		if _, err := fmt.Fprintln(w, report.Addresses[port]); err != nil {
			return err
		}
	}
	for _, status := range report.Statuses {
		if _, done := report.Addresses[status.Port]; done {
			continue
		}
		// The instance the command was about is not explained twice: when there
		// is simply nothing running there, the caller says so once — with an
		// error, or with the exit code the command is documented to return.
		if status.Port == report.Status.Port && !status.Owning() && !status.Survivor {
			continue
		}
		if _, err := fmt.Fprintf(extra, i18nLine(MsgNoAddressOnPort)+"\n", status.Port, service.StatusSummary(status)); err != nil {
			return err
		}
	}
	return nil
}

// printStatus writes the human-readable status report.
func printStatus(w io.Writer, status domain.Status) error {
	summary := service.StatusSummary(status)
	switch status.State {
	case domain.StateRunning:
		if _, err := fmt.Fprintf(w, i18nLine(MsgStatusRunningBlock), status.URL, status.ListenerPID); err != nil {
			return err
		}
		if status.URLFromRecord != "" {
			if _, err := fmt.Fprintf(w, i18nLine(MsgStatusTokenLine)+"\n", status.URLFromRecord); err != nil {
				return err
			}
		}
		// The subject of this branch is the service that is running, so the
		// checkout line names the tree that process serves. The configured one is
		// named too when the two differ: --repo applies to one invocation, and a
		// status that printed only the configuration would describe a directory
		// the running instance never used.
		checkout := status.RepoDir
		note := ""
		switch {
		case status.RecordedRepoDir != "":
			checkout = status.RecordedRepoDir
			if status.RecordedRepoDir != status.RepoDir {
				note = i18nLine(MsgStatusConfiguredIs, status.RepoDir)
			}
		case checkout != "":
			note = i18nLine(MsgStatusNoRecordedRepo)
		}
		if _, err := fmt.Fprintf(w, i18nLine(MsgStatusCheckoutLog)+"\n", checkout, note, status.LogPath); err != nil {
			return err
		}
		return nil
	case domain.StateStarting:
		if _, err := fmt.Fprintf(w, i18nLine(MsgStatusStartingBlock)+"\n", status.RecordedPID, status.LogPath); err != nil {
			return err
		}
		return nil
	case domain.StateForeign:
		if _, err := fmt.Fprintf(w, i18nLine(MsgStatusForeignBlock)+"\n",
			status.URL, status.ListenerCommand, status.LogPath); err != nil {
			return err
		}
		return nil
	case domain.StateOrphan:
		if _, err := fmt.Fprintf(w, i18nLine(MsgStatusOrphanBlock)+"\n", status.URL, status.ListenerCommand); err != nil {
			return err
		}
		if status.Survivor {
			_, err := fmt.Fprintln(w, i18nLine(MsgStatusSurvivorHint))
			return err
		}
		_, err := fmt.Fprintln(w, i18nLine(MsgStatusOrphanHint))
		return err
	case domain.StateUnobservable:
		// Nothing about this instance is known, and saying "not running" would
		// be a claim the failed probe cannot support.
		if _, err := fmt.Fprintf(w, i18nLine(MsgStatusUnobservable)+"\n", status.Port, status.ProbeError); err != nil {
			return err
		}
		if status.RecordedPID != 0 {
			if _, err := fmt.Fprintf(w, i18nLine(MsgStatusRecordPid)+"\n", status.RecordedPID); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintln(w, i18nLine(MsgStatusUnobservableTip))
		return err
	default:
		if _, err := fmt.Fprintf(w, i18nLine(MsgStatusGeneric)+"\n", summary); err != nil {
			return err
		}
		if status.RecordLive {
			if _, err := fmt.Fprintf(w, i18nLine(MsgStatusRecordLive)+"\n", status.RecordedPID, status.Port); err != nil {
				return err
			}
		}
		if status.RecordStale && status.StaleRecord != nil {
			if _, err := fmt.Fprintf(w, i18nLine(MsgStatusStaleRecord)+"\n", status.StaleRecord.PID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, i18nLine(MsgStatusLog)+"\n", status.LogPath); err != nil {
			return err
		}
		return nil
	}
}

// printChecks writes the human-readable diagnosis.
func printChecks(w io.Writer, checks []service.Check) error {
	for _, check := range checks {
		label := i18nLine(MsgCheckOK)
		switch check.Status {
		case service.CheckWarn:
			label = i18nLine(MsgCheckWarn)
		case service.CheckFail:
			label = i18nLine(MsgCheckFail)
		}
		if _, err := fmt.Fprintf(w, i18nLine(MsgCheckLine)+"\n", label, check.Name, check.Detail); err != nil {
			return err
		}
	}
	return nil
}

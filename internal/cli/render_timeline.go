package cli

// This file is the shell's rendering of the timeline report. The service layer
// computes it; how it reads is the front-end's business.

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/history"
	"github.com/rhczz/dshctl/internal/service"
)

// PrintTimeline writes the human-readable report.
func printTimeline(w io.Writer, report service.TimelineReport) error {
	if _, err := fmt.Fprintf(w, i18nLine(MsgTimelineCheckout), report.RepoDir); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, i18nLine(MsgTimelineCurrent), report.Current.Short, currentName(report.Current)); err != nil {
		return err
	}
	if report.Fetched {
		remote := report.Remote.Short + " (" + report.Remote.Name
		if report.Remote.Tag != "" {
			remote += ", tag " + report.Remote.Tag
		}
		if _, err := fmt.Fprintf(w, i18nLine(MsgTimelineRemote), remote); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, i18nLine(MsgTimelineRemoteFailed), report.FetchError); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, i18nLine(MsgTimelineGap), timelineGap(report)); err != nil {
		return err
	}
	switch {
	case report.Dirty:
		if _, err := fmt.Fprintln(w, i18nLine(MsgTimelineDirty)); err != nil {
			return err
		}
	case report.DirtyError != "":
		if _, err := fmt.Fprintf(w, i18nLine(MsgTimelineDirtyUnknown), report.DirtyError); err != nil {
			return err
		}
	}
	if len(report.Commits) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		for _, commit := range report.Commits {
			if commit.Skipped > 0 {
				if _, err := fmt.Fprintf(w, i18nLine(MsgTimelineElided), commit.Skipped); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(w, timelineCommitLine(commit)); err != nil {
				return err
			}
		}
	}
	if len(report.History) > 0 {
		if _, err := fmt.Fprintln(w, i18nLine(MsgTimelineHistoryHeader)); err != nil {
			return err
		}
		for _, record := range report.History {
			if _, err := fmt.Fprintln(w, timelineHistoryLine(record, report.Current.Commit)); err != nil {
				return err
			}
		}
		if report.HistoryTotal > len(report.History) {
			if _, err := fmt.Fprintf(w, i18nLine(MsgTimelineHistoryMore),
				report.HistoryTotal-len(report.History)); err != nil {
				return err
			}
		}
	}
	return nil
}

// currentName names the current position for the header.
func currentName(current service.TimelineCurrent) string {
	switch {
	case current.Tag != "":
		return "tag " + current.Tag
	case current.Branch != "":
		return current.Branch
	default:
		return i18nLine(MsgTimelineDetached)
	}
}

// timelineGap renders the one-line answer to "how far behind am I".
//
// A failed fetch must never produce the up-to-date message: the gap may be zero
// only against the last known remote state, and the line says so.
func timelineGap(report service.TimelineReport) string {
	unconfirmed := i18nLine(MsgTimelineUnconfirmed)
	switch {
	case report.Behind == 0 && report.Ahead == 0:
		if report.Fetched {
			return i18nLine(MsgTimelineUpToDate, report.Remote.Name)
		}
		return i18nLine(MsgTimelineSameAsLocal, report.Remote.Name) + unconfirmed
	case report.Behind > 0 && report.Ahead == 0:
		tagged := 0
		for _, commit := range report.Commits {
			if !commit.Current && len(commit.Tags) > 0 {
				tagged++
			}
		}
		line := i18nLine(MsgTimelineBehind, report.Behind)
		if tagged == 0 {
			line += i18nLine(MsgTimelineBehindNoTags)
		} else {
			line += i18nLine(MsgTimelineBehindTags, tagged)
		}
		return line + unconfirmedIf(report, unconfirmed)
	case report.Behind == 0:
		return i18nLine(MsgTimelineAhead, report.Ahead) +
			unconfirmedIf(report, unconfirmed)
	default:
		return i18nLine(MsgTimelineDiverged, report.Remote.Name, report.Behind, report.Ahead) + unconfirmedIf(report, unconfirmed)
	}
}

// unconfirmedIf appends the qualifier a failed fetch requires.
func unconfirmedIf(report service.TimelineReport, qualifier string) string {
	if report.Fetched {
		return ""
	}
	return qualifier
}

// timelineCommitLine renders one commit row.
func timelineCommitLine(commit service.TimelineCommit) string {
	marker := "  "
	switch {
	case commit.Current:
		marker = "● "
	case commit.Remote:
		marker = "○ "
	}
	line := marker + commit.Short + "  "
	if len(commit.Tags) > 0 {
		line += strings.Join(commit.Tags, ", ") + "  "
	}
	line += commit.Subject
	switch {
	case commit.Current:
		line += i18nLine(MsgTimelineMarkCurrent)
	case commit.Remote:
		line += i18nLine(MsgTimelineMarkRemote)
	}
	return line
}

// timelineHistoryLine renders one recorded deployment.
func timelineHistoryLine(record history.Record, current string) string {
	marker := "  "
	if record.Commit == current {
		marker = "● "
	}
	selector := record.Selector
	if selector == "" {
		selector = "-"
	}
	return fmt.Sprintf(i18nLine(MsgTimelineHistoryLine), marker, domain.ShortCommit(record.Commit), selector,
		time.Unix(record.At, 0).Format("2006-01-02 15:04"))
}

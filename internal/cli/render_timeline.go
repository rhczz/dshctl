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
	if _, err := fmt.Fprintf(w, "仓库: %s\n", report.RepoDir); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "当前: %s (%s)\n", report.Current.Short, currentName(report.Current)); err != nil {
		return err
	}
	if report.Fetched {
		remote := report.Remote.Short + " (" + report.Remote.Name
		if report.Remote.Tag != "" {
			remote += ", tag " + report.Remote.Tag
		}
		if _, err := fmt.Fprintf(w, "远程: %s)\n", remote); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "远程: 无法获取（%s）\n", report.FetchError); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "差距: %s\n", timelineGap(report)); err != nil {
		return err
	}
	switch {
	case report.Dirty:
		if _, err := fmt.Fprintln(w, "工作区: 有未提交修改（update/rollback 会拒绝，请先处理）"); err != nil {
			return err
		}
	case report.DirtyError != "":
		if _, err := fmt.Fprintf(w, "工作区: 无法确认（%s）\n", report.DirtyError); err != nil {
			return err
		}
	}
	if len(report.Commits) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		for _, commit := range report.Commits {
			if commit.Skipped > 0 {
				if _, err := fmt.Fprintf(w, "  … 省略 %d 个提交 …\n", commit.Skipped); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(w, timelineCommitLine(commit)); err != nil {
				return err
			}
		}
	}
	if len(report.History) > 0 {
		if _, err := fmt.Fprintln(w, "\n更新历史:"); err != nil {
			return err
		}
		for _, record := range report.History {
			if _, err := fmt.Fprintln(w, timelineHistoryLine(record, report.Current.Commit)); err != nil {
				return err
			}
		}
		if report.HistoryTotal > len(report.History) {
			if _, err := fmt.Fprintf(w, "  … 还有 %d 条（--json 查看）\n",
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
		return "detached"
	}
}

// timelineGap renders the one-line answer to "how far behind am I".
//
// A failed fetch must never produce the words "已是最新": the gap may be zero
// only against the last known remote state, and the line says so.
func timelineGap(report service.TimelineReport) string {
	unconfirmed := "（基于本地已知状态，远程未确认）"
	switch {
	case report.Behind == 0 && report.Ahead == 0:
		if report.Fetched {
			return "已是最新（" + report.Remote.Name + "）"
		}
		return "与本地已知的 " + report.Remote.Name + " 相同" + unconfirmed
	case report.Behind > 0 && report.Ahead == 0:
		tagged := 0
		for _, commit := range report.Commits {
			if !commit.Current && len(commit.Tags) > 0 {
				tagged++
			}
		}
		line := fmt.Sprintf("落后 %d 个提交", report.Behind)
		if tagged == 0 {
			line += "（中间没有新 tag）"
		} else {
			line += fmt.Sprintf("（中间有 %d 个 tag）", tagged)
		}
		return line + unconfirmedIf(report, unconfirmed)
	case report.Behind == 0:
		return fmt.Sprintf("本地领先 %d 个提交（未推送，update 无法快进）", report.Ahead) +
			unconfirmedIf(report, unconfirmed)
	default:
		return fmt.Sprintf("与 %s 分叉：落后 %d 个、本地领先 %d 个（update 无法快进）",
			report.Remote.Name, report.Behind, report.Ahead) + unconfirmedIf(report, unconfirmed)
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
		line += "   ← 当前"
	case commit.Remote:
		line += "   ← 远程最新"
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
	return fmt.Sprintf("%s%s  %-16s  %s", marker, domain.ShortCommit(record.Commit), selector,
		time.Unix(record.At, 0).Format("2006-01-02 15:04"))
}

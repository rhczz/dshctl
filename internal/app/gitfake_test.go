package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/rhczz/dshctl/internal/repo"
	"github.com/rhczz/dshctl/internal/run"
)

// fakeGitCommit is one entry of the fixture's scripted history.
type fakeGitCommit struct {
	Full    string
	Short   string
	Subject string
}

// fakeCommit builds a history entry, abbreviating like git does.
func fakeCommit(full, subject string) fakeGitCommit {
	short := full
	if len(short) > 7 {
		short = short[:7]
	}
	return fakeGitCommit{Full: full, Short: short, Subject: subject}
}

// gitResult answers one git invocation from the fixture's model.
//
// Captured queries and streamed commands share this one answer, so a test
// cannot pass because the two paths disagreed about the same repository. The
// caller must not hold the host lock.
func (h *fakeHost) gitResult(cmd run.Command) run.Result {
	h.mu.Lock()
	defer h.mu.Unlock()
	fail := func(code int) run.Result {
		return run.Result{Err: &run.ExitError{Command: cmd.String(), Code: code}}
	}
	switch {
	case hasArgument(cmd, "config", "--get"):
		if h.gitOrigin == "" {
			return fail(1)
		}
		return run.Result{Stdout: h.gitOrigin}
	case hasArgument(cmd, "fetch"):
		// A checkout without an origin cannot be fetched from: the same answer
		// real git gives, so the origin preflight is exercised faithfully.
		if h.gitOrigin == "" {
			return fail(128)
		}
		return run.Result{Stderr: "fake git: fetch\n"}
	case hasArgument(cmd, "rev-parse", "--symbolic-full-name"):
		// The answer the branch refusal reads: a branch (and HEAD while it
		// points at one) resolves through refs/heads; a remote-tracking ref
		// through refs/remotes; a commit does not resolve at all.
		selector := lastArgument(cmd)
		switch {
		case selector == "HEAD" && h.gitBranch != "":
			return run.Result{Stdout: "refs/heads/" + h.gitBranch}
		case selector == h.gitBranch && h.gitBranch != "":
			return run.Result{Stdout: "refs/heads/" + h.gitBranch}
		case selector == repo.RemoteTipName:
			return run.Result{Stdout: "refs/remotes/" + repo.RemoteTipName}
		default:
			return run.Result{}
		}
	case hasArgument(cmd, "rev-parse", "--verify", "origin/master^{commit}"):
		if h.gitRemote == "" {
			return fail(128)
		}
		return run.Result{Stdout: h.gitRemote}
	case hasArgument(cmd, "rev-parse", "--verify"):
		return h.resolveVerify(cmd, fail)
	case hasArgument(cmd, "rev-parse", "--short"):
		return run.Result{Stdout: h.shortHead()}
	case hasArgument(cmd, "rev-parse", "--abbrev-ref"):
		if h.gitBranch == "" {
			return run.Result{Stdout: "HEAD"}
		}
		return run.Result{Stdout: h.gitBranch}
	case hasArgument(cmd, "rev-parse", "HEAD"):
		return run.Result{Stdout: h.gitHead}
	case hasArgument(cmd, "describe", "--tags", "--exact-match"):
		for name, sha := range h.gitTags {
			if sha == h.gitHead {
				return run.Result{Stdout: name}
			}
		}
		return fail(128)
	case hasArgument(cmd, "rev-list", "--count"):
		return run.Result{Stdout: strconv.Itoa(h.rangeCount(cmd))}
	case hasArgument(cmd, "log", "-1"):
		return h.commitInfo(cmd, fail)
	case hasArgument(cmd, "log", "--first-parent"):
		return run.Result{Stdout: h.logOutput()}
	case hasArgument(cmd, "for-each-ref"):
		return run.Result{Stdout: h.tagOutput()}
	case hasArgument(cmd, "merge-base", "--is-ancestor"):
		if h.gitAncestor {
			return run.Result{}
		}
		return fail(1)
	case hasArgument(cmd, "status", "--porcelain"):
		return run.Result{Stdout: h.gitStatus}
	case hasArgument(cmd, "checkout", "--detach"):
		return h.checkoutDetach(cmd, fail)
	case hasArgument(cmd, "checkout"):
		h.gitHead = h.masterTip()
		h.gitBranch = "master"
		return run.Result{Stderr: "fake git: checkout master\n"}
	case hasArgument(cmd, "merge", "--ff-only"):
		h.gitHead = h.gitRemote
		return run.Result{Stderr: "fake git: merge --ff-only\n"}
	case hasArgument(cmd, "ls-files"):
		return run.Result{Stdout: h.trackedFiles}
	}
	return run.Result{}
}

// resolveVerify answers `rev-parse --verify <selector>^{commit}` from the
// fixture's refs. The caller holds the host lock.
func (h *fakeHost) resolveVerify(cmd run.Command, fail func(int) run.Result) run.Result {
	selector := lastArgument(cmd)
	selector = strings.TrimSuffix(selector, "^{commit}")
	selector = strings.TrimPrefix(selector, "refs/tags/")
	if sha, ok := h.gitTags[selector]; ok {
		return run.Result{Stdout: sha}
	}
	if selector == "HEAD" {
		return run.Result{Stdout: h.gitHead}
	}
	if sha := h.commitByPrefix(selector); sha != "" {
		return run.Result{Stdout: sha}
	}
	return fail(128)
}

// checkoutDetach answers `checkout --detach <commit>`: only a known commit
// moves the head, exactly as git refuses an object it cannot check out. The
// caller holds the host lock.
func (h *fakeHost) checkoutDetach(cmd run.Command, fail func(int) run.Result) run.Result {
	sha := lastArgument(cmd)
	if !h.knownCommit(sha) {
		return fail(128)
	}
	h.gitHead = sha
	h.gitBranch = ""
	return run.Result{Stderr: "fake git: checkout --detach\n"}
}

// knownCommit reports whether the fixture has this commit. The caller holds the
// host lock.
func (h *fakeHost) knownCommit(sha string) bool {
	return h.commitByPrefix(sha) != ""
}

// commitByPrefix resolves a full or abbreviated commit the fixture knows, the
// way git resolves a short sha. The caller holds the host lock.
func (h *fakeHost) commitByPrefix(selector string) string {
	if selector == "" {
		return ""
	}
	known := []string{h.gitHead, h.gitRemote, h.masterTip()}
	for _, commit := range h.gitLog {
		known = append(known, commit.Full)
	}
	for _, tagSha := range h.gitTags {
		known = append(known, tagSha)
	}
	for _, sha := range known {
		if sha != "" && (sha == selector || strings.HasPrefix(sha, selector)) {
			return sha
		}
	}
	return ""
}

// commitInfo answers `log -1 --format=... <revision>`. The caller holds the host
// lock.
func (h *fakeHost) commitInfo(cmd run.Command, fail func(int) run.Result) run.Result {
	revision := lastArgument(cmd)
	if revision == h.gitHead {
		return run.Result{Stdout: formatGitCommit(fakeGitCommit{
			Full: h.gitHead, Short: h.shortHead(), Subject: h.gitHeadSubject,
		})}
	}
	for _, commit := range h.gitLog {
		if commit.Full == revision || commit.Short == revision {
			return run.Result{Stdout: formatGitCommit(commit)}
		}
	}
	return fail(128)
}

// rangeCount answers `rev-list --count <from>..<to>`. The caller holds the host
// lock.
func (h *fakeHost) rangeCount(cmd run.Command) int {
	for _, arg := range cmd.Args {
		from, to, ok := strings.Cut(arg, "..")
		if !ok {
			continue
		}
		switch {
		case from == h.gitHead && to == h.gitRemote:
			return h.gitBehind
		case from == h.gitRemote && to == h.gitHead:
			return h.gitAhead
		}
	}
	return 0
}

// logOutput renders the scripted first-parent history, newest first. The caller
// holds the host lock.
func (h *fakeHost) logOutput() string {
	var builder strings.Builder
	for _, commit := range h.gitLog {
		builder.WriteString(formatGitCommit(commit))
		builder.WriteByte('\n')
	}
	return builder.String()
}

// tagOutput renders `for-each-ref` for lightweight commit tags. The caller
// holds the host lock.
func (h *fakeHost) tagOutput() string {
	names := make([]string, 0, len(h.gitTags))
	for name := range h.gitTags {
		names = append(names, name)
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		fmt.Fprintf(&builder, "%s%c%ccommit%c%c%s\n", h.gitTags[name], 0, 0, 0, 0, name)
	}
	return builder.String()
}

// shortHead abbreviates the head the way git does. The caller holds the host
// lock.
func (h *fakeHost) shortHead() string {
	if len(h.gitHead) > 7 {
		return h.gitHead[:7]
	}
	return h.gitHead
}

// masterTip is where the local master branch points. The caller holds the host
// lock.
func (h *fakeHost) masterTip() string {
	if h.gitMaster != "" {
		return h.gitMaster
	}
	return h.gitHead
}

// formatGitCommit renders one commit in the NUL-separated format the repo
// queries ask git for.
func formatGitCommit(commit fakeGitCommit) string {
	return fmt.Sprintf("%s%c%s%c%s", commit.Full, 0, commit.Short, 0, commit.Subject)
}

// lastArgument is the argument a query's subject sits in: the revision, the
// selector or the commit, depending on the command.
func lastArgument(cmd run.Command) string {
	if len(cmd.Args) == 0 {
		return ""
	}
	return cmd.Args[len(cmd.Args)-1]
}

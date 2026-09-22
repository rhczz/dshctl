package repo

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/rhczz/dshctl/internal/run"
)

// The deployment contract names one remote and one branch, and which ones is the
// caller's decision: this package knows how to read and move a checkout, not
// that a particular product follows origin/master. The service layer states its
// choice (see internal/service/checkout.go); `latest` is that ref and not
// "whatever the checked-out branch happens to track", because a checkout on a
// topic branch would otherwise deploy that branch's upstream while the operator
// believes they are following the harness releases.
//
// The zero value means the usual convention: origin and master.
func (r Repo) remote() string {
	if r.Remote == "" {
		return "origin"
	}
	return r.Remote
}

func (r Repo) branch() string {
	if r.Branch == "" {
		return "master"
	}
	return r.Branch
}

// r.RemoteTipName() is the ref `latest` means for this checkout.
func (r Repo) RemoteTipName() string { return r.remote() + "/" + r.branch() }

// Commit is one entry of the first-parent history.
type Commit struct {
	// Full is the complete revision.
	Full string
	// Short is git's abbreviated form.
	Short string
	// Subject is the first line of the commit message.
	Subject string
}

// gitCommand builds a git invocation inside the checkout.
func (r Repo) gitCommand(args ...string) run.Command {
	return run.Command{Name: "git", Args: append([]string{"-C", r.Dir}, args...), Dir: r.Dir}
}

// stream runs a git command with the caller's streams attached.
//
// Queries capture their output; the commands that move the tree write it
// through, because update mirrors them to the console and the log and a failed
// switch has to be explainable afterwards.
func (r Repo) stream(ctx context.Context, out, errOut io.Writer, args ...string) error {
	cmd := r.gitCommand(args...)
	cmd.Stdout = out
	cmd.Stderr = errOut
	return r.Ex.Run(ctx, cmd)
}

// HasOrigin reports whether the checkout has an origin remote.
func (r Repo) HasOrigin(ctx context.Context) (bool, error) {
	out, err := r.output().Output(ctx, r.gitCommand("config", "--get", "remote.origin.url"))
	if err != nil {
		// A missing key exits 1; that is "no origin", not a failure to look.
		if run.IsExit(err, 1) {
			return false, nil
		}
		return false, fmt.Errorf("the checkout's remotes could not be read: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

// Fetch updates the remote-tracking references and tags from origin.
//
// It does not prune: deleting a remote-tracking ref the remote no longer has is
// the operator's cleanup, not a side effect of reading the version gap. The
// action stays "learn what the remote has".
func (r Repo) Fetch(ctx context.Context, out, errOut io.Writer) error {
	if err := r.stream(ctx, out, errOut, "fetch", r.remote(), "--tags"); err != nil {
		return fmt.Errorf("the remote could not be fetched: %w", err)
	}
	return nil
}

// HeadCommit reports the full revision HEAD points at.
func (r Repo) HeadCommit(ctx context.Context) (string, error) {
	out, err := r.output().Output(ctx, r.gitCommand("rev-parse", "HEAD"))
	if err != nil {
		return "", fmt.Errorf("the checkout revision could not be read: %w", err)
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("the checkout revision could not be read: git returned no commit")
	}
	return sha, nil
}

// HeadName reports the branch and the exact tag at HEAD.
//
// A detached head has no branch; a commit without a tag has no tag. Either is a
// valid position, so only a failure to ask is an error.
func (r Repo) HeadName(ctx context.Context) (string, string, error) {
	branch, err := r.output().Output(ctx, r.gitCommand("rev-parse", "--abbrev-ref", "HEAD"))
	if err != nil {
		return "", "", fmt.Errorf("the checkout branch could not be read: %w", err)
	}
	branch = strings.TrimSpace(branch)
	if branch == "HEAD" {
		branch = ""
	}
	tag, err := r.output().Output(ctx, r.gitCommand("describe", "--tags", "--exact-match", "HEAD"))
	if err != nil {
		return branch, "", nil
	}
	return branch, strings.TrimSpace(tag), nil
}

// RemoteTip reports the commit origin/master points at.
func (r Repo) RemoteTip(ctx context.Context) (string, error) {
	out, err := r.output().Output(ctx, r.gitCommand("rev-parse", "--verify", r.RemoteTipName()+"^{commit}"))
	if err != nil {
		return "", fmt.Errorf("the position of %s could not be determined: %w", r.RemoteTipName(), err)
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("the position of %s could not be determined: git returned no commit", r.RemoteTipName())
	}
	return sha, nil
}

// ResolveRevision resolves a version selector to the full commit it names.
//
// A tag is tried first, then the selector as a revision, and both are peeled to
// a commit: a selector that names a tree or a blob can never be deployed.
//
// A local branch name is refused. `dshctl update master` reads like "the latest
// master" but would deploy wherever the local branch happens to point — often
// behind origin/master — and the warning about targets outside origin/master
// would stay silent because the stale tip is still an ancestor. `latest` is the
// name for the remote's master; a specific commit is spelled as a tag or a sha.
//
// The selector is never read as an option. It is validated before it reaches
// git, because a value like "--help" would otherwise be answered by git's own
// option parser instead of by the version lookup — and any future git option
// spelled like a version would run.
func (r Repo) ResolveRevision(ctx context.Context, selector string) (string, error) {
	if strings.TrimSpace(selector) == "" {
		return "", fmt.Errorf("the version cannot be empty")
	}
	if strings.HasPrefix(selector, "-") {
		return "", fmt.Errorf("the version cannot start with -: %q", selector)
	}
	// HEAD is not a branch name even though git resolves it through one: it
	// names the commit the checkout is already at, which the caller's short
	// circuit turns into a no-op.
	if selector != "HEAD" {
		if name, err := r.output().Output(ctx, r.gitCommand("rev-parse", "--symbolic-full-name", selector)); err == nil {
			if strings.HasPrefix(strings.TrimSpace(name), "refs/heads/") {
				return "", fmt.Errorf("the version cannot name a local branch %q: use %s for the remote tip, or a tag/commit", selector, r.RemoteTipName())
			}
		}
	}
	var lastErr error
	for _, candidate := range []string{"refs/tags/" + selector, selector} {
		out, err := r.output().Output(ctx, r.gitCommand("rev-parse", "--verify", candidate+"^{commit}"))
		if err == nil {
			if sha := strings.TrimSpace(out); sha != "" {
				return sha, nil
			}
			continue
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("git returned no commit")
	}
	return "", fmt.Errorf("the version %q could not be resolved: %w", selector, lastErr)
}

// CountRange counts the commits reachable from to but not from.
func (r Repo) CountRange(ctx context.Context, from, to string) (int, error) {
	out, err := r.output().Output(ctx, r.gitCommand("rev-list", "--count", from+".."+to))
	if err != nil {
		return 0, fmt.Errorf("the commits in %s..%s could not be counted: %w", from, to, err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("the commits in %s..%s could not be counted: git answered %q", from, to, out)
	}
	return count, nil
}

// FirstParentLog lists the commits in from..to along the first-parent spine,
// newest first.
//
// The spine is what makes a version list readable: a merge commit stands for
// the branch it brought in, and the side branch's commits are not shown.
func (r Repo) FirstParentLog(ctx context.Context, from, to string) ([]Commit, error) {
	const format = "%H%x00%h%x00%s"
	out, err := r.output().Output(ctx, r.gitCommand("log", "--first-parent", "--format="+format, from+".."+to))
	if err != nil {
		return nil, fmt.Errorf("the commit list of %s..%s could not be read: %w", from, to, err)
	}
	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 3 {
			return nil, fmt.Errorf("git's commit list could not be parsed: %q", line)
		}
		commits = append(commits, Commit{Full: fields[0], Short: fields[1], Subject: fields[2]})
	}
	return commits, nil
}

// CommitInfo describes one revision: the full and short sha and the subject.
//
// It exists for the position a range excludes: the timeline shows the current
// commit as a row of the same shape as the commits ahead of it.
func (r Repo) CommitInfo(ctx context.Context, revision string) (Commit, error) {
	const format = "%H%x00%h%x00%s"
	out, err := r.output().Output(ctx, r.gitCommand("log", "-1", "--format="+format, revision))
	if err != nil {
		return Commit{}, fmt.Errorf("the information of %s could not be read: %w", revision, err)
	}
	fields := strings.Split(strings.TrimSpace(out), "\x00")
	if len(fields) != 3 {
		return Commit{}, fmt.Errorf("the information of %s could not be parsed: %q", revision, out)
	}
	return Commit{Full: fields[0], Short: fields[1], Subject: fields[2]}, nil
}

// Tags maps each commit to the tag names pointing at it.
//
// Both tag shapes are answered: a lightweight tag points at the commit
// directly, while an annotated tag points at a tag object and carries the
// commit in its peeled form. Tags that resolve to anything but a commit are
// left out, because they can never name a deployable version.
func (r Repo) Tags(ctx context.Context) (map[string][]string, error) {
	const format = "%(objectname)%00%(*objectname)%00%(objecttype)%00%(*objecttype)%00%(refname:short)"
	out, err := r.output().Output(ctx, r.gitCommand("for-each-ref", "--format="+format, "refs/tags"))
	if err != nil {
		return nil, fmt.Errorf("the checkout's tags could not be read: %w", err)
	}
	tags := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 5 {
			return nil, fmt.Errorf("git's tag list could not be parsed: %q", line)
		}
		object, peeled, objectType, peeledType, name := fields[0], fields[1], fields[2], fields[3], fields[4]
		var commit string
		switch {
		case objectType == "commit":
			commit = object
		case peeledType == "commit":
			commit = peeled
		default:
			continue
		}
		tags[commit] = append(tags[commit], name)
	}
	return tags, nil
}

// IsAncestor reports whether ancestor is reachable from descendant.
func (r Repo) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	err := r.Ex.Run(ctx, r.gitCommand("merge-base", "--is-ancestor", ancestor, descendant))
	switch {
	case err == nil:
		return true, nil
	case run.IsExit(err, 1):
		// Exit 1 is git's answer "no"; anything else is a failure to look.
		return false, nil
	default:
		return false, fmt.Errorf("whether %s is an ancestor of %s could not be decided: %w", ancestor, descendant, err)
	}
}

// CheckoutDetach moves the worktree to commit without moving any branch.
func (r Repo) CheckoutDetach(ctx context.Context, commit string, out, errOut io.Writer) error {
	if err := r.stream(ctx, out, errOut, "checkout", "--detach", commit); err != nil {
		return fmt.Errorf("could not switch to %s: %w", commit, err)
	}
	return nil
}

// FastForwardMaster returns to the master branch and advances it to
// origin/master. A local commit makes the merge refuse; nothing is discarded.
func (r Repo) FastForwardMaster(ctx context.Context, out, errOut io.Writer) error {
	if err := r.stream(ctx, out, errOut, "checkout", r.branch()); err != nil {
		return fmt.Errorf("could not switch to the %s branch: %w", r.branch(), err)
	}
	if err := r.stream(ctx, out, errOut, "merge", "--ff-only", r.RemoteTipName()); err != nil {
		return fmt.Errorf("could not fast-forward to %s: %w", r.RemoteTipName(), err)
	}
	return nil
}

// TrackedChanges reports whether tracked files have uncommitted changes.
//
// Untracked files are ignored on purpose: an operator's own plugin directory is
// not a reason to refuse a version switch, and git still protects such a file
// when the target commit introduces the same path.
func (r Repo) TrackedChanges(ctx context.Context) (bool, error) {
	out, err := r.output().Output(ctx, r.gitCommand("status", "--porcelain", "--untracked-files=no"))
	if err != nil {
		return false, fmt.Errorf("the checkout's state could not be read: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

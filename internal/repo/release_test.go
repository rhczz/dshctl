package repo

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// releaseBox is a checkout with a real remote and a second clone that plays the
// upstream: commits and tags land in the peer first, so the checkout only sees
// them after a fetch. That is the shape every version decision is about.
type releaseBox struct {
	*checkout
	remote string
	peer   string
}

// newReleaseBox builds the three repositories: the managed checkout, a bare
// remote, and a peer clone used to add commits and tags upstream.
//
// The checkout's branch is forced to master rather than left to the machine's
// init.defaultBranch: the tool's contract names origin/master, and a fixture
// whose branch depends on the developer's git configuration would test a
// different contract on a different laptop.
func newReleaseBox(t *testing.T) *releaseBox {
	t.Helper()
	box := newCheckout(t)
	box.git("symbolic-ref", "HEAD", "refs/heads/master")
	box.commit()

	root := filepath.Dir(box.dir)
	remote := filepath.Join(root, "remote.git")
	peer := filepath.Join(root, "peer")
	runFixtureGit(t, root, "init", "--bare", "-q", remote)
	box.git("remote", "add", "origin", remote)
	box.git("push", "-q", "-u", "origin", "master")

	runFixtureGit(t, root, "clone", "-q", remote, peer)
	configureFixtureUser(t, peer)
	return &releaseBox{checkout: box, remote: remote, peer: peer}
}

// configureFixtureUser gives a repository a committer identity.
func configureFixtureUser(t *testing.T, dir string) {
	t.Helper()
	runFixtureGit(t, dir, "config", "user.email", "fixture@example.com")
	runFixtureGit(t, dir, "config", "user.name", "fixture")
	runFixtureGit(t, dir, "config", "commit.gpgsign", "false")
}

// peerGit runs a git command in the peer clone.
func (b *releaseBox) peerGit(args ...string) string {
	b.t.Helper()
	return runFixtureGit(b.t, b.peer, args...)
}

// peerCommit adds a tracked file and commits it in the peer.
func (b *releaseBox) peerCommit(relative, content, message string) {
	b.t.Helper()
	path := filepath.Join(b.peer, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.t.Fatalf("mkdir for %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.t.Fatalf("write %s: %v", relative, err)
	}
	b.peerGit("add", "-A")
	b.peerGit("commit", "-qm", message)
}

// peerPush publishes the peer's master and tags.
func (b *releaseBox) peerPush() {
	b.t.Helper()
	b.peerGit("push", "-q", "origin", "master", "--tags")
}

// revParse resolves a revision inside the managed checkout.
func (b *releaseBox) revParse(revision string) string {
	b.t.Helper()
	return strings.TrimSpace(b.git("rev-parse", revision))
}

// runFixtureGit runs a git command with the fixture's sanitised environment.
func runFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "core.hooksPath="}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = fixtureGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed (%v): %s", args, err, out)
	}
	return string(out)
}

// TestHasOriginReportsAMissingRemote pins the preflight that turns "this
// checkout has no origin" into a clear answer instead of a fetch failure.
func TestHasOriginReportsAMissingRemote(t *testing.T) {
	ctx := context.Background()
	plain := newCheckout(t)
	plain.commit()
	has, err := plain.repo.HasOrigin(ctx)
	if err != nil {
		t.Fatalf("HasOrigin: %v", err)
	}
	if has {
		t.Fatal("a checkout without a remote reported an origin")
	}

	box := newReleaseBox(t)
	has, err = box.repo.HasOrigin(ctx)
	if err != nil {
		t.Fatalf("HasOrigin: %v", err)
	}
	if !has {
		t.Fatal("a checkout with an origin reported none")
	}
}

// TestFetchBringsNewCommitsAndTags pins that fetch is what makes upstream
// visible: before it, origin/master and the new tag are unknown.
func TestFetchBringsNewCommitsAndTags(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	before := box.revParse("origin/master")

	box.peerCommit("next.txt", "next", "the next commit")
	box.peerTag("dsh-v0.1.0")
	box.peerPush()

	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if after := box.revParse("origin/master"); after == before {
		t.Fatal("origin/master did not move after the fetch")
	}
	if got := box.revParse("refs/tags/dsh-v0.1.0"); got == "" {
		t.Fatal("the tag did not arrive with the fetch")
	}
}

// TestRemoteTipFollowsOriginMaster pins the one name the contract uses for
// "latest".
func TestRemoteTipFollowsOriginMaster(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.peerCommit("next.txt", "next", "the next commit")
	box.peerPush()
	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	tip, err := box.repo.RemoteTip(ctx)
	if err != nil {
		t.Fatalf("RemoteTip: %v", err)
	}
	if want := box.revParse("origin/master"); tip != want {
		t.Fatalf("RemoteTip = %q, want %q", tip, want)
	}
}

// TestHeadNameReportsTagBranchAndDetached pins the three names a version can
// have, because the view prints them and rollback records them.
func TestHeadNameReportsTagBranchAndDetached(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)

	branch, tag, err := box.repo.HeadName(ctx)
	if err != nil {
		t.Fatalf("HeadName: %v", err)
	}
	if branch != "master" || tag != "" {
		t.Fatalf("HeadName = (%q, %q), want (master, )", branch, tag)
	}

	head := box.revParse("HEAD")
	box.git("tag", "dsh-v0.1.0")
	box.git("checkout", "-q", "--detach", head)

	branch, tag, err = box.repo.HeadName(ctx)
	if err != nil {
		t.Fatalf("HeadName: %v", err)
	}
	if branch != "" || tag != "dsh-v0.1.0" {
		t.Fatalf("HeadName = (%q, %q), want (, dsh-v0.1.0)", branch, tag)
	}
}

// TestResolveRevisionAcceptsTagShortAndFull pins the three selector shapes the
// command line documents.
func TestResolveRevisionAcceptsTagShortAndFull(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.git("tag", "dsh-v0.1.0")
	head := box.revParse("HEAD")

	cases := []struct {
		name     string
		selector string
	}{
		{"full", head},
		{"short", head[:8]},
		{"tag", "dsh-v0.1.0"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := box.repo.ResolveRevision(ctx, testCase.selector)
			if err != nil {
				t.Fatalf("ResolveRevision(%q): %v", testCase.selector, err)
			}
			if got != head {
				t.Fatalf("ResolveRevision(%q) = %q, want %q", testCase.selector, got, head)
			}
		})
	}
}

// TestResolveRevisionNamesAnUnknownSelector pins that a typo is reported with
// the selector in it, because "git failed" alone leaves the operator guessing
// which argument was wrong.
func TestResolveRevisionNamesAnUnknownSelector(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	_, err := box.repo.ResolveRevision(ctx, "no-such-version-here")
	if err == nil {
		t.Fatal("an unknown selector resolved")
	}
	if !strings.Contains(err.Error(), "no-such-version-here") {
		t.Fatalf("error = %v, want it to name the selector", err)
	}
}

// TestCountRangeCountsBothDirections pins the gap arithmetic the timeline
// prints.
func TestCountRangeCountsBothDirections(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.peerCommit("one.txt", "one", "one")
	box.peerCommit("two.txt", "two", "two")
	box.peerPush()
	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	head, err := box.repo.HeadCommit(ctx)
	if err != nil {
		t.Fatalf("HeadCommit: %v", err)
	}
	tip, err := box.repo.RemoteTip(ctx)
	if err != nil {
		t.Fatalf("RemoteTip: %v", err)
	}
	behind, err := box.repo.CountRange(ctx, head, tip)
	if err != nil {
		t.Fatalf("CountRange: %v", err)
	}
	if behind != 2 {
		t.Fatalf("behind = %d, want 2", behind)
	}
	ahead, err := box.repo.CountRange(ctx, tip, head)
	if err != nil {
		t.Fatalf("CountRange: %v", err)
	}
	if ahead != 0 {
		t.Fatalf("ahead = %d, want 0", ahead)
	}
}

// TestFirstParentLogSkipsSideBranchCommits pins the shape of the version list:
// the first-parent spine, so a merge reads as one entry rather than its whole
// side branch.
func TestFirstParentLogSkipsSideBranchCommits(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.peerCommit("base.txt", "base", "the base commit")
	box.peerGit("checkout", "-q", "-b", "side")
	box.peerCommit("side.txt", "side", "the side commit")
	box.peerGit("checkout", "-q", "master")
	box.peerGit("merge", "-q", "--no-ff", "-m", "the merge commit", "side")
	box.peerPush()
	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	head, err := box.repo.HeadCommit(ctx)
	if err != nil {
		t.Fatalf("HeadCommit: %v", err)
	}
	tip, err := box.repo.RemoteTip(ctx)
	if err != nil {
		t.Fatalf("RemoteTip: %v", err)
	}
	commits, err := box.repo.FirstParentLog(ctx, head, tip)
	if err != nil {
		t.Fatalf("FirstParentLog: %v", err)
	}
	subjects := make([]string, 0, len(commits))
	for _, commit := range commits {
		subjects = append(subjects, commit.Subject)
	}
	joined := strings.Join(subjects, "\n")
	if !strings.Contains(joined, "the merge commit") {
		t.Fatalf("subjects = %q, want the merge commit", joined)
	}
	if strings.Contains(joined, "the side commit") {
		t.Fatalf("subjects = %q, want the side branch left out", joined)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2 (base and merge)", len(commits))
	}
	if commits[0].Subject != "the merge commit" {
		t.Fatalf("first row = %q, want the newest commit first", commits[0].Subject)
	}
	if commits[0].Short == "" || commits[0].Full == "" || commits[0].Full == commits[0].Short {
		t.Fatalf("commit = %+v, want full and short shas", commits[0])
	}
}

// TestTagsMapAnnotatedAndLightweightTags pins both tag shapes: an annotated tag
// points at a tag object, and the map has to answer with the commit either way.
func TestTagsMapAnnotatedAndLightweightTags(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	head := box.revParse("HEAD")
	box.git("tag", "-a", "dsh-v0.1.0-ann", "-m", "annotated")
	box.git("tag", "dsh-v0.1.0-light")

	tags, err := box.repo.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	got := tags[head]
	if len(got) != 2 {
		t.Fatalf("tags at head = %v, want two", got)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"dsh-v0.1.0-ann", "dsh-v0.1.0-light"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tags = %v, want %s", got, want)
		}
	}
}

// TestIsAncestorAnswersBothWays pins the question the update warning asks.
func TestIsAncestorAnswersBothWays(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.peerCommit("next.txt", "next", "the next commit")
	box.peerPush()
	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	head, _ := box.repo.HeadCommit(ctx)
	tip, _ := box.repo.RemoteTip(ctx)

	ok, err := box.repo.IsAncestor(ctx, head, tip)
	if err != nil {
		t.Fatalf("IsAncestor: %v", err)
	}
	if !ok {
		t.Fatal("the old head is an ancestor of the remote tip")
	}
	ok, err = box.repo.IsAncestor(ctx, tip, head)
	if err != nil {
		t.Fatalf("IsAncestor: %v", err)
	}
	if ok {
		t.Fatal("the remote tip is not an ancestor of the old head")
	}
}

// TestCheckoutDetachLeavesMasterWhereItWas pins the promise that a named
// version never rewrites the operator's branch.
func TestCheckoutDetachLeavesMasterWhereItWas(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.write("second.txt", "second")
	box.commit()
	older := box.revParse("HEAD~1")
	master := box.revParse("master")

	if err := box.repo.CheckoutDetach(ctx, older, io.Discard, io.Discard); err != nil {
		t.Fatalf("CheckoutDetach: %v", err)
	}
	if head := box.revParse("HEAD"); head != older {
		t.Fatalf("HEAD = %q, want %q", head, older)
	}
	if after := box.revParse("master"); after != master {
		t.Fatalf("master = %q, want it untouched at %q", after, master)
	}
	branch, tag, err := box.repo.HeadName(ctx)
	if err != nil {
		t.Fatalf("HeadName: %v", err)
	}
	if branch != "" || tag != "" {
		t.Fatalf("HeadName = (%q, %q), want a detached head", branch, tag)
	}
}

// TestFastForwardMasterMovesHeadToTheRemoteTip pins the latest path: the
// checkout returns to the branch and advances to origin/master.
func TestFastForwardMasterMovesHeadToTheRemoteTip(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.peerCommit("next.txt", "next", "the next commit")
	box.peerPush()
	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	tip, _ := box.repo.RemoteTip(ctx)

	if err := box.repo.FastForwardMaster(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("FastForwardMaster: %v", err)
	}
	if head := box.revParse("HEAD"); head != tip {
		t.Fatalf("HEAD = %q, want the remote tip %q", head, tip)
	}
	branch, _, err := box.repo.HeadName(ctx)
	if err != nil {
		t.Fatalf("HeadName: %v", err)
	}
	if branch != "master" {
		t.Fatalf("branch = %q, want master", branch)
	}
}

// TestFastForwardMasterRefusesADivergedBranch pins that a local commit is never
// discarded: the merge refuses, and the tree stays where it was.
func TestFastForwardMasterRefusesADivergedBranch(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.peerCommit("next.txt", "next", "the next commit")
	box.peerPush()
	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	box.write("local.txt", "local")
	box.commit()
	local := box.revParse("HEAD")

	err := box.repo.FastForwardMaster(ctx, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("a diverged branch fast-forwarded")
	}
	if head := box.revParse("HEAD"); head != local {
		t.Fatalf("HEAD = %q, want the local commit %q", head, local)
	}
}

// TestTrackedChangesIgnoresUntrackedFiles pins the boundary of the dirty check:
// an untracked plugin directory is the operator's own work and must not block a
// version switch.
func TestTrackedChangesIgnoresUntrackedFiles(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)

	box.write("untracked/plugin.txt", "mine")
	dirty, err := box.repo.TrackedChanges(ctx)
	if err != nil {
		t.Fatalf("TrackedChanges: %v", err)
	}
	if dirty {
		t.Fatal("an untracked file counted as a tracked change")
	}

	box.write("package.json", `{"changed": true}`)
	dirty, err = box.repo.TrackedChanges(ctx)
	if err != nil {
		t.Fatalf("TrackedChanges: %v", err)
	}
	if !dirty {
		t.Fatal("a modified tracked file did not count")
	}
}

// peerTag creates a lightweight tag in the peer.
func (b *releaseBox) peerTag(name string) {
	b.t.Helper()
	b.peerGit("tag", name)
}

// TestResolveRevisionRefusesAnEmptySelector pins the cheapest rejection: an
// empty selector is a caller bug, and handing it to git would answer with a
// message about git rather than about the version that was asked for.
func TestResolveRevisionRefusesAnEmptySelector(t *testing.T) {
	box := newReleaseBox(t)
	for _, selector := range []string{"", " ", "\t", "\n"} {
		_, err := box.repo.ResolveRevision(context.Background(), selector)
		if err == nil {
			t.Fatalf("ResolveRevision(%q) resolved", selector)
		}
	}
}

// TestResolveRevisionRefusesFlagLikeSelectors pins that a selector is never
// read as an option. `git rev-parse --help` prints usage and exits 129, which
// would make a typo look like a git failure instead of a version that does not
// exist — and worse, any future git option named like a version would run.
func TestResolveRevisionRefusesFlagLikeSelectors(t *testing.T) {
	box := newReleaseBox(t)
	for _, selector := range []string{"--help", "-h", "--verify", "-C"} {
		_, err := box.repo.ResolveRevision(context.Background(), selector)
		if err == nil {
			t.Fatalf("ResolveRevision(%q) resolved", selector)
		}
		if !strings.Contains(err.Error(), selector) {
			t.Fatalf("error = %v, want it to name %q", err, selector)
		}
	}
}

// TestResolveRevisionRefusesJunkSelectors pins that anything that is not a
// commit-ish is refused: a tree, a blob, a path, a broken revision expression.
func TestResolveRevisionRefusesJunkSelectors(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	blob := strings.TrimSpace(box.git("hash-object", "-w", filepath.Join(box.dir, "package.json")))
	box.git("tag", "blob-tag", blob)

	for _, selector := range []string{"HEAD^{tree}", "blob-tag", "refs/tags/", "..", "no/such/thing"} {
		t.Run(selector, func(t *testing.T) {
			if _, err := box.repo.ResolveRevision(ctx, selector); err == nil {
				t.Fatalf("ResolveRevision(%q) resolved", selector)
			}
		})
	}
}

// TestResolveRevisionRefusesAnAmbiguousShortHash pins the one selector shape
// that silently means the wrong commit if nobody checks: a short sha that
// matches more than one object must fail, never pick one.
func TestResolveRevisionRefusesAnAmbiguousShortHash(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	tree := strings.TrimSpace(box.git("write-tree"))
	head := box.revParse("HEAD")

	seen := map[string]string{}
	ambiguous := ""
	for index := 0; index < 2000 && ambiguous == ""; index++ {
		sha := strings.TrimSpace(box.git("commit-tree", tree, "-p", head, "-m", fmt.Sprintf("probe %d", index)))
		prefix := sha[:2]
		if other, ok := seen[prefix]; ok && other != sha {
			ambiguous = prefix
			break
		}
		seen[prefix] = sha
	}
	if ambiguous == "" {
		t.Fatal("could not construct an ambiguous prefix")
	}
	_, err := box.repo.ResolveRevision(ctx, ambiguous)
	if err == nil {
		t.Fatalf("ambiguous prefix %q resolved", ambiguous)
	}
}

// TestFetchReportsAnUnreachableOrigin pins that a network failure is an error
// carrying git's reason, because the timeline has to show it and update has to
// refuse rather than continue on a stale view.
func TestFetchReportsAnUnreachableOrigin(t *testing.T) {
	box := newReleaseBox(t)
	box.git("remote", "set-url", "origin", filepath.Join(filepath.Dir(box.dir), "missing.git"))
	err := box.repo.Fetch(context.Background(), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("fetching a missing remote succeeded")
	}
}

// TestFetchReportsAMissingOrigin pins the preflight distinction: no origin is
// the operator's setup, not a transient network problem.
func TestFetchReportsAMissingOrigin(t *testing.T) {
	box := newCheckout(t)
	box.commit()
	err := box.repo.Fetch(context.Background(), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("fetching without a remote succeeded")
	}
}

// TestFetchStreamsGitOutput pins that the caller's writers receive git's own
// words: update mirrors them to the console and the log, and a silently
// captured fetch would make a failed update unexplainable.
func TestFetchStreamsGitOutput(t *testing.T) {
	box := newReleaseBox(t)
	box.peerCommit("next.txt", "next", "the next commit")
	box.peerPush()
	var out, errOut strings.Builder
	if err := box.repo.Fetch(context.Background(), &out, &errOut); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if out.Len() == 0 && errOut.Len() == 0 {
		t.Fatal("git wrote nothing to the streams it was given")
	}
}

// TestRemoteTipReportsAMissingOriginMaster pins that "latest" has no answer
// when the remote branch was never fetched, instead of inventing one.
func TestRemoteTipReportsAMissingOriginMaster(t *testing.T) {
	box := newReleaseBox(t)
	box.git("update-ref", "-d", "refs/remotes/origin/master")
	if _, err := box.repo.RemoteTip(context.Background()); err == nil {
		t.Fatal("RemoteTip answered without origin/master")
	}
}

// TestFirstParentLogOnAnEmptyRange pins the up-to-date case: no commits, no
// error.
func TestFirstParentLogOnAnEmptyRange(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	head, _ := box.repo.HeadCommit(ctx)
	commits, err := box.repo.FirstParentLog(ctx, head, head)
	if err != nil {
		t.Fatalf("FirstParentLog: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("commits = %+v, want none", commits)
	}
}

// TestFirstParentLogOnADivergedRange pins that a local commit is not part of
// "what the update would bring": the range is from the current head to the
// remote tip.
func TestFirstParentLogOnADivergedRange(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.peerCommit("upstream.txt", "upstream", "the upstream commit")
	box.peerPush()
	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	box.write("local.txt", "local")
	box.commit()

	head, _ := box.repo.HeadCommit(ctx)
	tip, _ := box.repo.RemoteTip(ctx)
	commits, err := box.repo.FirstParentLog(ctx, head, tip)
	if err != nil {
		t.Fatalf("FirstParentLog: %v", err)
	}
	subjects := ""
	for _, commit := range commits {
		subjects += commit.Subject + "\n"
	}
	if !strings.Contains(subjects, "the upstream commit") {
		t.Fatalf("subjects = %q, want the upstream commit", subjects)
	}
	if strings.Contains(subjects, "fixture") {
		t.Fatalf("subjects = %q, want the local commit left out", subjects)
	}
}

// TestFirstParentLogNamesAnInvalidRevision pins that a broken range is an error
// rather than an empty list, which a caller could mistake for "up to date".
func TestFirstParentLogNamesAnInvalidRevision(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	head, _ := box.repo.HeadCommit(ctx)
	if _, err := box.repo.FirstParentLog(ctx, head, "no-such-revision"); err == nil {
		t.Fatal("a log over an unknown revision succeeded")
	}
	if _, err := box.repo.CountRange(ctx, head, "no-such-revision"); err == nil {
		t.Fatal("a count over an unknown revision succeeded")
	}
}

// TestTagsSkipTagsThatDoNotPointAtCommits pins that the tag map is about
// versions: a tag on a blob or a tree can never name a deployable position and
// must not appear.
func TestTagsSkipTagsThatDoNotPointAtCommits(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	head := box.revParse("HEAD")
	blob := strings.TrimSpace(box.git("hash-object", "-w", filepath.Join(box.dir, "package.json")))
	tree := strings.TrimSpace(box.git("write-tree"))
	box.git("tag", "blob-light", blob)
	box.git("tag", "-a", "blob-annotated", "-m", "msg", blob)
	box.git("tag", "tree-light", tree)
	box.git("tag", "good", head)

	tags, err := box.repo.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	for sha, names := range tags {
		if sha != head {
			t.Fatalf("tag map has a non-commit key %s: %v", sha, names)
		}
	}
	if got := strings.Join(tags[head], " "); got != "good" {
		t.Fatalf("tags at head = %q, want only good", got)
	}
}

// TestTagsOnARepositoryWithoutTags pins the empty answer, so the timeline can
// print "no tag in the gap" without a special case.
func TestTagsOnARepositoryWithoutTags(t *testing.T) {
	box := newCheckout(t)
	box.commit()
	tags, err := box.repo.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("tags = %v, want none", tags)
	}
}

// TestIsAncestorReportsUnknownObjects pins that a failed query is an error, not
// a "no": the warning about a target outside origin/master must never be
// printed because git could not look.
func TestIsAncestorReportsUnknownObjects(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	head, _ := box.repo.HeadCommit(ctx)
	if _, err := box.repo.IsAncestor(ctx, strings.Repeat("0", 40), head); err == nil {
		t.Fatal("IsAncestor answered about an unknown object")
	}
}

// TestCheckoutDetachRefusesANonCommit pins that the switch accepts only a
// commit, even though the selector was already resolved: the guard is repeated
// at the boundary that changes the tree.
func TestCheckoutDetachRefusesANonCommit(t *testing.T) {
	box := newReleaseBox(t)
	tree := strings.TrimSpace(box.git("write-tree"))
	head := box.revParse("HEAD")
	if err := box.repo.CheckoutDetach(context.Background(), tree, io.Discard, io.Discard); err == nil {
		t.Fatal("detaching at a tree succeeded")
	}
	if got := box.revParse("HEAD"); got != head {
		t.Fatalf("HEAD moved to %q, want it at %q", got, head)
	}
}

// TestCheckoutDetachRefusesToOverwriteTrackedChanges pins that the operator's
// uncommitted work survives even when dshctl's own dirty check is bypassed: git
// is the last line of defense, and the head must not move.
func TestCheckoutDetachRefusesToOverwriteTrackedChanges(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.peerCommit("package.json", `{"upstream": true}`, "the upstream change")
	box.peerPush()
	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	box.write("package.json", `{"mine": true}`)
	before := box.revParse("HEAD")
	tip, _ := box.repo.RemoteTip(ctx)

	if err := box.repo.CheckoutDetach(ctx, tip, io.Discard, io.Discard); err == nil {
		t.Fatal("detaching over a modified tracked file succeeded")
	}
	if got := box.revParse("HEAD"); got != before {
		t.Fatalf("HEAD moved to %q, want it at %q", got, before)
	}
	data, err := os.ReadFile(filepath.Join(box.dir, "package.json"))
	if err != nil || string(data) != `{"mine": true}` {
		t.Fatalf("the local change = %q (err=%v), want it intact", data, err)
	}
}

// TestCheckoutDetachRefusesToOverwriteUntrackedFiles pins the untracked
// boundary: dshctl's dirty check ignores untracked files on purpose, so git
// still has to refuse when an upstream commit introduces the same path.
func TestCheckoutDetachRefusesToOverwriteUntrackedFiles(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.peerCommit("plugin.txt", "upstream", "the upstream plugin")
	box.peerPush()
	if err := box.repo.Fetch(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	box.write("plugin.txt", "mine")
	before := box.revParse("HEAD")
	tip, _ := box.repo.RemoteTip(ctx)

	if err := box.repo.CheckoutDetach(ctx, tip, io.Discard, io.Discard); err == nil {
		t.Fatal("detaching over an untracked file succeeded")
	}
	if got := box.revParse("HEAD"); got != before {
		t.Fatalf("HEAD moved to %q, want it at %q", got, before)
	}
	data, _ := os.ReadFile(filepath.Join(box.dir, "plugin.txt"))
	if string(data) != "mine" {
		t.Fatalf("the untracked file = %q, want it intact", data)
	}
}

// TestCheckoutDetachStreamsGitOutput pins the writer wiring for the switch, so
// the log explains what moved.
func TestCheckoutDetachStreamsGitOutput(t *testing.T) {
	box := newReleaseBox(t)
	head := box.revParse("HEAD")
	var out, errOut strings.Builder
	if err := box.repo.CheckoutDetach(context.Background(), head, &out, &errOut); err != nil {
		t.Fatalf("CheckoutDetach: %v", err)
	}
	if out.Len() == 0 && errOut.Len() == 0 {
		t.Fatal("git wrote nothing to the streams it was given")
	}
}

// TestFastForwardMasterReportsAMissingBranch pins the preflight the message has
// to carry: a checkout whose branch is not master cannot be fast-forwarded, and
// "pathspec did not match" alone would not say what was expected. The
// remote-tracking ref is removed too, or git would recreate the branch from it.
func TestFastForwardMasterReportsAMissingBranch(t *testing.T) {
	box := newReleaseBox(t)
	box.git("branch", "-m", "master", "trunk")
	box.git("update-ref", "-d", "refs/remotes/origin/master")
	err := box.repo.FastForwardMaster(context.Background(), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("a fast-forward without a master branch succeeded")
	}
	if !strings.Contains(err.Error(), "master") {
		t.Fatalf("error = %v, want it to name master", err)
	}
}

// TestTrackedChangesCountsStagedAndDeletedChanges pins every shape of "the
// operator touched a tracked file": unstaged, staged, and deleted.
func TestTrackedChangesCountsStagedAndDeletedChanges(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)

	box.write("package.json", `{"staged": true}`)
	box.git("add", "package.json")
	dirty, err := box.repo.TrackedChanges(ctx)
	if err != nil {
		t.Fatalf("TrackedChanges: %v", err)
	}
	if !dirty {
		t.Fatal("a staged change did not count")
	}

	box.git("reset", "-q", "--hard")
	if err := os.Remove(filepath.Join(box.dir, "package.json")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	dirty, err = box.repo.TrackedChanges(ctx)
	if err != nil {
		t.Fatalf("TrackedChanges: %v", err)
	}
	if !dirty {
		t.Fatal("a deleted tracked file did not count")
	}
}

// TestHeadCommitReportsAnUnbornRepository pins that a checkout without commits
// is an error at the boundary, not an empty sha that later commands would pass
// to git.
func TestHeadCommitReportsAnUnbornRepository(t *testing.T) {
	box := newCheckout(t)
	if _, err := box.repo.HeadCommit(context.Background()); err == nil {
		t.Fatal("HeadCommit answered for an unborn branch")
	}
}

// TestHeadNameReportsAnUnbornRepository pins the same boundary for the name.
func TestHeadNameReportsAnUnbornRepository(t *testing.T) {
	box := newCheckout(t)
	if _, _, err := box.repo.HeadName(context.Background()); err == nil {
		t.Fatal("HeadName answered for an unborn branch")
	}
}

// TestHeadNameReportsATagThatIsNotAnExactMatch pins that a nearby tag is not
// the current version: only a tag pointing at HEAD names it.
func TestHeadNameReportsATagThatIsNotAnExactMatch(t *testing.T) {
	ctx := context.Background()
	box := newReleaseBox(t)
	box.git("tag", "dsh-v0.1.0")
	box.write("second.txt", "second")
	box.commit()

	branch, tag, err := box.repo.HeadName(ctx)
	if err != nil {
		t.Fatalf("HeadName: %v", err)
	}
	if branch != "master" || tag != "" {
		t.Fatalf("HeadName = (%q, %q), want (master, )", branch, tag)
	}
}

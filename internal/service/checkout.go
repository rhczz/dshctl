package service

import (
	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/repo"
)

// This file is the product side of the checkout contract.
//
// The repo package knows how to read and move a git checkout; which remote and
// branch `latest` means, and which leftovers a deleted package can leave behind,
// are this product's values. They live here so another product can use the same
// mechanism with its own layout.

// historyMaxRecords is how much deployment history this product keeps: the
// oldest positions fall off so the file stays small enough to read during an
// incident, and an operator who needs something older can still name it
// explicitly with `dshctl update <sha>`.
const historyMaxRecords = 50

// missingCheckout is the refusal for a --repo that does not exist. The hint is
// the one sentence that gets an operator unstuck: name it once, and a
// successful run records it.
func missingCheckoutError(repoDir string) error {
	return exitcode.New(exitcode.Preflight, "the checkout does not exist: %s\nhint: name it with --repo or the %s environment variable", repoDir, paths.EnvRepoDir)
}

// notAGitRepository is the refusal for a directory that holds the manifests but
// no git repository — a copied tree, an exported tarball. Nothing here may run
// git inside it.
func notAGitRepositoryError(repoDir string) error {
	return exitcode.New(exitcode.Preflight, "%s is not a git repository", repoDir)
}

// notServerCheckout is the refusal for a tree that carries no checkout marker:
// a mistyped --repo must not be able to run anything inside a stranger's tree.
func notServerCheckoutError(repoDir string) error {
	return exitcode.New(exitcode.Preflight, "%s does not look like a DeepSeek Harness checkout (no %s or %s)\nhint: point --repo at the right checkout", repoDir, config.ServerManifestRel, config.WorkspaceManifestRel)
}

// requireCheckout runs the three checkout gates every tree-touching command
// shares: the directory exists, it is a git repository, and it is the managed
// checkout. Commands that never read git ask only the first and third.
func (s *Service) requireCheckout(withGit bool) error {
	if !s.Repo.Exists() {
		return missingCheckoutError(s.Settings.RepoDir)
	}
	if withGit && !s.Repo.IsGit() {
		return notAGitRepositoryError(s.Settings.RepoDir)
	}
	if !s.Repo.IsServerCheckout() {
		return notServerCheckoutError(s.Settings.RepoDir)
	}
	return nil
}

// checkoutLayout is how dshctl's checkout is shaped.
//
// The deployment contract is origin/master — not "whatever the checked-out
// branch happens to track" — and the residue classification matches the
// repository's own scripts/clean.ts: node_modules, lib and .typecheck, plus
// stray TypeScript incremental state, under packages/<group>/<name> and
// vendor/<name>.
func checkoutLayout(r repo.Repo) repo.Repo {
	r.Remote = "origin"
	r.Branch = "master"
	r.Residue = map[string]struct{}{
		"node_modules": {},
		"lib":          {},
		".typecheck":   {},
	}
	r.Areas = []repo.PruneArea{
		{Name: "packages", Depth: 2},
		{Name: "vendor", Depth: 1},
	}
	return r
}

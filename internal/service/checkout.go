package service

import "github.com/rhczz/dshctl/internal/repo"

// This file is the product side of the checkout contract.
//
// The repo package knows how to read and move a git checkout; which remote and
// branch `latest` means, and which leftovers a deleted package can leave behind,
// are this product's values. They live here so another product can use the same
// mechanism with its own layout.

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

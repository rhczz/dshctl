// Package repo inspects and maintains the deepseek-harness checkout.
package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rhczz/dshctl/internal/run"
)

// Repo is the deepseek-harness checkout dshctl manages.
type Repo struct {
	// Dir is the absolute checkout path.
	Dir string
	// Ex runs git and pnpm.
	Ex run.Executor
	// ManifestRel and WorkspaceManifestRel identify the checkout.
	ManifestRel          string
	WorkspaceManifestRel string
	// BuildRecordRel is the build marker, relative to Dir.
	BuildRecordRel string
}

// output collects a command's standard output.
func (r Repo) output() run.Outputer {
	return run.Collector(r.Ex)
}

// Exists reports whether the checkout directory exists.
func (r Repo) Exists() bool {
	info, err := os.Stat(r.Dir)
	return err == nil && info.IsDir()
}

// IsGit reports whether Dir holds a git worktree.
func (r Repo) IsGit() bool {
	info, err := os.Stat(filepath.Join(r.Dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

// IsServerCheckout reports whether Dir looks like a DeepSeek Harness checkout.
//
// The check exists so that a mistyped --repo can never point the build's cleanup
// at an unrelated git repository: pruning is destructive, and "it is a git
// repository" is not evidence that the tree belongs to this tool.
func (r Repo) IsServerCheckout() bool {
	if !r.Exists() {
		return false
	}
	if !fileExists(filepath.Join(r.Dir, r.ManifestRel)) {
		return false
	}
	return fileExists(filepath.Join(r.Dir, r.WorkspaceManifestRel))
}

// NodeModulesPresent reports whether dependencies are installed.
func (r Repo) NodeModulesPresent() bool {
	info, err := os.Stat(filepath.Join(r.Dir, "node_modules"))
	return err == nil && info.IsDir()
}

// BuildRecordPath is the marker pnpm run build writes last.
func (r Repo) BuildRecordPath() string {
	return filepath.Join(r.Dir, filepath.FromSlash(r.BuildRecordRel))
}

// BuildReady reports whether the tree is installed and built. The build record
// is authoritative because it is written at the end of a successful build.
func (r Repo) BuildReady() bool {
	if !r.NodeModulesPresent() {
		return false
	}
	return fileExists(r.BuildRecordPath())
}

// Head reports the checked-out revision and branch.
func (r Repo) Head(ctx context.Context) (string, string, error) {
	sha, err := r.output().Output(ctx, run.Command{Name: "git", Args: []string{"-C", r.Dir, "rev-parse", "--short", "HEAD"}})
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", i18nLine(MsgHeadFailed), err)
	}
	branch, err := r.output().Output(ctx, run.Command{Name: "git", Args: []string{"-C", r.Dir, "rev-parse", "--abbrev-ref", "HEAD"}})
	if err != nil {
		return sha, "HEAD", nil
	}
	return sha, branch, nil
}

// Dirty reports whether the worktree has uncommitted changes.
func (r Repo) Dirty(ctx context.Context) (bool, error) {
	out, err := r.output().Output(ctx, run.Command{Name: "git", Args: []string{"-C", r.Dir, "status", "--porcelain"}})
	if err != nil {
		return false, fmt.Errorf("%s: %w", i18nLine(MsgStatusFailed), err)
	}
	return len(out) > 0, nil
}

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

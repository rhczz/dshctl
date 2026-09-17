package repo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rhczz/dshctl/internal/run"
)

// residueEntries are the build leftovers a deleted package can leave behind.
// The classification matches the repository's own scripts/clean.ts:
// node_modules, lib and .typecheck, plus stray TypeScript incremental state.
var residueEntries = map[string]struct{}{
	"node_modules": {},
	"lib":          {},
	".typecheck":   {},
}

// pruneAreas describe where a package directory can live, and how deep the
// pattern has to reach to name one.
//
// depth counts the path segments used by the pattern itself, so the candidate a
// match names is the segment at that depth: "packages" with depth 2 matches
// packages/<group>/<name>, and "vendor" with depth 1 matches vendor/<name>.
// Residue is then looked for inside that candidate.
var pruneAreas = []struct {
	// name is the repository-relative area root.
	name string
	// depth is the number of pattern segments below the area root.
	depth int
}{
	{name: "packages", depth: 2},
	{name: "vendor", depth: 1},
}

// Candidate is one directory scheduled for removal.
type Candidate struct {
	// Path is the absolute directory.
	Path string
	// Entries are the residue entries found inside it.
	Entries []string
}

// PruneReport describes what a prune did.
type PruneReport struct {
	// Removed are the directories that were deleted.
	Removed []string
	// Failed are the directories that could not be deleted.
	Failed []string
}

// PruneCandidates lists package directories that git no longer tracks and that
// hold nothing but known build residue.
//
// Safety properties, each of which a test pins:
//
//   - A candidate must be a real directory strictly inside the repository after
//     resolving symlinks, so a symlinked packages/ or vendor/ can never lead the
//     removal outside the checkout.
//   - A candidate whose parent holds a package.json is a vendor archive or a
//     build product rather than residue, and is left alone.
//   - Tracking is decided by git's NUL-separated output, which emits paths raw,
//     so a path with non-ASCII characters is compared literally instead of as
//     its escaped form.
//   - The pattern walk uses io/fs on an opened root, so glob metacharacters in
//     the repository path cannot redirect the walk to another tree.
//
// Returns:
//   - the candidates, sorted by path.
//   - an error when the repository cannot be inspected.
func (r Repo) PruneCandidates(ctx context.Context) ([]Candidate, error) {
	// The root is resolved through symlinks so that the boundary check below
	// compares real paths, which is what makes a symlinked packages/ directory
	// detectable at all.
	root, err := resolveExistingPrefix(r.Dir)
	if err != nil {
		return nil, fmt.Errorf("无法解析仓库路径 %s: %w", r.Dir, err)
	}
	rootFS := os.DirFS(root)

	tracked, err := r.trackedDirectories(ctx)
	if err != nil {
		return nil, err
	}

	var candidates []Candidate
	for _, area := range pruneAreas {
		pattern := area.name + strings.Repeat("/*", area.depth)
		matches, err := fs.Glob(rootFS, pattern)
		if err != nil {
			return nil, fmt.Errorf("无法展开 %s: %w", pattern, err)
		}
		for _, relative := range matches {
			candidate, ok := r.classify(root, rootFS, relative, tracked)
			if ok {
				// Report the candidate under the path the operator configured,
				// so the message matches what they typed even when the checkout
				// is reached through a symlinked parent such as /var on macOS.
				candidate.Path = filepath.Join(r.Dir, filepath.FromSlash(relative))
				candidates = append(candidates, candidate)
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })
	return candidates, nil
}

// trackedDirectories returns every directory inside the prune roots that still
// holds a tracked file. One git invocation answers for the whole tree; asking
// per package would spawn a process per package on every build.
//
// The NUL-separated form is required, not cosmetic: without `-z` git quotes a
// path holding non-ASCII bytes, and the quoted name never matches the directory
// the walk produced. `core.quotePath` is deliberately not passed: it has no
// effect on `-z` output, so setting it would be configuration that decides
// nothing.
func (r Repo) trackedDirectories(ctx context.Context) (map[string]struct{}, error) {
	args := []string{"-C", r.Dir, "ls-files", "-z", "--"}
	for _, area := range pruneAreas {
		args = append(args, area.name)
	}
	output, err := r.output().Output(ctx, run.Command{Name: "git", Args: args})
	if err != nil {
		return nil, fmt.Errorf("git ls-files 失败: %w", err)
	}
	directories := make(map[string]struct{})
	for _, file := range strings.Split(output, "\x00") {
		if file == "" {
			continue
		}
		// git always reports slash-separated paths, so path.Dir is the right
		// splitter rather than filepath.Dir.
		for dir := path.Dir(file); dir != "." && dir != "/" && dir != ""; dir = path.Dir(dir) {
			directories[dir] = struct{}{}
		}
	}
	return directories, nil
}

// classify decides whether one relative directory is removable residue.
func (r Repo) classify(root string, rootFS fs.FS, relative string, tracked map[string]struct{}) (Candidate, bool) {
	absolute := filepath.Join(root, filepath.FromSlash(relative))

	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		// A symlink is never residue: its target may be anywhere.
		return Candidate{}, false
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil || !strictlyInside(root, resolved) {
		return Candidate{}, false
	}
	if _, ok := tracked[path.Clean(relative)]; ok {
		return Candidate{}, false
	}
	if hasSiblingManifest(rootFS, relative) {
		// The parent ships its own package.json, so this directory is part of
		// the project rather than leftovers from a deleted package.
		return Candidate{}, false
	}
	entries, err := fs.ReadDir(rootFS, relative)
	if err != nil || len(entries) == 0 {
		return Candidate{}, false
	}
	found := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if _, ok := residueEntries[name]; ok {
			found = append(found, name)
			continue
		}
		if strings.HasSuffix(name, ".tsbuildinfo") {
			found = append(found, name)
			continue
		}
		return Candidate{}, false
	}
	sort.Strings(found)
	return Candidate{Path: absolute, Entries: found}, true
}

// resolveExistingPrefix resolves the symlinks of the part of path that exists,
// leaving the rest appended verbatim.
//
// A plain filepath.EvalSymlinks fails when any element is missing, and this runs
// during diagnostics where a missing directory is reported separately. Resolving
// only the existing prefix also keeps the resolved root comparable with the
// candidates the walk produces.
func resolveExistingPrefix(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := absolute
	var missing []string
	for {
		if _, err := os.Lstat(current); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absolute, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", err
	}
	for index := len(missing) - 1; index >= 0; index-- {
		resolved = filepath.Join(resolved, missing[index])
	}
	return resolved, nil
}

// strictlyInside reports whether target is below root.
func strictlyInside(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// hasSiblingManifest reports whether the candidate's parent directory holds a
// package.json.
//
// The parent is the directory that *contains* the candidate: vendor/<name> and
// packages/<group>/<name> are reached at different depths and the same rule has
// to find the right one for both. Consulting the grandparent instead protected
// the wrong directory — a manifest at the area root packages/package.json would
// have shielded every package below it from ever being pruned, while a manifest
// in the group directory that actually holds the candidate was never seen.
func hasSiblingManifest(rootFS fs.FS, relative string) bool {
	parent := path.Dir(relative)
	if parent == "." || parent == "/" || parent == "" {
		return false
	}
	info, err := fs.Stat(rootFS, path.Join(parent, "package.json"))
	return err == nil && info.Mode().IsRegular()
}

// Prune removes the residue directories PruneCandidates reports.
//
// A directory that cannot be removed is reported and skipped rather than failing
// a build that would otherwise succeed; `pnpm run clean` stays the manual
// fallback.
//
// Parameters:
//   - ctx: cancellation stops the inspection; removal itself is not interruptible.
//   - report: receives one line per removed or failed directory; nil is allowed.
func (r Repo) Prune(ctx context.Context, report func(string)) (PruneReport, error) {
	candidates, err := r.PruneCandidates(ctx)
	if err != nil {
		return PruneReport{}, err
	}
	var result PruneReport
	for _, candidate := range candidates {
		if report != nil {
			report(fmt.Sprintf("清理残留目录: %s (仅含 %s)", candidate.Path, strings.Join(candidate.Entries, ", ")))
		}
		if err := os.RemoveAll(candidate.Path); err != nil {
			result.Failed = append(result.Failed, candidate.Path)
			if report != nil {
				report(fmt.Sprintf("警告: 无法删除 %s，构建将继续;可稍后手动执行 pnpm run clean", candidate.Path))
			}
			continue
		}
		result.Removed = append(result.Removed, candidate.Path)
	}
	return result, nil
}

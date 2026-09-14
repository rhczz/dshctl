// Package nodejs resolves the Node.js runtime dshctl launches the Web server
// with, preferring an nvm or fnm installation and falling back to PATH.
package nodejs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/run"
)

// Latest selects the newest installed release instead of a pinned version.
const Latest = "latest"

// Source labels reported by Installation.Source.
const (
	// SourceNVM is an nvm-managed release.
	SourceNVM = "nvm"
	// SourceFNM is an fnm-managed release.
	SourceFNM = "fnm"
	// SourcePath is whatever `node` resolves to on PATH.
	SourcePath = "path"
)

// Installation is the resolved Node runtime.
type Installation struct {
	// Version is the release without its leading v. It is empty for a PATH hit
	// until Info fills it in by running the binary.
	Version string
	// NodePath is the absolute path of the node binary.
	NodePath string
	// BinDir is the directory prepended to PATH so pnpm and node agree.
	BinDir string
	// Source names where the installation came from.
	Source string
	// Requested echoes the version that was asked for when it was not installed.
	Requested string
}

// Preferences describes what the operator asked for.
type Preferences struct {
	// Version is a release such as "24.20.0", "latest", or empty for the newest
	// installed release.
	Version string
	// Home is the operating-system home directory holding the version managers.
	Home string
}

// Resolver discovers installations. Every host lookup is injectable.
type Resolver struct {
	// LookPath resolves an executable on PATH; nil uses run.LookPath.
	LookPath func(string) (string, error)
	// Glob expands an installation pattern; nil uses filepath.Glob.
	Glob func(string) ([]string, error)
	// Stat probes a path; nil uses os.Stat.
	Stat func(string) (os.FileInfo, error)
	// Output runs the node binary when a version has to be read from it; nil
	// collects output with the real runner.
	Output run.Outputer
}

// NewResolver returns a resolver bound to the real machine.
func NewResolver() *Resolver {
	return &Resolver{LookPath: run.LookPath, Glob: filepath.Glob, Stat: os.Stat, Output: run.NewRunner()}
}

// output returns the collector used to read a version from a node binary.
func (r *Resolver) output() run.Outputer {
	if r.Output != nil {
		return r.Output
	}
	return run.NewRunner()
}

// Resolve finds the Node runtime to use.
//
// Returns:
//   - the resolved installation.
//   - an error when no runtime can be found at all, or when a pinned release was
//     asked for and is not installed anywhere.
func (r *Resolver) Resolve(prefs Preferences) (Installation, error) {
	requested := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prefs.Version), "v"))
	latest := requested == "" || strings.EqualFold(requested, Latest)

	candidates := r.installed(r.home(prefs.Home))
	if !latest {
		for _, candidate := range candidates {
			if Matches(candidate.version, requested) {
				return candidate.installation(), nil
			}
		}
		return Installation{}, fmt.Errorf(
			"找不到 Node %s(已查找 nvm 与 fnm 的安装目录)\n"+
				"可选: 安装该版本;把配置里的 nodeVersion 改成已安装的版本或 %q;"+
				"或临时用环境变量 %s=%s 使用 PATH 中最新的 Node",
			requested, Latest, paths.EnvNodeVersion, Latest)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return Compare(candidates[i].version, candidates[j].version) > 0
	})
	if len(candidates) > 0 {
		return candidates[0].installation(), nil
	}

	path, err := r.lookPath("node")
	if err != nil {
		return Installation{}, fmt.Errorf("找不到可用的 node(已查找 nvm、fnm 与 PATH): 请安装 Node 或把它加入 PATH")
	}
	return Installation{NodePath: path, BinDir: filepath.Dir(path), Source: SourcePath}, nil
}

// Info runs `<node> -v` to learn the release of an installation whose version is
// not implied by a directory name, such as a PATH hit.
//
// When the requested version is known and the found one differs, Requested is
// set so the caller can warn instead of silently using something else.
func (r *Resolver) Info(ctx context.Context, installation Installation, requested string) Installation {
	if installation.Version == "" {
		if out, err := r.output().Output(ctx, run.Command{Name: installation.NodePath, Args: []string{"-v"}}); err == nil {
			installation.Version = ParseVersion(out)
		}
	}
	wanted := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(requested), "v"))
	if wanted != "" && !strings.EqualFold(wanted, Latest) && installation.Version != "" &&
		!Matches(installation.Version, wanted) {
		installation.Requested = wanted
	}
	return installation
}

// home fills in the home directory the version managers live under.
//
// Every installation root is home-relative, and filepath.Join("", "…") produces
// a path relative to the process's working directory rather than an empty one: a
// resolver handed no home would search whatever tree dshctl happened to be
// started in and could offer a node binary from an unrelated checkout. An unset
// home is therefore resolved from the platform, and when even that fails the
// search is skipped rather than pointed at the current directory.
func (r *Resolver) home(preferred string) string {
	if trimmed := strings.TrimSpace(preferred); trimmed != "" {
		return trimmed
	}
	resolved, err := paths.Home()
	if err != nil {
		return ""
	}
	return resolved
}

// installed lists the nvm and fnm releases under home.
//
// An empty home yields nothing at all: there is no absolute place to look, and
// searching a relative one would answer with the working directory.
func (r *Resolver) installed(home string) []candidate {
	if strings.TrimSpace(home) == "" {
		return nil
	}
	candidates := make([]candidate, 0, 4)
	candidates = append(candidates, r.collect(filepath.Join(home, ".nvm", "versions", "node"), "*", SourceNVM)...)
	// nvm-windows keeps the runtime directly under the version directory,
	// without the bin/ level the Unix layout has.
	candidates = append(candidates, r.collect(filepath.Join(home, "AppData", "Roaming", "nvm"), "*", SourceNVM)...)
	for _, root := range []string{
		filepath.Join(home, "Library", "Application Support", "fnm", "node-versions"),
		filepath.Join(home, ".local", "share", "fnm", "node-versions"),
		filepath.Join(home, "AppData", "Roaming", "fnm", "node-versions"),
	} {
		// fnm keeps the runtime one level below the version directory.
		candidates = append(candidates, r.collect(root, filepath.Join("*", "installation"), SourceFNM)...)
		// Older fnm layouts put the runtime directly under the version.
		candidates = append(candidates, r.collect(root, "*", SourceFNM)...)
	}
	return candidates
}

// candidate is one discovered installation.
type candidate struct {
	version  string
	nodePath string
	source   string
}

// installation converts a candidate into the exported form.
func (c candidate) installation() Installation {
	return Installation{
		Version:  c.version,
		NodePath: c.nodePath,
		BinDir:   filepath.Dir(c.nodePath),
		Source:   c.source,
	}
}

// collect expands one installation root and keeps the entries that hold a node
// binary. Directory names carry the version, so no process is started here.
//
// The binary is looked for in both layouts — bin/<name> for Unix nvm and fnm,
// and the version directory itself for nvm-windows and fnm's Windows layout —
// because a search that only understood one would silently find nothing on the
// other platform and report "no Node installed" for a working installation.
func (r *Resolver) collect(root, pattern, source string) []candidate {
	if root == "" {
		return nil
	}
	dirs, err := r.glob(filepath.Join(root, pattern))
	if err != nil {
		return nil
	}
	found := make([]candidate, 0, len(dirs))
	for _, dir := range dirs {
		info, err := r.stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		var nodePath string
		for _, candidate := range []string{
			filepath.Join(dir, "bin", nodeBinaryName),
			filepath.Join(dir, nodeBinaryName),
		} {
			if r.isExecutableFile(candidate) {
				nodePath = candidate
				break
			}
		}
		if nodePath == "" {
			continue
		}
		version := versionFromPath(dir)
		if version == "" {
			continue
		}
		found = append(found, candidate{version: version, nodePath: nodePath, source: source})
	}
	return found
}

// versionFromPath reads the release out of an installation directory name.
//
// The version is not always the last element: fnm nests the runtime in an
// "installation" directory.
func versionFromPath(dir string) string {
	name := filepath.Base(dir)
	if strings.EqualFold(name, "installation") {
		name = filepath.Base(filepath.Dir(dir))
	}
	if name == "." || name == string(filepath.Separator) || name == "" {
		return ""
	}
	version := strings.TrimPrefix(name, "v")
	if version == "" || !startsWithDigit(version) {
		return ""
	}
	return version
}

// isExecutableFile reports whether path is a regular file with an execute bit on
// Unix. Windows has no execute bit, so the regular-file test is the whole check
// there.
func (r *Resolver) isExecutableFile(path string) bool {
	info, err := r.stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return isExecutable(info.Mode())
}

// glob expands a pattern.
func (r *Resolver) glob(pattern string) ([]string, error) {
	if r.Glob == nil {
		return filepath.Glob(pattern)
	}
	return r.Glob(pattern)
}

// stat probes a path.
func (r *Resolver) stat(path string) (os.FileInfo, error) {
	if r.Stat == nil {
		return os.Stat(path)
	}
	return r.Stat(path)
}

// lookPath resolves node on PATH.
func (r *Resolver) lookPath(name string) (string, error) {
	if r.LookPath == nil {
		return run.LookPath(name)
	}
	return r.LookPath(name)
}

// ParseVersion extracts a release number from `node -v` output.
//
// The version is the first whitespace-delimited token of the line: anything
// after it — a warning on the same line, a second line, a stray carriage return
// — is not part of it. Every kind of space ends the token, not only the ASCII
// ones a caller thinks of, because a value that came back with whitespace inside
// it would be compared against directory names and pins as if it were a release.
func ParseVersion(out string) string {
	trimmed := strings.TrimSpace(out)
	trimmed = strings.TrimPrefix(trimmed, "v")
	if trimmed == "" {
		return ""
	}
	if index := strings.IndexFunc(trimmed, unicode.IsSpace); index >= 0 {
		trimmed = trimmed[:index]
	}
	return trimmed
}

// Compare orders two release numbers the way a package manager would. Missing
// segments count as zero and a pre-release or build suffix is ignored, which is
// enough for the decisions dshctl makes.
func Compare(left, right string) int {
	a := numericSegments(left)
	b := numericSegments(right)
	length := len(a)
	if len(b) > length {
		length = len(b)
	}
	for index := 0; index < length; index++ {
		var leftValue, rightValue int
		if index < len(a) {
			leftValue = a[index]
		}
		if index < len(b) {
			rightValue = b[index]
		}
		switch {
		case leftValue < rightValue:
			return -1
		case leftValue > rightValue:
			return 1
		}
	}
	return 0
}

// Matches reports whether an installed release satisfies a request, so that
// "24.20" and "24.20.0" mean the same release instead of failing an exact
// string comparison.
func Matches(installed, requested string) bool {
	if strings.TrimSpace(installed) == "" || strings.TrimSpace(requested) == "" {
		return false
	}
	return Compare(installed, requested) == 0
}

// AtLeast reports whether the installation satisfies a minimum. An unknown
// version never satisfies it, so callers warn instead of assuming.
func (i Installation) AtLeast(minimum string) bool {
	if i.Version == "" {
		return false
	}
	return Compare(i.Version, minimum) >= 0
}

// numericSegments splits a version into its leading numeric components.
func numericSegments(version string) []int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if index := strings.IndexAny(trimmed, "-+"); index >= 0 {
		trimmed = trimmed[:index]
	}
	parts := strings.Split(trimmed, ".")
	segments := make([]int, 0, len(parts))
	for _, part := range parts {
		digits := part
		for index, char := range part {
			if char < '0' || char > '9' {
				digits = part[:index]
				break
			}
		}
		value, err := strconv.Atoi(digits)
		if err != nil {
			value = 0
		}
		segments = append(segments, value)
	}
	return segments
}

// startsWithDigit reports whether a version directory name is usable.
func startsWithDigit(value string) bool {
	return value != "" && value[0] >= '0' && value[0] <= '9'
}

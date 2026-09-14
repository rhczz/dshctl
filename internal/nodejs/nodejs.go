// Package nodejs resolves the Node.js runtime dshctl launches the Web server
// with.
//
// Resolution has two modes and one gate.
//
//   - A release was asked for (the configuration names one, or the operator
//     passed --node): it is looked for in the version managers first, which
//     costs no process, and on PATH second, where it must match exactly.
//   - Nothing was asked for: the runtime is whatever `node` on PATH is, and its
//     release is read from the binary itself.
//
// Which release is asked for is decided by the caller (see internal/config);
// whether the result is usable is decided by Assess, so the same verdict applies
// to every source instead of only to some.
package nodejs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/run"
)

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
	// Version is the release without its leading v.
	Version string
	// NodePath is the path of the binary that will run: the real interpreter,
	// not the forwarding shim PATH named, when the two differ.
	NodePath string
	// BinDir is the directory prepended to PATH so pnpm and node agree.
	BinDir string
	// Source names where the installation came from.
	Source string
	// ViaShim reports that the PATH hit was a forwarding entry — a shim or a
	// symlink — and that NodePath and BinDir name what it forwards to.
	ViaShim bool
}

// Preferences describes what the caller asked for.
type Preferences struct {
	// Version is a release such as "24.20.0". An empty value means "use the
	// node the environment already provides", which is resolved from PATH.
	Version string
	// Home is the operating-system home directory holding the version managers.
	// It is only consulted when a release was asked for.
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
	// Output runs the node binary to learn what it is; nil collects output with
	// the real runner.
	Output run.Outputer
}

// NewResolver returns a resolver bound to the real machine.
func NewResolver() *Resolver {
	return &Resolver{LookPath: run.LookPath, Glob: filepath.Glob, Stat: os.Stat, Output: run.NewRunner()}
}

// probeTimeout bounds how long a node binary is given to answer.
//
// A version-manager shim can decide to install a runtime on first use and block
// on the network. A preflight that waits forever is worse than one that fails,
// so the wait is bounded before the process is started.
const probeTimeout = 10 * time.Second

// Resolve finds the Node runtime to use.
//
// Returns:
//   - the resolved installation.
//   - a non-nil *Failure when no usable runtime could be found: a requested
//     release is installed nowhere, PATH has no node, or the node on PATH cannot
//     say what it is. The concrete type is returned rather than an error so that
//     a caller cannot forget to render the failure: there is no other kind of
//     failure this function reports.
func (r *Resolver) Resolve(ctx context.Context, prefs Preferences) (Installation, *Failure) {
	// Only an absent request means "discover". A value that names nothing —
	// "latest", "v", a typo — is a request that cannot be satisfied, and saying
	// so is the difference between a corrected setting and a silently different
	// runtime.
	requested := strings.TrimSpace(prefs.Version)
	if requested == "" {
		return r.discover(ctx)
	}
	return r.findRequested(ctx, requested, prefs.Home)
}

// discover uses the runtime the environment already provides.
//
// The binary on PATH is asked what it is, because there is no directory name to
// trust here: a binary that cannot answer is not a runtime dshctl can claim
// anything about.
func (r *Resolver) discover(ctx context.Context) (Installation, *Failure) {
	path, err := r.lookPath("node")
	if err != nil {
		return Installation{}, &Failure{Observations: []Observation{{Source: SourcePath, Detail: "没有 node"}}}
	}
	installation, probeErr := r.inspect(ctx, path)
	if probeErr != nil {
		return Installation{}, &Failure{
			Observations: []Observation{{Source: SourcePath, Path: path, Detail: unusableDetail(probeErr)}},
			Err:          probeErr,
		}
	}
	return installation, nil
}

// findRequested looks for one release: the version managers first, which costs
// no process because the directory name carries the release, and PATH second,
// where the release has to be read from the binary and must match exactly.
func (r *Resolver) findRequested(ctx context.Context, requested, home string) (Installation, *Failure) {
	candidates := r.installed(r.home(home))
	for _, candidate := range candidates {
		if Matches(candidate.version, requested) {
			return candidate.installation(), nil
		}
	}
	managers := managersObservation(candidates)

	path, err := r.lookPath("node")
	if err != nil {
		return Installation{}, &Failure{
			Requested:    requested,
			Observations: []Observation{managers, {Source: SourcePath, Detail: "没有 node"}},
		}
	}
	installation, probeErr := r.inspect(ctx, path)
	if probeErr != nil {
		return Installation{}, &Failure{
			Requested:    requested,
			Observations: []Observation{managers, {Source: SourcePath, Path: path, Detail: unusableDetail(probeErr)}},
			Err:          probeErr,
		}
	}
	if Matches(installation.Version, requested) {
		return installation, nil
	}
	return Installation{}, &Failure{
		Requested:    requested,
		Observations: []Observation{managers, {Source: SourcePath, Path: path, Version: installation.Version}},
	}
}

// inspect asks a node binary what it is: which release it is, and — when it can
// say — which interpreter actually runs. The two answers differ when PATH names
// a forwarding entry, and the forwarder can resolve differently at exec time, so
// the real interpreter is what gets used.
func (r *Resolver) inspect(ctx context.Context, path string) (Installation, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	out, err := r.output().Output(ctx, run.Command{Name: path, Args: []string{"-v"}})
	if err != nil {
		return Installation{}, fmt.Errorf("无法执行 `%s -v`: %w", path, err)
	}
	version := ParseVersion(out)
	// The token has to look like a release. A binary that answers with a
	// sentence is not a runtime dshctl can describe, and reporting it as an
	// unreadable version is clearer than letting "not" travel onward as a release
	// that the gate then refuses.
	if version == "" || !startsWithDigit(version) {
		return Installation{}, fmt.Errorf("`%s -v` 的输出里没有版本: %q", path, strings.TrimSpace(out))
	}
	installation := Installation{
		Version:  version,
		NodePath: path,
		BinDir:   filepath.Dir(path),
		Source:   SourcePath,
	}

	// The second question is best-effort: an answer that is not an absolute path
	// tells nothing, and a binary that cannot answer it is still usable.
	real, err := r.output().Output(ctx, run.Command{Name: path, Args: []string{"-p", "process.execPath"}})
	if err != nil {
		return installation, nil
	}
	execPath := strings.TrimSpace(real)
	if execPath == "" || !filepath.IsAbs(execPath) {
		return installation, nil
	}
	if filepath.Dir(execPath) != installation.BinDir {
		installation.ViaShim = true
	}
	installation.NodePath = execPath
	installation.BinDir = filepath.Dir(execPath)
	return installation, nil
}

// unusableDetail explains why a binary on PATH could not be turned into a
// runtime, in the words of the failure itself.
func unusableDetail(err error) string { return "无法确定版本: " + err.Error() }

// managersObservation summarises what the version managers had to offer.
func managersObservation(candidates []candidate) Observation {
	if len(candidates) == 0 {
		return Observation{Source: SourceManagers, Detail: "没有安装"}
	}
	// installed() reports releases from the highest down.
	return Observation{Source: SourceManagers, Detail: "最新的是 " + candidates[0].version}
}

// Failure reports why no runtime could be resolved. It carries facts rather
// than a message: the wording belongs to the caller, which knows the release
// dshctl requires.
type Failure struct {
	// Requested is the release that was asked for, or empty when the runtime was
	// to be discovered on PATH.
	Requested string
	// Observations records what each place that was looked at had to offer.
	Observations []Observation
	// Err is the reason a candidate that existed could not be used.
	Err error
}

// Error renders a compact technical summary, for logs and for wrapping.
func (f *Failure) Error() string {
	switch {
	case f.Requested != "":
		return fmt.Sprintf("找不到 Node %s", f.Requested)
	case f.Err != nil:
		return fmt.Sprintf("PATH 上的 node 无法使用: %v", f.Err)
	default:
		return "PATH 上没有 node"
	}
}

// Unwrap exposes the underlying failure, when there was one.
func (f *Failure) Unwrap() error { return f.Err }

// Observation is what one place had to offer.
type Observation struct {
	// Source is SourceNVM, SourceFNM, SourcePath, or SourceManagers for the
	// summary of both version managers.
	Source string
	// Path is the binary that was examined, when there was one.
	Path string
	// Version is the release that was found, when it could be read.
	Version string
	// Detail explains the state of this source in one clause.
	Detail string
}

// SourceManagers labels the summary of the version-manager scan.
const SourceManagers = "managers"

// Verdict statuses.
const (
	// Supported means the release is inside the range dshctl is verified for.
	Supported = "supported"
	// Untested means the release is usable but outside the verified range.
	Untested = "untested"
	// TooOld means the release is below the minimum dshctl runs with.
	TooOld = "too-old"
)

// Verdict is the judgment on a resolved installation.
type Verdict struct {
	// Status is one of the status constants.
	Status string
	// Reason explains a status other than Supported in one line.
	Reason string
	// Remedy is the fix block, set when the installation must not be used.
	Remedy string
}

// Assess judges a resolved installation against the supported range.
//
// The minimum is a hard floor: a release below it is refused whatever named it,
// because dshctl cannot serve a Web client with it. A release in another major
// version than the one dshctl is verified against is usable but reported.
func Assess(installation Installation, minimum, tested string) Verdict {
	if Compare(installation.Version, minimum) < 0 {
		return Verdict{
			Status: TooOld,
			Reason: fmt.Sprintf("Node %s 低于最低要求 %s(%s，来源 %s)",
				installation.Version, minimum, installation.NodePath, originLabel(installation.Source)),
			Remedy: Remedies(minimum, tested),
		}
	}
	if majorOf(installation.Version) != majorOf(tested) {
		return Verdict{
			Status: Untested,
			Reason: fmt.Sprintf("Node %s 不在 dshctl 的验证范围内(已验证 %s；%s，来源 %s)；"+
				"若 Web 端出现 \"Failed to load plugins\" 请改用 Node %d.x",
				installation.Version, tested, installation.NodePath, originLabel(installation.Source), majorOf(tested)),
		}
	}
	return Verdict{Status: Supported}
}

// Remedies renders the fix block: one line per way of installing Node, plus the
// one-shot escape hatch that fixes the version for this installation only.
func Remedies(minimum, tested string) string {
	major := majorOf(minimum)
	return fmt.Sprintf(`修复(任选一种):
  nvm:      nvm install %[1]d && nvm alias default %[1]d
  fnm:      fnm install %[1]d && fnm default %[1]d
  Homebrew: brew install node@%[1]d
  n:        n %[1]d
  Volta:    volta install node@%[1]d
  asdf:     asdf install nodejs %[2]s && asdf global nodejs %[2]s
  mise:     mise use -g node@%[1]d
  nodenv:   nodenv install %[2]s && nodenv global %[2]s
  官方安装包: https://nodejs.org/en/download
也可以只指定一次: --node <版本> 或 DSH_NODE_VERSION=<版本>(成功后写入配置)`, major, tested)
}

// Describe renders the operator-facing explanation of a resolution failure: what
// was asked for, what each place had to offer, and every way to fix it.
func Describe(failure *Failure, minimum, tested string) string {
	var builder strings.Builder
	if failure.Requested != "" {
		fmt.Fprintf(&builder, "找不到 Node %s(已查找 nvm/fnm 的安装目录与 PATH)", failure.Requested)
	} else {
		builder.WriteString("找不到可用的 node")
	}
	for _, observation := range failure.Observations {
		builder.WriteString("\n")
		builder.WriteString(describeObservation(observation))
	}
	builder.WriteString("\n")
	builder.WriteString(Remedies(minimum, tested))
	return builder.String()
}

// output returns the collector used to ask a node binary what it is.
func (r *Resolver) output() run.Outputer {
	if r.Output != nil {
		return r.Output
	}
	return run.NewRunner()
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

// installed lists the nvm and fnm releases under home, newest first.
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
	sort.SliceStable(candidates, func(i, j int) bool {
		return Compare(candidates[i].version, candidates[j].version) > 0
	})
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
		for _, attempt := range []string{
			filepath.Join(dir, "bin", nodeBinaryName),
			filepath.Join(dir, nodeBinaryName),
		} {
			if r.isExecutableFile(attempt) {
				nodePath = attempt
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
// it would be compared against directory names and requests as if it were a
// release.
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

// majorOf reports the major version of a release, or 0 when it has none.
//
// numericSegments always answers with at least one segment — splitting the empty
// string yields one empty one — so the leading segment is always there.
func majorOf(version string) int {
	return numericSegments(version)[0]
}

// observationLabel names a source the way the failure text refers to it. The
// manager summary covers both managers, and PATH is spelled the way operators
// spell it.
func observationLabel(source string) string {
	switch source {
	case SourcePath:
		return "PATH"
	case SourceManagers:
		return "nvm/fnm"
	default:
		return source
	}
}

// originLabel names where a resolved installation came from, inside a sentence.
func originLabel(source string) string {
	if source == SourcePath {
		return "PATH"
	}
	return source
}

// describeObservation renders one observation as a line of the failure text.
func describeObservation(observation Observation) string {
	label := observationLabel(observation.Source)
	switch {
	case observation.Path != "" && observation.Version != "":
		return fmt.Sprintf("  %s: %s 是 %s", label, observation.Path, observation.Version)
	case observation.Path != "":
		return fmt.Sprintf("  %s: %s %s", label, observation.Path, observation.Detail)
	default:
		return fmt.Sprintf("  %s: %s", label, observation.Detail)
	}
}

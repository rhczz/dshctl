// Package config resolves dshctl's settings with the precedence
// command line > environment > config file > built-in default.
//
// Only deliberately tunable values live here. File names, polling intervals and
// process probes are implementation details and stay in code; every setting in
// this package has a default that works without a config file, so dshctl is
// usable the moment it is installed.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rhczz/dshctl/internal/atomically"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/paths"
)

// Built-in defaults. Every one of them is a working configuration on its own.
const (
	// DefaultPort is the loopback port the Web server binds.
	DefaultPort = 3080
	// TestedNodeVersion is the Node release deepseek-harness is developed and
	// verified against. It is the release the remedy text recommends and the
	// major version the gate treats as known: the Web client scan needs the
	// ModuleLoader.resolveSync v2 signature that stabilized in 24.12, and nothing
	// newer has been exercised.
	TestedNodeVersion = "24.20.0"
	// MinNodeVersion is the oldest Node dshctl treats as supported. It is a
	// constant on purpose: no setting, flag or environment variable can lower the
	// floor, so a machine cannot be talked into a runtime the Web client cannot
	// work with.
	MinNodeVersion = "24.12.0"
	// DefaultStartTimeout bounds how long start waits for the server to answer.
	DefaultStartTimeout = 90 * time.Second
	// DefaultStopTimeout bounds how long stop waits for the port to close.
	DefaultStopTimeout = 15 * time.Second
	// DefaultLockTimeout bounds how long an operation waits for the lock.
	DefaultLockTimeout = 10 * time.Second
	// DefaultLogRotateBytes is the size at which the log rolls to <log>.old.
	DefaultLogRotateBytes = 4 << 20
	// DefaultRepoDirName is the checkout directory looked for in the home
	// directory when nothing configures one. It is the only place the layout of
	// a particular machine used to be hardcoded.
	DefaultRepoDirName = "deepseek-harness"
	// DefaultLogFileName is the log inside the state directory.
	DefaultLogFileName = "dsh-web.log"
	// LockFileName serializes mutating operations inside the state directory.
	LockFileName = "dshctl.lock"
	// StateFileNamePattern is the runtime record of the server dshctl started.
	//
	// The record is per port. One state directory may manage several ports, and a
	// single shared record would make the second start overwrite the first: the
	// first server would keep serving while dshctl could no longer recognize it,
	// so it could never be stopped again. The log and the lock stay shared on
	// purpose — one place to look, and one operation at a time.
	StateFileNamePattern = "dsh-web-%d.state.json"
	// ConfigFileName is the settings document inside the state directory.
	ConfigFileName = "config.json"
	// ServerManifestRel names the checkout root marker. Its presence is what
	// distinguishes the managed checkout from any other git repository, so a
	// mistyped --repo can never point the build's cleanup at a stranger.
	ServerManifestRel = "package.json"
	// WorkspaceManifestRel names the pnpm workspace marker.
	WorkspaceManifestRel = "pnpm-workspace.yaml"
)

// Bounds that keep a configured number from silently misbehaving.
const (
	// MinPort and MaxPort are the ports a TCP listener can use.
	MinPort = 1
	// MaxPort is the highest assignable TCP port.
	MaxPort = 65535
	// MaxTimeoutSeconds bounds every configured timeout so that
	// time.Duration(seconds)*time.Second cannot overflow into a negative
	// duration, which would make an operation fail or force-kill instantly.
	MaxTimeoutSeconds = 86400
	// MinRotateBytes is the smallest rotation threshold that makes sense; below
	// it every write would rotate.
	MinRotateBytes = 64 << 10
)

// Settings is the effective configuration.
type Settings struct {
	// RepoDir is the deepseek-harness checkout dshctl builds and runs.
	RepoDir string
	// Port is the loopback port the Web server binds.
	Port int
	// NodeVersion is the Node release this installation runs, or empty when it
	// has not been determined yet. An empty value means "use the node the
	// environment provides": the resolution reads it from PATH, and a successful
	// start writes what it used back into the settings document.
	NodeVersion string
	// ConfiguredNodeVersion is the release the settings document names, or empty
	// when the document names none. It is what decides whether a successful start
	// writes the release back: an installation whose file already names one is
	// never rewritten.
	ConfiguredNodeVersion string
	// StartTimeout bounds the wait for the server to answer.
	StartTimeout time.Duration
	// StopTimeout bounds the wait for the server to stop.
	StopTimeout time.Duration
	// LockTimeout bounds the wait for another dshctl operation.
	LockTimeout time.Duration
	// LogRotateBytes is the size at which the log rolls; 0 disables rotation.
	LogRotateBytes int64
	// StateDir is the resolved state directory.
	StateDir string
	// ConfigPath is the resolved config file path.
	ConfigPath string
	// LogPath is the resolved log file path.
	LogPath string
	// Sources names where each value came from, for -v output.
	Sources Sources
}

// Sources records which layer supplied each setting.
type Sources struct {
	// RepoDir is one of "flag", "env", "file", or "default".
	RepoDir string
	// Port names the source of Port.
	Port string
	// NodeVersion names the source of NodeVersion.
	NodeVersion string
	// Timeouts names the source of the timeout and rotation values.
	Timeouts string
	// StateDir names the source of the state directory.
	StateDir string
	// ConfigPath names the source of the config file path.
	ConfigPath string
	// LogPath names the source of the log file path.
	LogPath string
}

// File is the on-disk settings document.
//
// Every field is optional. A field that is absent — or explicitly null — keeps
// the value from the layer below it, so a document may name only the settings an
// operator actually wants to change.
type File struct {
	// RepoDir is the managed checkout.
	RepoDir *string `json:"repoDir,omitempty"`
	// Port is the loopback port the Web server binds.
	Port *int `json:"port,omitempty"`
	// NodeVersion is the preferred Node release, or "latest".
	NodeVersion *string `json:"nodeVersion,omitempty"`
	// StartTimeoutSeconds bounds the wait for the server to answer.
	StartTimeoutSeconds *int `json:"startTimeoutSeconds,omitempty"`
	// StopTimeoutSeconds bounds the wait for the server to stop.
	StopTimeoutSeconds *int `json:"stopTimeoutSeconds,omitempty"`
	// LockTimeoutSeconds bounds the wait for another dshctl operation.
	LockTimeoutSeconds *int `json:"lockTimeoutSeconds,omitempty"`
	// LogRotateBytes is the size at which the log rolls; 0 disables rotation.
	LogRotateBytes *int64 `json:"logRotateBytes,omitempty"`
}

// Overrides are the optional values a caller can supply from flags.
type Overrides struct {
	// ConfigPath wins over every other source for the settings document itself.
	ConfigPath *string
	// RepoDir wins over every other source.
	RepoDir *string
	// Port wins over every other source.
	Port *int
	// NodeVersion wins over every other source.
	NodeVersion *string
}

// Default returns the configuration used when nothing is configured. The
// repository default sits in the operator's home directory rather than at a
// path that only makes sense on one machine.
func Default(home string) Settings {
	return Settings{
		RepoDir:        filepath.Join(home, DefaultRepoDirName),
		Port:           DefaultPort,
		StartTimeout:   DefaultStartTimeout,
		StopTimeout:    DefaultStopTimeout,
		LockTimeout:    DefaultLockTimeout,
		LogRotateBytes: DefaultLogRotateBytes,
	}
}

// Load resolves the effective settings without writing anything, so reporting
// commands (status, logs, url, doctor, version) cannot create state as a side
// effect of being run.
//
// Parameters:
//   - getenv: environment lookup.
//   - overrides: command-line values that win over every other source.
//
// Returns:
//   - the resolved settings.
//   - a Usage-coded error when a configured value is invalid, and the plain
//     underlying error when a path cannot be resolved.
func Load(getenv paths.Getenv, overrides Overrides) (Settings, error) {
	home, err := paths.Home()
	if err != nil {
		return Settings{}, err
	}
	stateDir, stateSource, err := resolveStateDir(getenv)
	if err != nil {
		return Settings{}, usagef("%v", err)
	}
	configPath, configSource, err := resolveConfigPath(getenv, stateDir, overrides.ConfigPath)
	if err != nil {
		return Settings{}, usagef("%v", err)
	}

	settings := Default(home)
	sources := Sources{
		RepoDir:     "default",
		Port:        "default",
		NodeVersion: "default",
		StateDir:    stateSource,
		ConfigPath:  configSource,
	}

	document, found, err := readFile(configPath)
	if err != nil {
		return Settings{}, err
	}
	if found {
		if err := applyFile(&settings, document); err != nil {
			return Settings{}, usagef("%v", err)
		}
		markFileSources(&sources, document)
	}
	if err := applyEnv(&settings, getenv); err != nil {
		return Settings{}, err
	}
	markEnvSources(&sources, getenv)
	if err := applyOverrides(&settings, overrides); err != nil {
		return Settings{}, usagef("%v", err)
	}
	if overrides.RepoDir != nil {
		sources.RepoDir = "flag"
	}
	if overrides.Port != nil {
		sources.Port = "flag"
	}
	// The Node release is layered last because it needs the document itself, not
	// just the settings the document produced (see applyNodeVersion).
	applyNodeVersion(&settings, &sources, document, found, getenv, overrides)

	logPath, logSource, err := resolveLogPath(getenv, stateDir)
	if err != nil {
		return Settings{}, usagef("%v", err)
	}
	sources.LogPath = logSource

	settings.StateDir = stateDir
	settings.ConfigPath = configPath
	settings.LogPath = logPath
	settings.Sources = sources

	if err := settings.Validate(); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

// Provision creates the state directory and writes the settings document on
// first use, so the tunable surface is discoverable on disk. The document
// records the built-in defaults rather than the values in effect, so a file
// generated while an override was set never freezes that override into the next
// run.
//
// Returns:
//   - an error when the directory or the file cannot be written.
func (s Settings) Provision() error {
	if err := paths.EnsureDir(s.StateDir); err != nil {
		return err
	}
	// A process killed mid-write leaves a temporary file behind and nothing else
	// ever collects it, so the directory is swept whenever it is provisioned.
	if err := atomically.Sweep(s.StateDir); err != nil {
		return err
	}
	if paths.IsRegularFile(s.ConfigPath) {
		return nil
	}
	if paths.IsDir(s.ConfigPath) {
		// A directory at the config path cannot be read as a settings document;
		// failing here is clearer than failing on every later load.
		return fmt.Errorf("配置路径 %s 是目录，无法写入默认配置", s.ConfigPath)
	}
	// A symlink or other residue falls through: WriteFile renames the document
	// over it, which is how a stale symlink is retired without following it.
	home, err := paths.Home()
	if err != nil {
		return err
	}
	data, err := Encode(Default(home))
	if err != nil {
		return err
	}
	return atomically.WriteFile(s.ConfigPath, data, 0o600)
}

// Validate rejects a configuration that would misfire later, so a typo fails at
// load with a message naming the field instead of at the first operation.
//
// Returns:
//   - a Usage-coded error naming the first invalid field.
func (s Settings) Validate() error {
	if strings.TrimSpace(s.RepoDir) == "" {
		return usagef("repoDir 不能为空")
	}
	if !filepath.IsAbs(s.RepoDir) {
		return usagef("repoDir 必须是绝对路径: %s", s.RepoDir)
	}
	if s.Port < MinPort || s.Port > MaxPort {
		return usagef("port 必须在 %d-%d 之间: %d", MinPort, MaxPort, s.Port)
	}
	// NodeVersion is deliberately not validated here. An empty value is legal and
	// means "not determined yet"; a value that names no installed release is a
	// resolution failure, not a typo in a document, and it is reported where the
	// runtime is resolved — which keeps `status` and `logs` working on a machine
	// whose runtime has been uninstalled.
	if err := validateTimeout("startTimeoutSeconds", s.StartTimeout); err != nil {
		return err
	}
	if err := validateTimeout("stopTimeoutSeconds", s.StopTimeout); err != nil {
		return err
	}
	if err := validateTimeout("lockTimeoutSeconds", s.LockTimeout); err != nil {
		return err
	}
	if s.LogRotateBytes < 0 {
		return usagef("logRotateBytes 不能为负: %d", s.LogRotateBytes)
	}
	if s.LogRotateBytes > 0 && s.LogRotateBytes < MinRotateBytes {
		return usagef("logRotateBytes 不能小于 %d (否则每次写入都会轮转): %d", MinRotateBytes, s.LogRotateBytes)
	}
	if !filepath.IsAbs(s.StateDir) {
		return usagef("状态目录必须是绝对路径: %s", s.StateDir)
	}
	if !filepath.IsAbs(s.LogPath) {
		return usagef("日志文件必须是绝对路径: %s", s.LogPath)
	}
	return nil
}

// StateFile is the runtime record of the server dshctl started on this port.
func (s Settings) StateFile() string {
	return filepath.Join(s.StateDir, fmt.Sprintf(StateFileNamePattern, s.Port))
}

// StateFileGlob is the pattern that finds the runtime record of every port in a
// state directory.
//
// The verb is replaced before the directory is joined, so a digit in the
// directory path can never be mistaken for the port placeholder: replacing the
// first "0" in the whole path silently matched nothing whenever the state
// directory happened to contain one, and a guard that sees no other server
// lets an update rewrite the checkout underneath it.
//
// The directory is quoted as a literal for the same reason. A state directory is
// an ordinary path and may hold any character a file name may hold, so a "[", a
// "*" or a "?" in it would otherwise be read as pattern syntax — the guard would
// then match nothing, and matching nothing is exactly how it concludes that no
// other server exists.
func StateFileGlob(stateDir string) string {
	return filepath.Join(quoteGlob(stateDir), strings.Replace(StateFileNamePattern, "%d", "*", 1))
}

// quoteGlob spells path as a pattern that matches that literal path.
//
// The platforms need different spellings: filepath.Match disables backslash
// escaping on Windows, where a backslash is a path separator, so every special
// character is written there as a one-character class instead. A closing bracket
// needs no quoting on Windows at all, because it is only special inside a class.
func quoteGlob(path string) string {
	if runtime.GOOS == "windows" {
		return strings.NewReplacer(
			"*", "[*]",
			"?", "[?]",
			"[", "[[]",
		).Replace(path)
	}
	return strings.NewReplacer(
		`\`, `\\`,
		"*", `\*`,
		"?", `\?`,
		"[", `\[`,
		"]", `\]`,
	).Replace(path)
}

// LockFile is the advisory lock mutating operations serialize on.
func (s Settings) LockFile() string { return filepath.Join(s.StateDir, LockFileName) }

// URL is the address the Web server answers on, without a token.
func (s Settings) URL() string { return "http://127.0.0.1:" + strconv.Itoa(s.Port) }

// RepoManifest is the root marker that identifies the managed checkout.
func (s Settings) RepoManifest() string { return filepath.Join(s.RepoDir, ServerManifestRel) }

// RepoWorkspaceManifest is the pnpm workspace marker of the managed checkout.
func (s Settings) RepoWorkspaceManifest() string {
	return filepath.Join(s.RepoDir, WorkspaceManifestRel)
}

// BuildRecordPath is the marker pnpm run build writes last.
func (s Settings) BuildRecordPath() string {
	return filepath.Join(s.RepoDir, ".dsh-build", "client-build-environment.json")
}

// nodeVersionDisplay renders the release for verbose output. An undetermined
// release is said to be undetermined rather than shown as an empty value, so the
// line stays readable and says what the next start will do about it.
func nodeVersionDisplay(version string) string {
	if strings.TrimSpace(version) == "" {
		return "(未确定，启动时按 PATH 解析)"
	}
	return version
}

// Describe renders the effective settings and where each value came from.
func (s Settings) Describe() []string {
	seconds := func(d time.Duration) string { return strconv.Itoa(int(d/time.Second)) + "s" }
	return []string{
		"配置文件: " + s.ConfigPath + " (" + s.Sources.ConfigPath + ")",
		"状态目录: " + s.StateDir + " (" + s.Sources.StateDir + ")",
		"仓库目录: " + s.RepoDir + " (" + s.Sources.RepoDir + ")",
		"监听端口: " + strconv.Itoa(s.Port) + " (" + s.Sources.Port + ")",
		"Node 版本: " + nodeVersionDisplay(s.NodeVersion) + " (" + s.Sources.NodeVersion + ")",
		"日志文件: " + s.LogPath + " (" + s.Sources.LogPath + ")",
		"启动超时: " + seconds(s.StartTimeout),
		"停止超时: " + seconds(s.StopTimeout),
		"锁超时:   " + seconds(s.LockTimeout),
		"日志轮转: " + strconv.FormatInt(s.LogRotateBytes, 10) + " 字节 (0 表示不轮转)",
	}
}

// Encode renders the settings document.
//
// The document is deliberately small: only values an operator may want to
// change appear, and every one of them may be deleted without breaking
// anything. The Node release is left out while it is undetermined, so a fresh
// document says nothing about a runtime it has not chosen yet.
func Encode(settings Settings) ([]byte, error) {
	return encode(provisionedDocument(settings))
}

// provisionedDocument renders the document a first run writes.
func provisionedDocument(settings Settings) File {
	document := File{}
	if settings.RepoDir != "" {
		repoDir := settings.RepoDir
		document.RepoDir = &repoDir
	}
	port := settings.Port
	document.Port = &port
	if version := strings.TrimSpace(settings.NodeVersion); version != "" {
		document.NodeVersion = &version
	}
	start := int(settings.StartTimeout / time.Second)
	document.StartTimeoutSeconds = &start
	stop := int(settings.StopTimeout / time.Second)
	document.StopTimeoutSeconds = &stop
	lock := int(settings.LockTimeout / time.Second)
	document.LockTimeoutSeconds = &lock
	rotate := settings.LogRotateBytes
	document.LogRotateBytes = &rotate
	return document
}

// encode marshals a settings document.
func encode(document File) ([]byte, error) {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("无法序列化配置: %w", err)
	}
	return append(data, '\n'), nil
}

// RecordNodeVersion writes the release this installation runs into the settings
// document, preserving everything else the operator wrote and creating the
// document when there is none.
//
// This is the only place dshctl writes a setting on its own initiative, and it
// writes exactly one key: the document belongs to the operator, and a write that
// re-rendered it from the values in effect would freeze flags and environment
// variables into the next run.
func (s Settings) RecordNodeVersion(version string) error {
	release := strings.TrimSpace(version)
	if release == "" {
		return errors.New("拒绝把空的 Node 版本写入配置")
	}
	document, found, err := readFile(s.ConfigPath)
	if err != nil {
		return err
	}
	if !found {
		home, err := paths.Home()
		if err != nil {
			return err
		}
		document = provisionedDocument(Default(home))
	}
	document.NodeVersion = &release

	data, err := encode(document)
	if err != nil {
		return err
	}
	if err := paths.EnsureDir(s.StateDir); err != nil {
		return err
	}
	return atomically.WriteFile(s.ConfigPath, data, 0o600)
}

// maxConfigBytes bounds the settings document. It holds a handful of fields; a
// file larger than this is not one, and reading without a bound would let a
// device or a runaway file make a reporting command hang or allocate without
// limit.
const maxConfigBytes = 64 << 10

// readFile decodes the settings document, distinguishing three outcomes: a
// missing file means "everything at its default" and is silent, a malformed one
// is a usage error that names the field, and a file that cannot be read at all —
// unreadable, not a regular file, too large — is reported as the failure it is
// rather than as a bad setting.
func readFile(path string) (File, bool, error) {
	var document File
	// The existence probe and the metadata read are separate steps on purpose.
	// A symlink is allowed — a checked-in dotfile pointing at the real document
	// is a normal setup — but whatever it resolves to must be a regular file of
	// sane size: a FIFO or a device here would block a command forever, and an
	// unbounded file would grow its memory without limit.
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return File{}, false, nil
		}
		return File{}, false, fmt.Errorf("无法读取配置文件 %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		// A dangling symlink: there is nothing to read, and Provision will
		// replace the link rather than follow it.
		return File{}, false, nil
	}
	if err != nil {
		return File{}, false, fmt.Errorf("无法读取配置文件 %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return File{}, false, fmt.Errorf("配置文件 %s 不是普通文件，无法作为配置读取", path)
	}
	if info.Size() > maxConfigBytes {
		return File{}, false, fmt.Errorf("配置文件 %s 过大 (%d 字节，上限 %d)", path, info.Size(), maxConfigBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// A file that cannot be read at all is not a bad setting: nothing the
		// operator wrote is wrong, the file simply could not be looked at. It is
		// reported as that failure rather than as a usage error — exactly like
		// the non-regular and oversized branches above — so the same condition
		// does not change status depending on which step noticed it.
		return File{}, false, fmt.Errorf("无法读取配置文件 %s: %w", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			// An empty file is a legal way to say "everything at its default".
			return File{}, true, nil
		}
		return File{}, false, usagef("配置文件 %s 解析失败: %v", path, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return File{}, false, usagef("配置文件 %s 在第一个 JSON 值之后还有内容", path)
	}
	return document, true, nil
}

// applyFile layers the config file over the defaults.
func applyFile(settings *Settings, document File) error {
	if document.RepoDir != nil {
		resolved, err := paths.Resolve(*document.RepoDir)
		if err != nil {
			return fmt.Errorf("配置文件 repoDir: %w", err)
		}
		settings.RepoDir = resolved
	}
	if document.Port != nil {
		settings.Port = *document.Port
	}
	if document.StartTimeoutSeconds != nil {
		settings.StartTimeout = time.Duration(*document.StartTimeoutSeconds) * time.Second
	}
	if document.StopTimeoutSeconds != nil {
		settings.StopTimeout = time.Duration(*document.StopTimeoutSeconds) * time.Second
	}
	if document.LockTimeoutSeconds != nil {
		settings.LockTimeout = time.Duration(*document.LockTimeoutSeconds) * time.Second
	}
	if document.LogRotateBytes != nil {
		settings.LogRotateBytes = *document.LogRotateBytes
	}
	return nil
}

// markFileSources records "file" for every field the document set. It is called
// before applyEnv so that a later environment value can overwrite the label.
func markFileSources(sources *Sources, document File) {
	if document.RepoDir != nil {
		sources.RepoDir = "file"
	}
	if document.Port != nil {
		sources.Port = "file"
	}
	if document.StartTimeoutSeconds != nil || document.StopTimeoutSeconds != nil ||
		document.LockTimeoutSeconds != nil || document.LogRotateBytes != nil {
		sources.Timeouts = "file"
	}
}

// applyEnv layers the DSH_* environment variables over the settings.
func applyEnv(settings *Settings, getenv paths.Getenv) error {
	if raw := getenv(paths.EnvRepoDir); strings.TrimSpace(raw) != "" {
		resolved, err := paths.Resolve(raw)
		if err != nil {
			return fmt.Errorf("环境变量 %s: %w", paths.EnvRepoDir, err)
		}
		settings.RepoDir = resolved
	}
	if raw := getenv(paths.EnvPort); strings.TrimSpace(raw) != "" {
		port, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return usagef("环境变量 %s 不是数字: %q", paths.EnvPort, raw)
		}
		settings.Port = port
	}
	return nil
}

// markEnvSources records "env" for every field the environment supplied.
func markEnvSources(sources *Sources, getenv paths.Getenv) {
	if strings.TrimSpace(getenv(paths.EnvRepoDir)) != "" {
		sources.RepoDir = "env"
	}
	if strings.TrimSpace(getenv(paths.EnvPort)) != "" {
		sources.Port = "env"
	}
}

// applyOverrides layers command-line values over the settings.
func applyOverrides(settings *Settings, overrides Overrides) error {
	if overrides.RepoDir != nil {
		resolved, err := paths.Resolve(*overrides.RepoDir)
		if err != nil {
			return fmt.Errorf("参数 --repo: %w", err)
		}
		settings.RepoDir = resolved
	}
	if overrides.Port != nil {
		settings.Port = *overrides.Port
	}
	return nil
}

// applyNodeVersion layers the Node release this installation runs.
//
// The order is the one every other setting follows: flag, then environment, then
// the settings document, then the default — with one addition at the bottom.
// Where the other settings have a built-in value, this one has "nothing named a
// release yet": the resolution reads it from PATH, and a successful start
// records what it used. The value the document names is kept separately because
// it is what decides whether that write-back happens at all.
func applyNodeVersion(settings *Settings, sources *Sources, document File, found bool, getenv paths.Getenv, overrides Overrides) {
	configured := ""
	if found && document.NodeVersion != nil {
		configured = strings.TrimSpace(*document.NodeVersion)
	}
	settings.ConfiguredNodeVersion = configured

	environment := strings.TrimSpace(getenv(paths.EnvNodeVersion))
	switch {
	case overrides.NodeVersion != nil && strings.TrimSpace(*overrides.NodeVersion) != "":
		settings.NodeVersion = strings.TrimSpace(*overrides.NodeVersion)
		sources.NodeVersion = "flag"
	case environment != "":
		settings.NodeVersion = environment
		sources.NodeVersion = "env"
	case configured != "":
		settings.NodeVersion = configured
		sources.NodeVersion = "file"
	default:
		// Nothing names a release: the resolution reads it from PATH and a
		// successful start writes it down.
		settings.NodeVersion = ""
		sources.NodeVersion = "default"
	}
}

// resolveStateDir resolves the state directory and names its source.
func resolveStateDir(getenv paths.Getenv) (string, string, error) {
	dir, err := paths.StateDir(getenv)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(getenv(paths.EnvStateDir)) != "" {
		return dir, "env " + paths.EnvStateDir, nil
	}
	if strings.TrimSpace(getenv(paths.EnvHarnessHome)) != "" {
		return dir, "env " + paths.EnvHarnessHome, nil
	}
	return dir, "default", nil
}

// resolveConfigPath resolves the config file and names its source.
//
// The layers are flag, environment, default, like every other path. The flag is
// layered here rather than by rewriting the environment lookup, so that the
// source `-v` prints is the layer that actually supplied the value.
func resolveConfigPath(getenv paths.Getenv, stateDir string, override *string) (string, string, error) {
	if override != nil && strings.TrimSpace(*override) != "" {
		path, err := paths.Resolve(*override)
		if err != nil {
			return "", "", fmt.Errorf("参数 --config: %w", err)
		}
		return path, "flag", nil
	}
	if strings.TrimSpace(getenv(paths.EnvConfigFile)) != "" {
		path, err := paths.ConfigFile(getenv)
		if err != nil {
			return "", "", err
		}
		return path, "env " + paths.EnvConfigFile, nil
	}
	return filepath.Join(stateDir, ConfigFileName), "default", nil
}

// resolveLogPath resolves the log file and names its source.
func resolveLogPath(getenv paths.Getenv, stateDir string) (string, string, error) {
	if raw := getenv(paths.EnvLogFile); strings.TrimSpace(raw) != "" {
		path, err := paths.Resolve(raw)
		if err != nil {
			return "", "", fmt.Errorf("环境变量 %s: %w", paths.EnvLogFile, err)
		}
		return path, "env " + paths.EnvLogFile, nil
	}
	return filepath.Join(stateDir, DefaultLogFileName), "default", nil
}

// validateTimeout rejects a duration that cannot be represented in seconds.
func validateTimeout(name string, value time.Duration) error {
	seconds := int(value / time.Second)
	if seconds < 1 {
		return usagef("%s 必须至少为 1 秒: %s", name, value)
	}
	if seconds > MaxTimeoutSeconds {
		return usagef("%s 不能超过 %d 秒: %s", name, MaxTimeoutSeconds, value)
	}
	return nil
}

// usagef builds a Usage-coded configuration error, so a bad configured value
// exits with the same status as a bad flag.
func usagef(format string, args ...any) error {
	return exitcode.Wrap(exitcode.Usage, fmt.Errorf(format, args...))
}

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/paths"
)

// fixtureHome is an absolute directory spelled the way this platform does.
//
// A Windows path cannot begin with "/", and every path in the settings is
// required to be absolute, so a fixture that hardcodes a Unix path fails there
// for reasons that have nothing to do with what the test measures. The value is
// only ever used as a string: nothing here creates it.
func fixtureHome() string {
	if runtime.GOOS == "windows" {
		return `C:\dshctl-fixture`
	}
	return "/dshctl-fixture"
}

// env is a map-backed environment lookup.
type env map[string]string

// Getenv implements paths.Getenv.
func (e env) Getenv(key string) string { return e[key] }

// resolver builds an environment that keeps every directory inside a temp root.
func resolver(t *testing.T) (env, string) {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	return env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}, stateDir
}

// TestDefaultsAreUsableWithoutAnyConfiguration pins the promise that dshctl runs
// with no config file at all.
func TestDefaultsAreUsableWithoutAnyConfiguration(t *testing.T) {
	environment, stateDir := resolver(t)
	settings, err := Load(environment.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port != DefaultPort {
		t.Fatalf("port = %d, want %d", settings.Port, DefaultPort)
	}
	if settings.NodeVersion != "" || settings.ConfiguredNodeVersion != "" {
		t.Fatalf("nodeVersion = %q/%q, want it undetermined until a start resolves one",
			settings.NodeVersion, settings.ConfiguredNodeVersion)
	}
	if settings.StartTimeout != DefaultStartTimeout || settings.StopTimeout != DefaultStopTimeout {
		t.Fatalf("timeouts = %s/%s, want the defaults", settings.StartTimeout, settings.StopTimeout)
	}
	if settings.LogRotateBytes != DefaultLogRotateBytes {
		t.Fatalf("logRotateBytes = %d, want %d", settings.LogRotateBytes, DefaultLogRotateBytes)
	}
	if settings.LogPath != filepath.Join(stateDir, DefaultLogFileName) {
		t.Fatalf("logPath = %q", settings.LogPath)
	}
	wantStateFile := filepath.Join(stateDir, fmt.Sprintf(StateFileNamePattern, DefaultPort))
	if settings.StateFile() != wantStateFile {
		t.Fatalf("stateFile = %q, want %q", settings.StateFile(), wantStateFile)
	}
	if settings.LockFile() != filepath.Join(stateDir, LockFileName) {
		t.Fatalf("lockFile = %q", settings.LockFile())
	}
	home, err := paths.Home()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	if settings.RepoDir != filepath.Join(home, DefaultRepoDirName) {
		t.Fatalf("repoDir = %q, want the checkout under the home directory", settings.RepoDir)
	}
	// Resolving settings must not create state.
	if paths.Exists(stateDir) {
		t.Fatalf("Load created %s", stateDir)
	}
}

// TestPrecedence pins flag > environment > file > default for every source.
func TestPrecedence(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(stateDir, "config.json")
	fromFile := map[string]any{"port": 1111, "repoDir": filepath.Join(fixtureHome(), "from", "file"), "nodeVersion": "20.0.0"}
	data, err := json.Marshal(fromFile)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	environment := env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
		paths.EnvPort:        "2222",
		paths.EnvRepoDir:     filepath.Join(fixtureHome(), "from", "env"),
		paths.EnvNodeVersion: "21.0.0",
	}

	fromEnv, err := Load(environment.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if fromEnv.Port != 2222 || fromEnv.RepoDir != filepath.Join(fixtureHome(), "from", "env") {
		t.Fatalf("environment must win over the file: %+v", fromEnv)
	}
	// The Node release follows the same order as every other setting; the P rows
	// in config_nodeversion_test.go pin it case by case.
	if fromEnv.NodeVersion != "21.0.0" || fromEnv.Sources.NodeVersion != "env" {
		t.Fatalf("nodeVersion = %q from %q, want the environment's value",
			fromEnv.NodeVersion, fromEnv.Sources.NodeVersion)
	}
	if fromEnv.Sources.Port != "env" || fromEnv.Sources.RepoDir != "env" {
		t.Fatalf("sources = %+v, want env", fromEnv.Sources)
	}

	port := 3333
	repoDir := filepath.Join(fixtureHome(), "from", "flag")
	nodeVersion := "22.0.0"
	fromFlag, err := Load(environment.Getenv, Overrides{Port: &port, RepoDir: &repoDir, NodeVersion: &nodeVersion})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if fromFlag.Port != 3333 || fromFlag.RepoDir != filepath.Join(fixtureHome(), "from", "flag") || fromFlag.NodeVersion != "22.0.0" {
		t.Fatalf("flags must win over the environment: %+v", fromFlag)
	}
	if fromFlag.Sources.Port != "flag" || fromFlag.Sources.RepoDir != "flag" || fromFlag.Sources.NodeVersion != "flag" {
		t.Fatalf("sources = %+v, want flag", fromFlag.Sources)
	}

	// With neither environment nor flag the file wins.
	plain, err := Load(env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if plain.Port != 1111 || plain.RepoDir != filepath.Join(fixtureHome(), "from", "file") || plain.NodeVersion != "20.0.0" {
		t.Fatalf("file values must survive: %+v", plain)
	}
	if plain.Sources.Port != "file" || plain.Sources.RepoDir != "file" {
		t.Fatalf("sources = %+v, want file", plain.Sources)
	}
}

// TestFileTuningValuesAreHonoured pins the tunable half of the document.
func TestFileTuningValuesAreHonoured(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	document := `{
  "startTimeoutSeconds": 12,
  "stopTimeoutSeconds": 3,
  "lockTimeoutSeconds": 4,
  "logRotateBytes": 262144
}`
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(document), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	settings, err := Load(env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.StartTimeout != 12*time.Second || settings.StopTimeout != 3*time.Second || settings.LockTimeout != 4*time.Second {
		t.Fatalf("timeouts = %s/%s/%s", settings.StartTimeout, settings.StopTimeout, settings.LockTimeout)
	}
	if settings.LogRotateBytes != 262144 {
		t.Fatalf("logRotateBytes = %d", settings.LogRotateBytes)
	}
}

// TestRepoDirExpandsTildeLikeEveryOtherPath pins the consistency of path
// handling. repoDir was the one path that bypassed paths.Resolve, so `~/...`
// worked for the state directory, the log and the config file but was a hard
// error for the checkout — a difference nobody could guess.
func TestRepoDirExpandsTildeLikeEveryOtherPath(t *testing.T) {
	home, err := paths.Home()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	want := filepath.Join(home, "projects", "deepseek-harness")

	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	base := env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}

	// From the environment.
	fromEnv, err := Load(func(key string) string {
		if key == paths.EnvRepoDir {
			return "~/projects/deepseek-harness"
		}
		return base[key]
	}, Overrides{})
	if err != nil {
		t.Fatalf("Load with ~ in the environment: %v", err)
	}
	if fromEnv.RepoDir != want {
		t.Fatalf("repoDir = %q, want %q", fromEnv.RepoDir, want)
	}

	// From the command line.
	flagValue := "~/projects/deepseek-harness"
	fromFlag, err := Load(base.Getenv, Overrides{RepoDir: &flagValue})
	if err != nil {
		t.Fatalf("Load with ~ in a flag: %v", err)
	}
	if fromFlag.RepoDir != want {
		t.Fatalf("repoDir = %q, want %q", fromFlag.RepoDir, want)
	}

	// From the config file.
	document := `{"repoDir": "~/projects/deepseek-harness"}`
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(document), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	fromFile, err := Load(base.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load with ~ in the file: %v", err)
	}
	if fromFile.RepoDir != want {
		t.Fatalf("repoDir = %q, want %q", fromFile.RepoDir, want)
	}

	// A relative value is still refused, from every source.
	relative := "projects/deepseek-harness"
	if _, err := Load(base.Getenv, Overrides{RepoDir: &relative}); err == nil {
		t.Fatal("a relative repoDir must be refused")
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(`{"repoDir": "rel/path"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(base.Getenv, Overrides{}); err == nil {
		t.Fatal("a relative repoDir in the file must be refused")
	}
}

// TestAnEmptyFileMeansDefaults pins that an empty document is legal.
func TestAnEmptyFileMeansDefaults(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), nil, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	settings, err := Load(env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("an empty file means all defaults: %v", err)
	}
	if settings.Port != DefaultPort {
		t.Fatalf("port = %d", settings.Port)
	}
}

// TestLoadRejectsBadInput pins that every malformed document fails with a usage
// code and a message that names the field.
func TestLoadRejectsBadInput(t *testing.T) {
	cases := []struct {
		name        string
		document    string
		environment env
		wantSubstr  string
	}{
		{"unknown field", `{"port": 3080, "nope": true}`, nil, "nope"},
		{"malformed json", `{"port": `, nil, "could not be parsed"},
		{"trailing content", `{"port": 3080} {"port": 1}`, nil, "has content after its first JSON value"},
		{"trailing garbage", `{"port": 3080} NOT JSON`, nil, "has content after its first JSON value"},
		{"relative repo", `{"repoDir": "relative/path"}`, nil, "repoDir"},
		{"port above range", `{"port": 70000}`, nil, "port must be between"},
		{"port zero", `{"port": 0}`, nil, "port must be between"},
		{"negative port", `{"port": -1}`, nil, "port must be between"},
		{"zero start timeout", `{"startTimeoutSeconds": 0}`, nil, "startTimeoutSeconds must be at least 1 second"},
		{"negative stop timeout", `{"stopTimeoutSeconds": -5}`, nil, "stopTimeoutSeconds must be at least 1 second"},
		{"lock timeout above max", `{"lockTimeoutSeconds": 90000}`, nil, "lockTimeoutSeconds cannot exceed"},
		{"huge lock timeout", `{"lockTimeoutSeconds": 9223372037}`, nil, "lockTimeoutSeconds"},
		{"negative rotation", `{"logRotateBytes": -1}`, nil, "logRotateBytes cannot be negative"},
		{"tiny rotation", `{"logRotateBytes": 10}`, nil, "logRotateBytes cannot be smaller than"},
		{"bad env port", `{}`, env{paths.EnvPort: "abc"}, "is not a number"},
		{"env port above range", `{}`, env{paths.EnvPort: "70000"}, "port must be between"},
		{"relative state dir", `{}`, env{paths.EnvStateDir: "relstate"}, "a path must be absolute or start with ~"},
		{"relative log file", `{}`, env{paths.EnvLogFile: "logs/dsh.log"}, "DSH_LOG_FILE"},
		{"tilde user log file", `{}`, env{paths.EnvLogFile: "~someone/dsh.log"}, "DSH_LOG_FILE"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			stateDir := filepath.Join(root, "state")
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if testCase.document != "" {
				if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(testCase.document), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}
			environment := env{
				paths.EnvHarnessHome: filepath.Join(root, "harness"),
				paths.EnvStateDir:    stateDir,
			}
			for key, value := range testCase.environment {
				environment[key] = value
			}
			_, err := Load(environment.Getenv, Overrides{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if code := exitcode.Of(err); code != exitcode.Usage {
				t.Fatalf("exit code = %d (%v), want %d", code, err, exitcode.Usage)
			}
			if !strings.Contains(err.Error(), testCase.wantSubstr) {
				t.Fatalf("error = %q, want it to name the field with %q", err, testCase.wantSubstr)
			}
		})
	}
}

// TestLoadRejectsANonRegularConfigFile pins the guard that keeps a device or a
// directory at the config path from hanging a reporting command: whatever the
// path resolves to must be a regular file of sane size before it is read.
func TestLoadRejectsANonRegularConfigFile(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	load := func() error {
		_, err := Load(env{
			paths.EnvHarnessHome: filepath.Join(root, "harness"),
			paths.EnvStateDir:    stateDir,
		}.Getenv, Overrides{})
		return err
	}

	// A directory at the config path.
	if err := os.MkdirAll(filepath.Join(stateDir, "config.json"), 0o700); err != nil {
		t.Fatalf("mkdir config.json: %v", err)
	}
	err := load()
	if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Fatalf("Load with a directory config = %v, want a non-regular report", err)
	}
	// A file that cannot be read is a failure, not a malformed value: exit 2
	// means "the operator wrote something invalid", and this is not that.
	if code := exitcode.Of(err); code != exitcode.Failure {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Failure)
	}
	if err := os.RemoveAll(filepath.Join(stateDir, "config.json")); err != nil {
		t.Fatalf("remove directory config: %v", err)
	}

	// An oversized document is refused before it is read into memory.
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), bytes.Repeat([]byte(" "), maxConfigBytes+1), 0o600); err != nil {
		t.Fatalf("write oversized config: %v", err)
	}
	err = load()
	if err == nil || !strings.Contains(err.Error(), "is too large (") {
		t.Fatalf("Load with an oversized config = %v, want a size report", err)
	}
	if code := exitcode.Of(err); code != exitcode.Failure {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Failure)
	}
}

// TestNullFieldKeepsTheDefault pins that an explicit JSON null is "unset" rather
// than a zero value.
func TestNullFieldKeepsTheDefault(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(`{"port": null, "nodeVersion": null}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	settings, err := Load(env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port != DefaultPort || settings.NodeVersion != "" {
		t.Fatalf("settings = %+v, want the defaults", settings)
	}
}

// TestProvisionWritesDefaultsOnlyOnce pins that the generated document records
// built-in defaults and never overwrites an edited file.
func TestProvisionWritesDefaultsOnlyOnce(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	environment := env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
		paths.EnvPort:        "4444",
	}
	settings, err := Load(environment.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := settings.Provision(); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	data, err := os.ReadFile(settings.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("generated config is not valid JSON: %v\n%s", err, data)
	}
	if written["port"] != float64(DefaultPort) {
		t.Fatalf("generated port = %v, want the default %d (an override must not be frozen)", written["port"], DefaultPort)
	}
	if strings.Contains(string(data), "4444") {
		t.Fatalf("the generated document froze an override:\n%s", data)
	}

	// A second provision leaves an edited file alone.
	if err := os.WriteFile(settings.ConfigPath, []byte(`{"port": 1234}`), 0o600); err != nil {
		t.Fatalf("edit config: %v", err)
	}
	if err := settings.Provision(); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	again, err := os.ReadFile(settings.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(again), "1234") {
		t.Fatalf("Provision overwrote an existing file:\n%s", again)
	}
}

// TestProvisionDoesNotFollowADanglingSymlink pins that generating the config
// cannot write outside the state directory.
func TestProvisionDoesNotFollowADanglingSymlink(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	outside := filepath.Join(root, "outside.json")
	if err := os.Symlink(outside, filepath.Join(stateDir, "config.json")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	settings, err := Load(env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := settings.Provision(); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if paths.Exists(outside) {
		t.Fatal("provisioning followed a symlink out of the state directory")
	}
	if !paths.IsRegularFile(filepath.Join(stateDir, "config.json")) {
		t.Fatal("the dangling symlink was not replaced by a real settings document")
	}
}

// TestEncodeRoundTrips pins that the document this package writes is one it can
// read back.
func TestEncodeRoundTrips(t *testing.T) {
	settings := Default(fixtureHome())
	settings.Port = 4123
	settings.LogRotateBytes = 1 << 20
	settings.StartTimeout = 45 * time.Second
	data, err := Encode(settings)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("Encode produced invalid JSON:\n%s", data)
	}

	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := Load(env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Port != settings.Port || loaded.LogRotateBytes != settings.LogRotateBytes ||
		loaded.StartTimeout != settings.StartTimeout || loaded.RepoDir != settings.RepoDir {
		t.Fatalf("round trip changed the settings:\n got %+v\nwant %+v", loaded, settings)
	}
}

// TestDescribeNamesEverySource pins the -v output.
func TestDescribeNamesEverySource(t *testing.T) {
	settings := Default(fixtureHome())
	settings.StateDir = "/state"
	settings.ConfigPath = "/state/config.json"
	settings.LogPath = "/state/dsh-web.log"
	lines := settings.Describe()
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"settings document", "state directory", "checkout", "port", "Node version", "log file: ", "start timeout", "stop timeout", "lock timeout", "log rotation: "} {
		if !strings.Contains(joined, want) {
			t.Fatalf("verbose output is missing %q:\n%s", want, joined)
		}
	}
}

// TestStateSelectionNamesTheInstancesACommandActsOn pins the model every
// multi-instance command rests on: without a named port the selection is the
// configured port plus every record the state directory holds, the configured
// port leads whatever its number, and a name that matches the record pattern but
// carries no usable port is skipped rather than allowed to break the whole
// directory.
func TestStateSelectionNamesTheInstancesACommandActsOn(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(stateDir, name), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write(fmt.Sprintf(StateFileNamePattern, 3081))
	write(fmt.Sprintf(StateFileNamePattern, 4123))
	// A record for the configured port, a name no build wrote, and a file that
	// only looks like a record: the first must not be duplicated, the other two
	// must not be acted on.
	write(fmt.Sprintf(StateFileNamePattern, 3080))
	write("dsh-web-abc.state.json")
	write("dsh-web-999999.state.json")
	write("notes.txt")

	cases := []struct {
		name     string
		port     int
		source   string
		want     []int
		explicit bool
	}{
		{"configured port only", 3080, "file", []int{3080, 3081, 4123}, false},
		{"no record for the configured port", 3999, "file", []int{3999, 3080, 3081, 4123}, false},
		// The first port is the instance a report answers for: the exit code of
		// `status`, the `stop` result an operator reads first. A configured port
		// that happens to sit above the recorded ones must still lead, or the
		// command silently becomes about whichever server has the lowest number.
		{"configured port above every record", 60000, "file", []int{60000, 3080, 3081, 4123}, false},
		{"port from the flag", 3080, "flag", []int{3080}, true},
		{"port from the environment", 3081, "env", []int{3081}, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			settings := Default(t.TempDir())
			settings.StateDir = stateDir
			settings.Port = testCase.port
			settings.Sources.Port = testCase.source

			selection, err := settings.StateSelection()
			if err != nil {
				t.Fatalf("StateSelection: %v", err)
			}
			if !equalPorts(selection.Ports, testCase.want) {
				t.Fatalf("ports = %v, want %v", selection.Ports, testCase.want)
			}
			if selection.Explicit != testCase.explicit {
				t.Fatalf("Explicit = %v, want %v", selection.Explicit, testCase.explicit)
			}
			if selection.All() == testCase.explicit {
				t.Fatalf("All() = %v with Explicit = %v", selection.All(), selection.Explicit)
			}
		})
	}
}

// TestStateSelectionFailsWhenTheDirectoryCannotBeSearched pins that a state
// directory the process may not read is an error rather than an empty selection:
// "I could not look" must never become "there is nothing here", because the
// commands that ask are the ones that end servers and rewrite checkouts.
func TestStateSelectionFailsWhenTheDirectoryCannotBeSearched(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a directory that cannot be read is a Unix permission, and root ignores it")
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(stateDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })

	settings := Default(t.TempDir())
	settings.StateDir = stateDir
	if _, err := settings.StateSelection(); err == nil {
		t.Fatal("an unreadable state directory produced a selection")
	}
}

// equalPorts reports whether two port lists are the same list.
func equalPorts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// TestStateFileGlobFindsEveryPortsRecord is the regression test for a guard that
// silently saw nothing: the glob used to be built by replacing the first "0" in
// the whole joined path, so a state directory containing a digit (…/001/state,
// which a temp directory often is) produced a pattern that matched no record at
// all — and a cross-port guard that sees no other server lets an update rewrite
// the checkout underneath one.
func TestStateFileGlobFindsEveryPortsRecord(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "001-state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, port := range []int{3080, 4123} {
		name := filepath.Join(stateDir, fmt.Sprintf(StateFileNamePattern, port))
		if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write record: %v", err)
		}
	}

	matches, err := filepath.Glob(StateFileGlob(stateDir))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("glob %q matched %v, want both ports' records", StateFileGlob(stateDir), matches)
	}
}

// TestQuoteGlobMatchesTheLiteralName pins the quoting the record glob rests on
// against the platform's own matcher, which is what the pattern is fed to.
//
// A state directory is an ordinary path, so any character a file name may hold
// can appear in it — and one that a glob reads as syntax would make the guard
// match nothing, which it reports as "no other server". The spelling differs per
// platform (a Windows pattern has no escape character), so the check runs through
// filepath.Match everywhere: this is also the only place the Windows spelling is
// executed on a Windows machine rather than merely compiled.
func TestQuoteGlobMatchesTheLiteralName(t *testing.T) {
	names := []string{"state", "state[1]", "state]x", "[unclosed", "state*all", "state?x", "state%dx", "plain directory"}
	if runtime.GOOS != "windows" {
		// A backslash is a legal file-name character everywhere except Windows,
		// where it is the path separator.
		names = append(names, `back\slash`)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			pattern := quoteGlob(name)
			matched, err := filepath.Match(pattern, name)
			if err != nil {
				t.Fatalf("quoteGlob(%q) = %q, which is not a valid pattern: %v", name, pattern, err)
			}
			if !matched {
				t.Fatalf("quoteGlob(%q) = %q, which does not match the literal name", name, pattern)
			}
		})
	}
}

// TestHelpersDerivePathsFromSettings pins the accessors the lifecycle uses.
func TestHelpersDerivePathsFromSettings(t *testing.T) {
	settings := Default(fixtureHome())
	settings.RepoDir = "/repo"
	settings.StateDir = "/state"
	if settings.URL() != "http://127.0.0.1:3080" {
		t.Fatalf("URL = %q", settings.URL())
	}
	// The expected values are derived with filepath.Join: the separator is the
	// platform's, and a forward-slash literal would only match on Unix.
	if got := settings.StateFile(); got != filepath.Join("/state", "dsh-web-3080.state.json") {
		t.Fatalf("StateFile = %q", got)
	}
	if got := settings.LockFile(); got != filepath.Join("/state", LockFileName) {
		t.Fatalf("LockFile = %q", got)
	}
}

// TestResolveErrorsAreNotUsageErrors pins that a broken home directory is
// reported as itself rather than as a bad setting.
func TestResolveErrorsAreNotUsageErrors(t *testing.T) {
	if _, err := paths.Resolve(""); !errors.Is(err, paths.ErrNotAbsolute) {
		t.Fatalf("Resolve(\"\") = %v, want ErrNotAbsolute", err)
	}
}

// TestProvisionRefusesADirectoryAtTheConfigPath pins the guard that keeps
// provisioning from reporting success while every later load fails: a directory
// cannot become a settings document, and the operator is told so at the moment
// the document would have been written.
func TestProvisionRefusesADirectoryAtTheConfigPath(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	configPath := filepath.Join(stateDir, "config.json")
	if err := os.MkdirAll(configPath, 0o700); err != nil {
		t.Fatalf("mkdir config path: %v", err)
	}

	settings := Default(root)
	settings.StateDir = stateDir
	settings.ConfigPath = configPath
	err := settings.Provision()
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("Provision = %v, want a directory report", err)
	}
	info, statErr := os.Stat(configPath)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("the directory at the config path was disturbed: %v", statErr)
	}
}

// TestTheConfigPathFlagIsLayeredAndNamed pins that --config is a layer like
// every other path value — flag, then environment, then the default — and that
// the verbose output names the layer that supplied it.
//
// It used to be applied by rewriting the environment lookup, so a --config value
// was reported as coming from DSHCTL_CONFIG: the one question `-v` exists to
// answer ("which layer set this?") got the wrong answer.
func TestTheConfigPathFlagIsLayeredAndNamed(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, "from-env.json")
	flagPath := filepath.Join(root, "from-flag.json")
	environment := env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    filepath.Join(root, "state"),
		paths.EnvConfigFile:  envPath,
	}

	fromEnv, err := Load(environment.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if fromEnv.ConfigPath != envPath || fromEnv.Sources.ConfigPath != "env "+paths.EnvConfigFile {
		t.Fatalf("config path = %q from %q, want the environment's path",
			fromEnv.ConfigPath, fromEnv.Sources.ConfigPath)
	}

	fromFlag, err := Load(environment.Getenv, Overrides{ConfigPath: &flagPath})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if fromFlag.ConfigPath != flagPath || fromFlag.Sources.ConfigPath != "flag" {
		t.Fatalf("config path = %q from %q, want the flag's path attributed to the flag",
			fromFlag.ConfigPath, fromFlag.Sources.ConfigPath)
	}

	relative := filepath.Join("relative", "config.json")
	if _, err := Load(environment.Getenv, Overrides{ConfigPath: &relative}); err == nil {
		t.Fatal("a relative --config must be refused")
	} else if !strings.Contains(err.Error(), "--config") {
		t.Fatalf("error = %v, want it to name the flag", err)
	}
}

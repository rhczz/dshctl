package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/paths"
)

// configDocument writes a settings document into a fresh state directory and
// returns the environment that points at it.
func configDocument(t *testing.T, document string) (env, string) {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, ConfigFileName), []byte(document), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}, stateDir
}

// TestLoadRejectsWrongJSONTypes pins what happens when a document is valid JSON
// but the wrong shape: a string where a port belongs, a fraction where an
// integer belongs, a top-level array instead of an object.
//
// Every one of them must come back as a usage error that names the field, so the
// operator is told which line to fix instead of being handed a confusing zero
// value. The parser refuses to coerce: a port of "3080" is a mistake, not a
// request for port 3080.
func TestLoadRejectsWrongJSONTypes(t *testing.T) {
	cases := []struct {
		name       string
		document   string
		wantSubstr string
	}{
		{"port as a string", `{"port": "3080"}`, "port"},
		{"port as a fraction", `{"port": 3080.5}`, "port"},
		{"port as a boolean", `{"port": true}`, "port"},
		{"repoDir as a number", `{"repoDir": 123}`, "repoDir"},
		{"logRotateBytes as a size string", `{"logRotateBytes": "1MB"}`, "logRotateBytes"},
		{"startTimeoutSeconds as a string", `{"startTimeoutSeconds": "30"}`, "startTimeoutSeconds"},
		{"a top-level array", `[1,2]`, "could not be parsed"},
		{"a top-level number", `5`, "could not be parsed"},
		{"a top-level string", `"x"`, "could not be parsed"},
		{"a byte order mark", "\xef\xbb\xbf" + `{"port": 3080}`, "could not be parsed"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			environment, _ := configDocument(t, testCase.document)
			settings, err := Load(environment.Getenv, Overrides{})
			if err == nil {
				t.Fatalf("Load accepted %s and produced %+v", testCase.document, settings)
			}
			if code := exitcode.Of(err); code != exitcode.Usage {
				t.Fatalf("exit code = %d (%v), want %d", code, err, exitcode.Usage)
			}
			if !strings.Contains(err.Error(), testCase.wantSubstr) {
				t.Fatalf("error = %q, want it to name %q", err, testCase.wantSubstr)
			}
		})
	}
}

// TestDuplicateJSONKeysKeepTheLastValue pins the parser's answer for a document
// that names the same setting twice. JSON leaves it to the implementation; Go
// keeps the last occurrence, so a hand-edited file whose stale line was left
// above the new one silently uses the new one.
func TestDuplicateJSONKeysKeepTheLastValue(t *testing.T) {
	environment, _ := configDocument(t, `{"port": 1, "port": 2}`)
	settings, err := Load(environment.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port != 2 {
		t.Fatalf("port = %d, want 2: the last key wins", settings.Port)
	}
}

// TestTopLevelNullMeansDefaults pins that a document holding only "null" is the
// same as an empty one. An operator who comments out the whole file, or whose
// generator writes null when it has nothing to say, must get a working
// configuration rather than a parse error.
func TestTopLevelNullMeansDefaults(t *testing.T) {
	environment, stateDir := configDocument(t, "null")
	settings, err := Load(environment.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port != DefaultPort || settings.NodeVersion != "" ||
		settings.StartTimeout != DefaultStartTimeout || settings.LogRotateBytes != DefaultLogRotateBytes {
		t.Fatalf("settings = %+v, want every default", settings)
	}
	if settings.Sources.Port != "default" || settings.Sources.RepoDir != "default" {
		t.Fatalf("sources = %+v, want every value attributed to the defaults", settings.Sources)
	}
	if settings.LogPath != filepath.Join(stateDir, DefaultLogFileName) {
		t.Fatalf("logPath = %q", settings.LogPath)
	}
}

// TestValidateNamesEveryFieldItRejects drives Validate directly on hand-built
// settings.
//
// Load can only reach these branches through a document or an environment that
// happens to produce them; a caller that assembles Settings itself — the service
// layer does — gets the same checks, and each message must name the field.
func TestValidateNamesEveryFieldItRejects(t *testing.T) {
	valid := func() Settings {
		settings := Default(fixtureHome())
		settings.StateDir = filepath.Join(fixtureHome(), "state")
		settings.LogPath = filepath.Join(fixtureHome(), "state", "dsh-web.log")
		return settings
	}
	cases := []struct {
		name       string
		mutate     func(*Settings)
		wantSubstr string
	}{
		{"an empty repoDir", func(s *Settings) { s.RepoDir = "" }, "repoDir cannot be empty"},
		{"a whitespace repoDir", func(s *Settings) { s.RepoDir = "   " }, "repoDir cannot be empty"},
		{"a relative repoDir", func(s *Settings) { s.RepoDir = "repo" }, "repoDir must be an absolute path"},
		{"a port below the range", func(s *Settings) { s.Port = MinPort - 1 }, "port must be between"},
		{"a port above the range", func(s *Settings) { s.Port = MaxPort + 1 }, "port must be between"},
		{"a zero start timeout", func(s *Settings) { s.StartTimeout = 0 }, "startTimeoutSeconds must be at least 1 second"},
		{"a stop timeout above the maximum", func(s *Settings) { s.StopTimeout = MaxTimeoutSeconds*time.Second + time.Second }, "stopTimeoutSeconds cannot exceed"},
		{"a lock timeout above the maximum", func(s *Settings) { s.LockTimeout = MaxTimeoutSeconds*time.Second + time.Second }, "lockTimeoutSeconds cannot exceed"},
		{"a negative rotation threshold", func(s *Settings) { s.LogRotateBytes = -1 }, "logRotateBytes cannot be negative"},
		{"a rotation threshold below the minimum", func(s *Settings) { s.LogRotateBytes = MinRotateBytes - 1 }, "logRotateBytes cannot be smaller than"},
		{"an empty stateDir", func(s *Settings) { s.StateDir = "" }, "the state directory must be an absolute path"},
		{"a non-absolute stateDir", func(s *Settings) { s.StateDir = "state" }, "the state directory must be an absolute path"},
		{"an empty logPath", func(s *Settings) { s.LogPath = "" }, "the log file must be an absolute path"},
		{"a non-absolute logPath", func(s *Settings) { s.LogPath = "dsh-web.log" }, "the log file must be an absolute path"},
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("the reference settings must validate: %v", err)
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			settings := valid()
			testCase.mutate(&settings)
			err := settings.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %+v", settings)
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

// TestValidateRangesIncludeTheirBoundaries pins the exact edge of every bounded
// setting.
//
// Each one is a value an operator can legitimately write: the smallest rotation
// threshold, the longest timeout the code documents, and the largest int64 for a
// rotation threshold (a sentinel meaning "never rotate in practice"). An
// off-by-one here rejects a correct document, and a missing upper bound lets a
// timeout overflow into a negative duration.
func TestValidateRangesIncludeTheirBoundaries(t *testing.T) {
	maxInt64 := strconv.FormatInt(math.MaxInt64, 10)
	rotate := func(value int64) *int64 { return &value }
	start := func(value time.Duration) *time.Duration { return &value }
	cases := []struct {
		name       string
		document   string
		wantErr    bool
		wantSubstr string
		// Exactly one of these is set per accepted case: only the field the
		// document names can be asserted, since the others keep their defaults.
		wantRotate *int64
		wantStart  *time.Duration
	}{
		{
			name:       "rotation exactly at the minimum",
			document:   `{"logRotateBytes": 65536}`,
			wantRotate: rotate(MinRotateBytes),
		},
		{
			name:       "rotation one byte below the minimum",
			document:   `{"logRotateBytes": 65535}`,
			wantErr:    true,
			wantSubstr: "logRotateBytes cannot be smaller than",
		},
		{
			name:       "rotation zero, which disables it",
			document:   `{"logRotateBytes": 0}`,
			wantRotate: rotate(0),
		},
		{
			name:       "rotation at the largest int64",
			document:   `{"logRotateBytes": ` + maxInt64 + `}`,
			wantRotate: rotate(math.MaxInt64),
		},
		{
			name:      "start timeout at one second",
			document:  `{"startTimeoutSeconds": 1}`,
			wantStart: start(time.Second),
		},
		{
			name:      "start timeout exactly at the documented maximum",
			document:  `{"startTimeoutSeconds": 86400}`,
			wantStart: start(MaxTimeoutSeconds * time.Second),
		},
		{
			name:       "start timeout one second above the documented maximum",
			document:   `{"startTimeoutSeconds": 86401}`,
			wantErr:    true,
			wantSubstr: "startTimeoutSeconds cannot exceed",
		},
		{
			name:       "a negative start timeout",
			document:   `{"startTimeoutSeconds": -1}`,
			wantErr:    true,
			wantSubstr: "startTimeoutSeconds must be at least 1 second",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			environment, _ := configDocument(t, testCase.document)
			settings, err := Load(environment.Getenv, Overrides{})
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("Load accepted %s and produced %+v", testCase.document, settings)
				}
				if !strings.Contains(err.Error(), testCase.wantSubstr) {
					t.Fatalf("error = %q, want %q", err, testCase.wantSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if testCase.wantRotate != nil && settings.LogRotateBytes != *testCase.wantRotate {
				t.Fatalf("logRotateBytes = %d, want %d", settings.LogRotateBytes, *testCase.wantRotate)
			}
			if testCase.wantStart != nil && settings.StartTimeout != *testCase.wantStart {
				t.Fatalf("startTimeout = %s, want %s", settings.StartTimeout, *testCase.wantStart)
			}
		})
	}
}

// TestProvisionCreatesTheConfigDirectory pins that provisioning creates the
// directory the configured document lives in, even when that is nowhere near the
// state directory: DSHCTL_CONFIG may point at a shared or checked-in location,
// and a config file that cannot be created where it was asked for is a failure
// the first command would otherwise hit later.
func TestProvisionCreatesTheConfigDirectory(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	configPath := filepath.Join(root, "new", "dir", ConfigFileName)

	settings := Default(root)
	settings.StateDir = stateDir
	settings.ConfigPath = configPath
	if err := settings.Provision(); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	dirInfo, err := os.Stat(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("stat config directory: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Fatalf("config directory mode = %v, want a directory", dirInfo.Mode())
	}
	fileInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if !fileInfo.Mode().IsRegular() {
		t.Fatalf("config path is not a regular file: %v", fileInfo.Mode())
	}
	// The 0700/0600 permission bits are a Unix concept: Windows reports 0777
	// for a directory and 0666 for a writable file whatever was asked for, so
	// the exact modes are pinned in the unix-tagged file instead of here.
	if runtime.GOOS != "windows" {
		if dirInfo.Mode().Perm() != 0o700 {
			t.Fatalf("config directory mode = %v, want 0700", dirInfo.Mode())
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Fatalf("config file mode = %v, want 0600", fileInfo.Mode())
		}
	}
	if !paths.IsDir(stateDir) {
		t.Fatal("provisioning skipped the state directory")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("the generated document is not valid JSON: %v\n%s", err, data)
	}
	if document["port"] != float64(DefaultPort) {
		t.Fatalf("generated port = %v, want the default %d", document["port"], DefaultPort)
	}
}

// TestEncodeOmitsAnEmptyRepoDir pins the one optional field of the generated
// document: an empty RepoDir is left out rather than written as "", because a
// document that carries an empty path would fail validation on the next load
// even though the value it replaced was perfectly usable.
func TestEncodeOmitsAnEmptyRepoDir(t *testing.T) {
	settings := Default(fixtureHome())
	settings.RepoDir = ""
	data, err := Encode(settings)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(data), "repoDir") {
		t.Fatalf("an empty repoDir was written to the document:\n%s", data)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("Encode produced invalid JSON: %v\n%s", err, data)
	}
	if _, present := document["repoDir"]; present {
		t.Fatalf("document = %v, want no repoDir key", document)
	}
	// Every setting that has a value is still recorded, so the document stays a
	// complete description of the tunable surface. The Node release is absent
	// while it is undetermined; TestEncodeWritesTheReleaseOnlyWhenItIsDetermined
	// pins both halves of that.
	for _, key := range []string{"port", "startTimeoutSeconds", "stopTimeoutSeconds", "lockTimeoutSeconds", "logRotateBytes"} {
		if _, present := document[key]; !present {
			t.Fatalf("document = %v, want it to record %q", document, key)
		}
	}
}

// TestDescribeOfAZeroSettingsIsStillWellFormed pins the verbose output for the
// degenerate value: nothing resolved, nothing sourced.
//
// The lines are read by operators and by scripts, so the shape has to hold even
// when the settings were never loaded — no missing label, no panic on a zero
// duration, and an empty source rendered as an empty parenthesis rather than as
// a missing one. Every line carries a source, the numeric ones included: a value
// whose layer cannot be seen is a value the reader has to guess about.
func TestDescribeOfAZeroSettingsIsStillWellFormed(t *testing.T) {
	var settings Settings
	want := []string{
		"settings document:  ()",
		"state directory:  ()",
		"checkout:  ()",
		"port: 0 ()",
		"Node version: (not determined; resolved from PATH at start) ()",
		"log file:  ()",
		"start timeout: 0s ()",
		"stop timeout: 0s ()",
		"lock timeout: 0s ()",
		"log rotation: 0 bytes (0 disables) ()",
		"log level: unset ()",
	}
	got := settings.Describe()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Describe =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestLoadRejectsARelativeConfigPath pins that the config file itself is held to
// the same rule as every other path: it must be absolute or start with ~.
//
// A relative --config or DSHCTL_CONFIG would be resolved against whatever
// directory dshctl happens to run in, so the file that configures the operation
// would depend on the caller's working directory. The refusal is a usage error,
// because the operator's argument is what is wrong.
func TestLoadRejectsARelativeConfigPath(t *testing.T) {
	root := t.TempDir()
	environment := env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    filepath.Join(root, "state"),
		paths.EnvConfigFile:  filepath.Join("relative", "config.json"),
	}
	_, err := Load(environment.Getenv, Overrides{})
	if err == nil {
		t.Fatal("a relative config path must be refused")
	}
	if code := exitcode.Of(err); code != exitcode.Usage {
		t.Fatalf("exit code = %d (%v), want %d", code, err, exitcode.Usage)
	}
	// The message quotes the value with %q, so the platform's separator is
	// escaped there; comparing against the same rendering keeps the assertion
	// about "the offending value is named" on every platform.
	if !strings.Contains(err.Error(), fmt.Sprintf("%q", filepath.Join("relative", "config.json"))) {
		t.Fatalf("error = %q, want it to quote the offending value", err)
	}
}

// TestNodeVersionEnvironmentWhitespace pins how DSH_NODE_VERSION treats the
// spaces around its value.
//
// A value of spaces alone is not a release and is ignored, so the setting stays
// undetermined and the next start resolves one from PATH. A padded value is
// trimmed at the boundary rather than stored verbatim: the release it names is
// what every later comparison and the written-back document must carry.
func TestNodeVersionEnvironmentWhitespace(t *testing.T) {
	environment, _ := configDocument(t, "")

	environment[paths.EnvNodeVersion] = "   "
	settings, err := Load(environment.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load with a whitespace-only node version: %v", err)
	}
	if settings.NodeVersion != "" {
		t.Fatalf("nodeVersion = %q, want it undetermined", settings.NodeVersion)
	}
	if settings.Sources.NodeVersion != "default" {
		t.Fatalf("nodeVersion source = %q, want default", settings.Sources.NodeVersion)
	}

	environment[paths.EnvNodeVersion] = "  24.20.0  "
	settings, err = Load(environment.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load with a padded node version: %v", err)
	}
	if settings.Sources.NodeVersion != "env" {
		t.Fatalf("nodeVersion source = %q, want env", settings.Sources.NodeVersion)
	}
	if settings.NodeVersion != "24.20.0" {
		t.Fatalf("nodeVersion = %q, want the trimmed release", settings.NodeVersion)
	}
}

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/paths"
)

// Every setting is resolved from the same four layers, and `-v` reports which
// one supplied each value. The rows below drive all of them at once, because the
// failure this guards against is not a wrong value — it is a setting that quietly
// stops participating in the order: a layer that only ever applies to some keys,
// or a source label that names the wrong layer for the one question `-v` exists
// to answer.
//
// The settings that have no flag or no environment variable are part of the
// table on purpose. "This one is file-only" is a decision that has to be visible,
// not an accident of which resolver happened to be written.

// sourcesOf renders every resolved setting as "value (layer)".
func sourcesOf(settings Settings) map[string]string {
	seconds := func(d time.Duration) string { return strconv.Itoa(int(d/time.Second)) + "s" }
	return map[string]string{
		"repoDir":             settings.RepoDir + " (" + settings.Sources.RepoDir + ")",
		"port":                strconv.Itoa(settings.Port) + " (" + settings.Sources.Port + ")",
		"nodeVersion":         settings.NodeVersion + " (" + settings.Sources.NodeVersion + ")",
		"startTimeoutSeconds": seconds(settings.StartTimeout) + " (" + settings.Sources.Timeouts + ")",
		"stopTimeoutSeconds":  seconds(settings.StopTimeout) + " (" + settings.Sources.Timeouts + ")",
		"lockTimeoutSeconds":  seconds(settings.LockTimeout) + " (" + settings.Sources.Timeouts + ")",
		"logRotateBytes":      strconv.FormatInt(settings.LogRotateBytes, 10) + " (" + settings.Sources.Timeouts + ")",
		"stateDir":            settings.StateDir + " (" + settings.Sources.StateDir + ")",
		"configPath":          settings.ConfigPath + " (" + settings.Sources.ConfigPath + ")",
		"logPath":             settings.LogPath + " (" + settings.Sources.LogPath + ")",
	}
}

// TestEverySettingIsResolvedFromTheLayersThatOwnIt pins the source of every
// setting under the layer that supplies it.
func TestEverySettingIsResolvedFromTheLayersThatOwnIt(t *testing.T) {
	w := newTestWorld(t)
	document := filepath.Join(w.stateDir, ConfigFileName)
	otherDocument := filepath.Join(w.root, "other-config.json")

	documentFields := map[string]any{
		"repoDir":             w.checkout(),
		"port":                1111,
		"nodeVersion":         "24.19.0",
		"startTimeoutSeconds": 11,
		"stopTimeoutSeconds":  12,
		"lockTimeoutSeconds":  13,
		"logRotateBytes":      131072,
	}
	writeDocumentFields(t, document, documentFields)

	cases := []struct {
		name      string
		vars      env
		overrides Overrides
		want      map[string]string
	}{
		{
			name: "nothing configured",
			vars: env{},
			want: map[string]string{
				"repoDir":             w.guess() + " (default)",
				"port":                strconv.Itoa(DefaultPort) + " (default)",
				"nodeVersion":         " (default)",
				"startTimeoutSeconds": "90s (default)",
				"stopTimeoutSeconds":  "15s (default)",
				"lockTimeoutSeconds":  "10s (default)",
				"logRotateBytes":      strconv.FormatInt(DefaultLogRotateBytes, 10) + " (default)",
				"stateDir":            filepath.Join(w.home, paths.HarnessDirName, paths.StateDirName) + " (default)",
				"configPath":          filepath.Join(w.home, paths.HarnessDirName, paths.StateDirName, ConfigFileName) + " (default)",
				"logPath":             filepath.Join(w.home, paths.HarnessDirName, paths.StateDirName, DefaultLogFileName) + " (default)",
			},
		},
		{
			name: "the document sets every key",
			vars: env{paths.EnvStateDir: w.stateDir},
			want: map[string]string{
				"repoDir":             w.checkout() + " (file)",
				"port":                "1111 (file)",
				"nodeVersion":         "24.19.0 (file)",
				"startTimeoutSeconds": "11s (file)",
				"stopTimeoutSeconds":  "12s (file)",
				"lockTimeoutSeconds":  "13s (file)",
				"logRotateBytes":      "131072 (file)",
			},
		},
		{
			name: "the environment beats the document",
			vars: env{
				paths.EnvStateDir:    w.stateDir,
				paths.EnvRepoDir:     filepath.Join(w.home, "from-env"),
				paths.EnvPort:        "2222",
				paths.EnvNodeVersion: "24.21.0",
				paths.EnvLogFile:     filepath.Join(w.root, "from-env.log"),
			},
			want: map[string]string{
				"repoDir":     filepath.Join(w.home, "from-env") + " (env)",
				"port":        "2222 (env)",
				"nodeVersion": "24.21.0 (env)",
				"stateDir":    w.stateDir + " (env " + paths.EnvStateDir + ")",
				"logPath":     filepath.Join(w.root, "from-env.log") + " (env " + paths.EnvLogFile + ")",
				// The four numeric settings have no environment variable, so the
				// document still supplies them: no layer is skipped by accident.
				"startTimeoutSeconds": "11s (file)",
				"stopTimeoutSeconds":  "12s (file)",
				"lockTimeoutSeconds":  "13s (file)",
				"logRotateBytes":      "131072 (file)",
			},
		},
		{
			name: "the flags beat the environment",
			vars: env{
				paths.EnvStateDir:    w.stateDir,
				paths.EnvRepoDir:     filepath.Join(w.home, "from-env"),
				paths.EnvPort:        "2222",
				paths.EnvNodeVersion: "24.21.0",
			},
			overrides: Overrides{
				RepoDir:     ptr(filepath.Join(w.home, "from-flag")),
				Port:        ptr(3333),
				NodeVersion: ptr("24.20.0"),
			},
			want: map[string]string{
				"repoDir":     filepath.Join(w.home, "from-flag") + " (flag)",
				"port":        "3333 (flag)",
				"nodeVersion": "24.20.0 (flag)",
				"stateDir":    w.stateDir + " (env " + paths.EnvStateDir + ")",
				// Unchanged by the flags: still the document's values.
				"startTimeoutSeconds": "11s (file)",
				"logRotateBytes":      "131072 (file)",
			},
		},
		{
			name: "the environment names another document",
			vars: env{
				paths.EnvStateDir:   w.stateDir,
				paths.EnvConfigFile: otherDocument,
			},
			want: map[string]string{
				// The document the environment names is the one that is read, so
				// its values are the ones in effect.
				"configPath": otherDocument + " (env " + paths.EnvConfigFile + ")",
				"port":       "2222 (file)",
				"repoDir":    w.guess() + " (default)",
			},
		},
		{
			name: "the flag names another document",
			vars: env{paths.EnvStateDir: w.stateDir},
			overrides: Overrides{
				ConfigPath: ptr(otherDocument),
			},
			want: map[string]string{
				"configPath": otherDocument + " (flag)",
				"port":       "2222 (file)",
			},
		},
	}

	// The second document exists for the two rows that name it.
	writeDocumentFields(t, otherDocument, map[string]any{"port": 2222})

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			settings, err := Load(testCase.vars.Getenv, testCase.overrides)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got := sourcesOf(settings)
			for field, want := range testCase.want {
				if got[field] != want {
					t.Errorf("%s = %q, want %q\nall: %v", field, got[field], want, got)
				}
			}
		})
	}
}

// TestTheBuiltInDefaultsAreTheConstants pins the values a fresh installation
// runs with. A default that drifts from the constant it is documented as is
// invisible: every test that sets the value it asserts would keep passing, and
// the promise "works with no configuration at all" would quietly change meaning.
func TestTheBuiltInDefaultsAreTheConstants(t *testing.T) {
	w := newTestWorld(t)
	settings, err := Load(env{}.Getenv, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if DefaultPort != 3080 || settings.Port != DefaultPort {
		t.Fatalf("port = %d, want the documented %d", settings.Port, DefaultPort)
	}
	if DefaultStartTimeout != 90*time.Second || settings.StartTimeout != DefaultStartTimeout {
		t.Fatalf("start timeout = %s, want the documented %s", settings.StartTimeout, DefaultStartTimeout)
	}
	if DefaultStopTimeout != 15*time.Second || settings.StopTimeout != DefaultStopTimeout {
		t.Fatalf("stop timeout = %s, want the documented %s", settings.StopTimeout, DefaultStopTimeout)
	}
	if DefaultLockTimeout != 10*time.Second || settings.LockTimeout != DefaultLockTimeout {
		t.Fatalf("lock timeout = %s, want the documented %s", settings.LockTimeout, DefaultLockTimeout)
	}
	if DefaultLogRotateBytes != 4<<20 || settings.LogRotateBytes != DefaultLogRotateBytes {
		t.Fatalf("log rotation = %d, want the documented %d", settings.LogRotateBytes, DefaultLogRotateBytes)
	}
	if settings.RepoDir != filepath.Join(w.home, DefaultRepoDirName) {
		t.Fatalf("repoDir = %q, want the checkout named by DefaultRepoDirName", settings.RepoDir)
	}
	if MinNodeVersion != "24.12.0" || TestedNodeVersion != "24.20.0" {
		t.Fatalf("node floor/verified = %s/%s, want the documented 24.12.0/24.20.0",
			MinNodeVersion, TestedNodeVersion)
	}
}

// TestTimeoutBoundsAreEnforcedOnEveryKey pins the guards that keep a configured
// duration from silently misbehaving: the same bound applies to all three
// timeouts, in both directions, and the message names the field.
//
// A value near the int64 limit is checked separately from a merely large one:
// the bound exists because time.Duration(seconds)*time.Second overflows into a
// negative duration, and a report that could be either message would leave that
// classification unpinned.
func TestTimeoutBoundsAreEnforcedOnEveryKey(t *testing.T) {
	cases := []struct {
		key        string
		document   string
		wantSubstr string
	}{
		{"start below the floor", `{"startTimeoutSeconds": 0}`, "startTimeoutSeconds must be at least 1 second"},
		{"stop below the floor", `{"stopTimeoutSeconds": 0}`, "stopTimeoutSeconds must be at least 1 second"},
		{"lock below the floor", `{"lockTimeoutSeconds": 0}`, "lockTimeoutSeconds must be at least 1 second"},
		{"start above the ceiling", `{"startTimeoutSeconds": 86401}`, "startTimeoutSeconds cannot exceed 86400 seconds"},
		{"stop above the ceiling", `{"stopTimeoutSeconds": 86401}`, "stopTimeoutSeconds cannot exceed 86400 seconds"},
		{"lock above the ceiling", `{"lockTimeoutSeconds": 86401}`, "lockTimeoutSeconds cannot exceed 86400 seconds"},
		// 9223372037 seconds is beyond what a signed 64-bit nanosecond duration
		// can hold, so this is the overflow the ceiling exists for.
		{"start overflows", `{"startTimeoutSeconds": 9223372037}`, "startTimeoutSeconds cannot exceed 86400 seconds"},
		{"stop overflows", `{"stopTimeoutSeconds": 9223372037}`, "stopTimeoutSeconds cannot exceed 86400 seconds"},
		{"lock overflows", `{"lockTimeoutSeconds": 9223372037}`, "lockTimeoutSeconds cannot exceed 86400 seconds"},
	}
	for _, testCase := range cases {
		t.Run(testCase.key, func(t *testing.T) {
			w := newTestWorld(t)
			w.writeDocument(testCase.document)
			_, err := Load(w.vars.Getenv, Overrides{})
			if err == nil {
				t.Fatalf("%s must be refused", testCase.key)
			}
			if !strings.Contains(err.Error(), testCase.wantSubstr) {
				t.Fatalf("error = %v, want it to say %q", err, testCase.wantSubstr)
			}
		})
	}
}

// TestProvisionReportsAMissingPlatformHome pins that generating the document
// needs a home to render the defaults from: without one there is nothing to
// write, and a document holding a path derived from nowhere is exactly the file
// every later run would honour.
//
// The settings carry the home they were resolved with, so this is the same
// question a load answers, asked of the value the command is actually holding —
// not of the environment, which may have moved since.
func TestProvisionReportsAMissingPlatformHome(t *testing.T) {
	w := newTestWorld(t)
	settings := w.settings()
	settings.Home = ""
	settings.RepoDir = ""

	if err := settings.Provision(); err == nil {
		t.Fatal("provisioning without a platform home must be reported")
	}
	if _, err := os.Stat(settings.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("a refused provisioning must leave no document behind: %v", err)
	}
}

// writeDocumentFields writes a settings document from fields.
func writeDocumentFields(t *testing.T, path string, fields map[string]any) {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ptr returns a pointer to a value, for an override.
func ptr[T any](value T) *T { return &value }

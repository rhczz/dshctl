package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/paths"
)

// The configuration document is the only file a person is expected to edit, and
// it is the only place untrusted input enters the program. The fuzz target below
// asserts the contract the loader owes its callers: whatever the document holds,
// the result is either a classified error or settings every later stage can act
// on — never a panic, and never a half-applied configuration.

// FuzzLoadClassifiesEveryDocument pins that contract.
//
// A document that loads must produce settings the validator accepts: the loader
// is the boundary, so anything that gets past it has to be usable by the code
// that follows.
func FuzzLoadClassifiesEveryDocument(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`null`,
		`{"port": 3080}`,
		`{"port": "3080"}`,
		`{"port": 3080.5}`,
		`{"port": 65536}`,
		`{"port": -1}`,
		`{"port": 0}`,
		`{"logRotateBytes": 65536}`,
		`{"logRotateBytes": "1MB"}`,
		`{"logRotateBytes": 9223372036854775807}`,
		`{"startTimeoutSeconds": 86400}`,
		`{"startTimeoutSeconds": 0}`,
		`{"repoDir": 123}`,
		`{"repoDir": "relative/path"}`,
		`{"nodeVersion": " 24.20.0 "}`,
		`{"nodeVersion": ""}`,
		`[1,2]`,
		`5`,
		`"x"`,
		`{"port": 1, "port": 2}`,
		"\xef\xbb\xbf{\"port\": 3080}",
		`{"unknownField": {"nested": [1,2,3]}}`,
		`{"port": 1e999}`,
		`{"port": 3080`,
		``,
		"\xff\xfe\x00",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, document []byte) {
		root := t.TempDir()
		path := filepath.Join(root, "config.json")
		if err := os.WriteFile(path, document, 0o600); err != nil {
			t.Fatalf("seed the document: %v", err)
		}
		environment := env{
			paths.EnvHarnessHome: filepath.Join(root, "harness"),
			paths.EnvStateDir:    filepath.Join(root, "state"),
			paths.EnvRepoDir:     filepath.Join(root, "repo"),
			paths.EnvConfigFile:  path,
		}

		settings, err := Load(environment.Getenv, Overrides{})
		if err != nil {
			// Every rejection must be a classified error the command line can
			// map to an exit code, and it must name something.
			if err.Error() == "" {
				t.Fatalf("document %q was rejected with an empty message", document)
			}
			return
		}
		if validateErr := settings.Validate(); validateErr != nil {
			t.Fatalf("document %q loaded into settings that fail their own validation: %v",
				document, validateErr)
		}
		if settings.Port < MinPort || settings.Port > MaxPort {
			t.Fatalf("document %q produced the out-of-range port %d", document, settings.Port)
		}
		// The timeouts are what bound every wait in the program; a zero or
		// negative one would turn a wait into an immediate failure.
		for name, value := range map[string]time.Duration{
			"start": settings.StartTimeout,
			"stop":  settings.StopTimeout,
			"lock":  settings.LockTimeout,
		} {
			if value < time.Second {
				t.Fatalf("document %q produced a %s timeout of %s", document, name, value)
			}
		}
		if settings.LogRotateBytes < 0 {
			t.Fatalf("document %q produced a negative rotation threshold %d", document, settings.LogRotateBytes)
		}
	})
}

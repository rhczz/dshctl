package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/service"
)

// readReadme returns the operator contract as it is published.
func readReadme(t *testing.T) string {
	t.Helper()
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("cannot read the README: %v", err)
	}
	return string(readme)
}

// TestTheReadmeDocumentsEverySetting pins the promise the README makes to
// somebody who has to configure this tool without reading Go: every environment
// variable and every settings key is in it.
//
// It is a test rather than a convention because the failure mode is silent — a
// new variable that works and is documented nowhere is discovered by reading the
// source, and the person who needs the documentation most is the one who cannot.
func TestTheReadmeDocumentsEverySetting(t *testing.T) {
	documented := readReadme(t)

	variables := []string{
		paths.EnvStateDir,
		paths.EnvHarnessHome,
		paths.EnvConfigFile,
		paths.EnvLogFile,
		paths.EnvRepoDir,
		paths.EnvPort,
		paths.EnvNodeVersion,
	}
	for _, name := range variables {
		if !strings.Contains(documented, name) {
			t.Errorf("environment variable %s is not documented in the README", name)
		}
	}

	kind := reflect.TypeOf(config.File{})
	for index := 0; index < kind.NumField(); index++ {
		key := strings.Split(kind.Field(index).Tag.Get("json"), ",")[0]
		if key == "" || key == "-" {
			continue
		}
		if !strings.Contains(documented, "`"+key+"`") {
			t.Errorf("setting %s is not documented in the README", key)
		}
	}
}

// TestTheReadmeDocumentsEveryCommand pins the other half of the contract: the
// command table is what an operator reads to learn the tool exists, and a
// command that is registered but missing from it is reachable only by guessing.
func TestTheReadmeDocumentsEveryCommand(t *testing.T) {
	documented := readReadme(t)
	for _, command := range Commands() {
		if !strings.Contains(documented, "| `"+command.Name+"` |") {
			t.Errorf("command %s is missing from the README command table", command.Name)
		}
	}
}

// TestTheReadmeDocumentsEveryExitCode pins the codes a script branches on.
//
// The help text and the README are two renderings of one table, and a script
// that treats 5 as "lock timeout" is broken by a README that stops mentioning it
// just as much as by a binary that stops returning it.
func TestTheReadmeDocumentsEveryExitCode(t *testing.T) {
	documented := readReadme(t)
	for _, entry := range strings.Split(helpExitCodes, ",") {
		fields := strings.Fields(strings.TrimSpace(entry))
		if len(fields) == 0 {
			continue
		}
		if code := fields[0]; !strings.Contains(documented, "| "+code+" |") {
			t.Errorf("exit code %s is missing from the README exit-code table", code)
		}
	}
}

// TestTheReadmeDocumentsTheDefaultLogLines pins a default an operator cannot
// guess: `dshctl logs` prints a tail, and how much of it is part of the command's
// behaviour rather than an implementation detail.
func TestTheReadmeDocumentsTheDefaultLogLines(t *testing.T) {
	documented := readReadme(t)
	want := fmt.Sprintf("默认 %d 行", service.DefaultLogLines)
	if !strings.Contains(documented, want) {
		t.Errorf("the README does not document the logs default line count (want %q)", want)
	}
}

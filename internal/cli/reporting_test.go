package cli

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
)

// The structured output is what a script consumes, so every value in it has to
// be the resolved one rather than a field that merely exists. A JSON document
// that carries the right keys with the wrong values is worse than no document at
// all: the caller cannot tell that it was handed the defaults of another machine.

// TestStatusJSONCarriesTheResolvedValues pins the values of the status document
// against the environment the binary was given.
//
// The whole report is cross-checked from the pieces it names: the state
// directory is where the log is, the checkout is the sibling the harness named,
// and the address is the port it was told to use. A field that was filled from a
// constant — the built-in port, the built-in checkout — fails every one of them.
func TestStatusJSONCarriesTheResolvedValues(t *testing.T) {
	result, stateDir := runBinary(t, "status", "--json")
	if result.code != 3 {
		t.Fatalf("status exit = %d, want 3 for a stopped service (stderr = %s)", result.code, result.stderr)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &document); err != nil {
		t.Fatalf("status --json is not JSON: %v\n%s", err, result.stdout)
	}
	// The instance the command was about is the document itself; `ports` carries
	// the others beside it, so a script that only knows the configured port keeps
	// reading the same keys it always did.
	status, ok := document["status"].(map[string]any)
	if !ok {
		t.Fatalf("status document has no status object: %s", result.stdout)
	}

	root := filepath.Dir(stateDir)
	if got := status["state"]; got != "stopped" {
		t.Fatalf("state = %v, want stopped", got)
	}
	if got := status["repoDir"]; got != filepath.Join(root, "repo") {
		t.Fatalf("repoDir = %v, want the checkout the environment named", got)
	}
	if got := status["logPath"]; got != filepath.Join(stateDir, config.DefaultLogFileName) {
		t.Fatalf("logPath = %v, want the log inside the state directory", got)
	}
	port, ok := status["port"].(float64)
	if !ok || port < config.MinPort || port > config.MaxPort {
		t.Fatalf("port = %v, want a usable port", status["port"])
	}
	if port == float64(config.DefaultPort) {
		t.Fatalf("port = %v, want the port the binary was given rather than the built-in default", status["port"])
	}
	if wantURL := "http://127.0.0.1:" + portString(t, status["port"]); status["url"] != wantURL {
		t.Fatalf("url = %v, want %q", status["url"], wantURL)
	}
	// The list of instances is the same observation, so the one the command was
	// about appears in it exactly once: a report that repeated it, or omitted it,
	// would describe a machine with an instance that does not exist.
	ports, ok := document["ports"].([]any)
	if !ok || len(ports) != 1 {
		t.Fatalf("ports = %v, want exactly the configured instance", document["ports"])
	}
	only, ok := ports[0].(map[string]any)
	if !ok {
		t.Fatalf("ports[0] = %v, want an object", ports[0])
	}
	if only["port"] != status["port"] {
		t.Fatalf("ports[0].port = %v, want the configured %v", only["port"], status["port"])
	}
}

// TestVersionJSONCarriesTheRunningBuild pins the values of the version document:
// the platform fields describe the machine the binary runs on, and the version
// itself is not empty. A report that could be all empty strings would make
// "which build is deployed?" unanswerable.
func TestVersionJSONCarriesTheRunningBuild(t *testing.T) {
	result, _ := runBinary(t, "version", "--json")
	if result.code != 0 {
		t.Fatalf("version exit = %d, stderr = %s", result.code, result.stderr)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &document); err != nil {
		t.Fatalf("version --json is not JSON: %v\n%s", err, result.stdout)
	}
	if want := runtime.GOOS + "/" + runtime.GOARCH; document["platform"] != want {
		t.Fatalf("platform = %v, want %q", document["platform"], want)
	}
	if version, _ := document["version"].(string); strings.TrimSpace(version) == "" {
		t.Fatalf("version = %v, want the build's version", document["version"])
	}
}

// portString renders a decoded JSON port the way an address spells it.
func portString(t *testing.T, value any) string {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("port = %v, want a number", value)
	}
	return strconv.Itoa(int(number))
}

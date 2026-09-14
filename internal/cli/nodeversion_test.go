package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are the end-to-end cases of the Node model: the real binary, a real
// start or a real refusal, and the settings document it leaves behind. The
// resolution itself is pinned in the nodejs package and the lifecycle in the
// service package; what is pinned here is that the command line wires them
// together — including the fact that a refusal happens before anything runs.

// checkoutFixture creates the markers a preflight requires, so a test reaches
// the runtime resolution instead of stopping at "the checkout is missing".
func checkoutFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"node_modules", ".dsh-build"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	for name, content := range map[string]string{
		"package.json":                             `{"name":"deepseek-harness"}`,
		"pnpm-workspace.yaml":                      "packages:\n  - packages/*/*\n",
		".dsh-build/client-build-environment.json": "{}",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// TestVerboseReportsAnUndeterminedNodeRelease pins the line an operator reads to
// find out what the installation will run: nothing has been determined yet, and
// the line says so rather than showing an empty value.
func TestVerboseReportsAnUndeterminedNodeRelease(t *testing.T) {
	result, _ := runBinaryWith(t, map[string]string{
		"PATH": stubToolPath(t, "pnpm", "node"),
	}, "-v", "doctor")

	combined := result.stdout + result.stderr
	if !strings.Contains(combined, "Node 版本: (未确定") {
		t.Fatalf("verbose output = %q, want the undetermined release reported", combined)
	}
}

// TestAReleaseBelowTheMinimumIsRefused pins the whole refusal at the command
// line: the exit code the caller scripts against, the release and the floor, the
// remedy, and — just as important — that nothing was written down and no server
// was started.
func TestAReleaseBelowTheMinimumIsRefused(t *testing.T) {
	result, stateDir := runBinaryWith(t, map[string]string{
		"PATH":         stubNodePath(t, "22.14.0"),
		"DSH_REPO_DIR": checkoutFixture(t),
	}, "start")

	if result.code != 4 {
		t.Fatalf("start exit = %d, want the preflight code\nstdout=%s\nstderr=%s", result.code, result.stdout, result.stderr)
	}
	for _, want := range []string{"22.14.0", "低于最低要求 24.12.0", "nvm install 24", "brew install node@24"} {
		if !strings.Contains(result.stderr, want) {
			t.Fatalf("stderr = %q, want it to contain %q", result.stderr, want)
		}
	}
	if _, err := os.Stat(filepath.Join(stateDir, "config.json")); err == nil {
		document := readSettingsDocument(t, stateDir)
		if _, present := document["nodeVersion"]; present {
			t.Fatalf("settings document = %v, want no release recorded by a refused start", document)
		}
	}
}

// TestARequestThatNamesNothingIsRefused pins that a value which is not a release
// is reported as a request that cannot be satisfied, rather than being silently
// treated as "use whatever is there" — which is what an operator who still
// writes the old "latest" needs to be told.
func TestARequestThatNamesNothingIsRefused(t *testing.T) {
	result, _ := runBinaryWith(t, map[string]string{
		"PATH":         stubToolPath(t, "pnpm", "node"),
		"DSH_REPO_DIR": checkoutFixture(t),
	}, "--node", "latest", "start")

	if result.code != 4 {
		t.Fatalf("start exit = %d, want the preflight code\nstderr=%s", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "找不到 Node latest") {
		t.Fatalf("stderr = %q, want the unsatisfiable request reported", result.stderr)
	}
	if !strings.Contains(result.stderr, "--node") {
		t.Fatalf("stderr = %q, want the way out named", result.stderr)
	}
}

// TestAnUnverifiedMajorVersionIsWarnedAboutAndUsed pins the middle ground at the
// command line: the release is used — the command goes on to fail for its own,
// unrelated reason — and the warning is printed.
func TestAnUnverifiedMajorVersionIsWarnedAboutAndUsed(t *testing.T) {
	result, _ := runBinaryWith(t, map[string]string{
		"PATH":         stubNodePath(t, "26.1.0"),
		"DSH_REPO_DIR": checkoutFixture(t),
	}, "build")

	if strings.Contains(result.stderr, "低于最低要求") {
		t.Fatalf("stderr = %q, want the release accepted", result.stderr)
	}
	if !strings.Contains(result.stderr, "不在 dshctl 的验证范围内") {
		t.Fatalf("stderr = %q, want the unverified release reported", result.stderr)
	}
}

// TestTheEnvironmentCannotLowerTheFloor pins the decision that the minimum is
// code rather than configuration: an environment that names both a release below
// it and a variable that looks like a setting for it changes nothing.
func TestTheEnvironmentCannotLowerTheFloor(t *testing.T) {
	result, stateDir := runBinaryWith(t, map[string]string{
		"PATH":                 stubNodePath(t, "22.14.0"),
		"DSH_MIN_NODE_VERSION": "1.0.0",
		"DSH_REPO_DIR":         checkoutFixture(t),
	}, "start")

	if result.code != 4 {
		t.Fatalf("start exit = %d, want the preflight code\nstderr=%s", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "低于最低要求") {
		t.Fatalf("stderr = %q, want the release refused", result.stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "config.json")); err == nil {
		if document := readSettingsDocument(t, stateDir); document["minNodeVersion"] != nil {
			t.Fatalf("settings document = %v, want no minimum recorded", document)
		}
	}
}

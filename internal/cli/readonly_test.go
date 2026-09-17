package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestReportingCommandsLeaveNoTrace runs the real binary for every reporting
// command and asserts that nothing appears on disk — neither the state
// directory, nor anything under the throwaway home the binary was handed.
//
// This is the property the documentation promises, and the reason it is checked
// against the binary rather than against the service layer is that the promise
// is about the command line: a lock file created by an intermediate call is
// invisible to a unit test that only inspects one path, and that is exactly how
// a read-only promise was broken before.
func TestReportingCommandsLeaveNoTrace(t *testing.T) {
	cases := []struct {
		name string
		args []string
		// wantCode is the exact status the command must produce in this
		// fixture: a fresh home, no repository, no log and a free port.
		wantCode int
	}{
		{"status", []string{"status"}, 3},
		{"status json", []string{"status", "--json"}, 3},
		{"url", []string{"url"}, 3},
		{"logs", []string{"logs"}, 1},
		{"logs build", []string{"logs", "--build"}, 0},
		{"doctor", []string{"doctor"}, 1},
		{"doctor json", []string{"doctor", "--json"}, 1},
		{"version", []string{"version"}, 0},
		{"version json", []string{"version", "--json"}, 0},
		{"help", []string{"--help"}, 0},
		{"verbose status", []string{"-v", "status"}, 3},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result, stateDir := runBinary(t, testCase.args...)
			// The exact status is asserted rather than "not a crash": a
			// reporting command that starts answering 1 instead of 3, or 0
			// instead of 1, breaks every script that branches on it.
			if result.code != testCase.wantCode {
				t.Fatalf("%v exit = %d, want %d (stderr = %s)", testCase.args, result.code, testCase.wantCode, result.stderr)
			}
			if _, err := os.Lstat(stateDir); err == nil {
				entries, readErr := os.ReadDir(stateDir)
				names := make([]string, 0, len(entries))
				for _, entry := range entries {
					names = append(names, entry.Name())
				}
				if readErr != nil {
					t.Fatalf("reading the state directory: %v", readErr)
				}
				t.Fatalf("%v created state: %v", testCase.args, names)
			} else if !os.IsNotExist(err) {
				t.Fatalf("inspecting the state directory: %v", err)
			}
			// The binary's home must stay untouched too: the state directory is
			// the documented place for writes, but a reporting command must not
			// write anywhere at all.
			home := filepath.Join(filepath.Dir(stateDir), "home")
			if _, err := os.Lstat(filepath.Join(home, ".dsh")); err == nil {
				t.Fatalf("%v created state under the home directory: %s", testCase.args, filepath.Join(home, ".dsh"))
			} else if !os.IsNotExist(err) {
				t.Fatalf("inspecting the home directory: %v", err)
			}
		})
	}
}

// TestReportingCommandsLeaveTheTreeUnchanged runs every reporting command inside
// a tree that already holds a settings document, and asserts the whole tree is
// byte-identical afterwards.
//
// The test above asserts that two paths stayed absent; this one asserts the
// stronger property the README promises — a read-only command writes nowhere —
// over every path the run could touch: the working directory, the state
// directory and the throwaway home at once. The document matters: a command that
// rewrites the operator's config.json in place, or drops a lock file beside it,
// leaves the absence checks green, and an in-place rewrite is exactly the shape
// of a "read-only" regression that survives a smoke test.
func TestReportingCommandsLeaveTheTreeUnchanged(t *testing.T) {
	cases := [][]string{
		{"status"},
		{"status", "--json"},
		{"url"},
		{"logs"},
		{"logs", "--build"},
		{"doctor"},
		{"doctor", "--json"},
		{"version"},
		{"version", "--json"},
		{"--help"},
		{"-v", "status"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := t.TempDir()
			stateDir := filepath.Join(root, "state")
			// The home is created here rather than by the runner helper: it has
			// to exist before the snapshot, or the snapshot would report the
			// fixture's own directory as the command's doing.
			for _, directory := range []string{stateDir, filepath.Join(root, "home")} {
				if err := os.MkdirAll(directory, 0o700); err != nil {
					t.Fatalf("mkdir %s: %v", directory, err)
				}
			}
			// The document names a path inside this fixture rather than a
			// literal: a settings document whose repoDir is not absolute is
			// rejected as a usage error, and "absolute" is spelled differently
			// on Windows.
			document := fmt.Sprintf("{\n  \"repoDir\": %q,\n  \"port\": 3080\n}\n", filepath.Join(root, "repo"))
			documentPath := filepath.Join(stateDir, "config.json")
			if err := os.WriteFile(documentPath, []byte(document), 0o600); err != nil {
				t.Fatalf("write the settings document: %v", err)
			}

			before := treeSnapshot(t, root)
			result := runBinaryIn(t, root, nil, args...)
			// The exit code belongs to the sibling test, which pins it per
			// command; this one is about what the run left behind, so it only
			// rules out a crash that would make the comparison meaningless.
			if result.code == 2 {
				t.Fatalf("%v exit = 2, which is a usage error, not a reporting run (stderr = %s)", args, result.stderr)
			}
			if diff := firstTreeDifference(before, treeSnapshot(t, root)); diff != "" {
				t.Fatalf("%v changed the tree it ran in:\n%s", args, diff)
			}
			// The document is checked by content as well as by hash: the
			// failure has to say what the command did to it, not only that a
			// digest moved.
			contents, err := os.ReadFile(documentPath)
			if err != nil {
				t.Fatalf("the settings document is gone after %v: %v", args, err)
			}
			if string(contents) != document {
				t.Fatalf("%v rewrote the settings document:\ngot  %q\nwant %q", args, contents, document)
			}
		})
	}
}

// treeSnapshot renders every entry under root as a comparable line, so two
// snapshots can be compared without a directory-diff dependency.
func treeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		line := relative + " " + info.Mode().String()
		if !entry.IsDir() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			line += " " + strconv.Itoa(len(contents)) + " " + digest(contents)
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", root, err)
	}
	sort.Strings(lines)
	return lines
}

// firstTreeDifference describes the first way two snapshots disagree, or returns
// an empty string when they are identical.
func firstTreeDifference(before, after []string) string {
	seen := map[string]bool{}
	for _, line := range before {
		seen[line] = true
	}
	for _, line := range after {
		if !seen[line] {
			return "  appeared or changed: " + line
		}
		delete(seen, line)
	}
	for _, line := range before {
		if seen[line] {
			return "  disappeared or changed: " + line
		}
	}
	return ""
}

// digest is a short content hash, enough to notice any rewrite.
func digest(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:8])
}

// TestMutatingCommandsDoProvision pins the other side of the boundary, so the
// read-only assertion above cannot pass by accident because nothing ever writes.
//
// The fixture is a directory that carries the checkout's markers but is not a
// git repository, so the build gets as far as the residue prune — which asks git
// about the tree — and fails there. Node and pnpm are stubbed onto the PATH so
// the run reaches that step on every machine: without them the same command
// stops earlier on a host that has no pnpm, which says something about the host
// rather than about the code.
func TestMutatingCommandsDoProvision(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	if err := os.MkdirAll(filepath.Join(checkout, "node_modules"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(checkout, ".dsh-build"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, content := range map[string]string{
		"package.json":                             `{"name":"deepseek-harness"}`,
		"pnpm-workspace.yaml":                      "packages:\n  - packages/*/*\n",
		".dsh-build/client-build-environment.json": "{}",
	} {
		if err := os.WriteFile(filepath.Join(checkout, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// A build is the cheapest mutating command: it provisions the state
	// directory before it runs anything.
	//
	// The fixture is a checkout by its markers but not a git repository, so the
	// build proceeds as far as the residue prune and then fails there. That is
	// the status asserted: 1, a plain failure, because the tool could not do what
	// it was asked — and the provisioning below happened before that.
	result, stateDir := runBinaryWith(t, map[string]string{
		"PATH": stubToolPath(t, "pnpm", "node"),
	}, "--repo", checkout, "build")
	if result.code != 1 {
		t.Fatalf("build exit = %d, want 1 (stderr = %s)", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "git") {
		t.Fatalf("build stderr = %q, want it to name what stopped it", result.stderr)
	}
	if _, err := os.Lstat(stateDir); err != nil {
		t.Fatalf("a mutating command must provision the state directory: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "config.json")); err != nil {
		t.Fatalf("a mutating command must write the default config: %v", err)
	}
}

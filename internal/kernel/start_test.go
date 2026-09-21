package kernel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
)

// TestStartReportsAServerThatIsStillStarting pins the second early return: a
// server dshctl started that owns the port but does not answer yet is reported
// as starting, not as already running and not as a reason to launch a second
// one.
func TestStartReportsAServerThatIsStillStarting(t *testing.T) {
	f := newFixture(t)
	f.host.add(4321, "pnpm --dir repo dsh web", fixtureStartTime)
	f.host.listen(4321)
	f.host.ready = false
	if err := f.Record.Save(domain.Record{
		PID: 4321, StartedAt: fixtureStartTime, Port: f.Settings.Port, Phase: domain.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if result.AlreadyRunning {
		t.Fatal("a server that has not answered yet was reported as already running")
	}
	if result.SpawnedPID != 0 {
		t.Fatalf("SpawnedPID = %d, want 0: a starting server must not be duplicated", result.SpawnedPID)
	}
	if result.Status.State != domain.StateStarting {
		t.Fatalf("state = %q, want %q", result.Status.State, domain.StateStarting)
	}
	want := fmt.Sprintf("DSH Web 正在启动中: %s (pid=%d)\n", f.Settings.URL(), 4321)
	if f.out.String() != want {
		t.Fatalf("output = %q, want %q", f.out.String(), want)
	}
	f.wantNoSpawn(t)
	f.wantNoSignals(t)
}

// startPreflightCases are the preconditions a start checks before it spawns
// anything. Each one must be a Preflight failure naming the unmet condition:
// the operator has to learn what to fix, and no process may be launched on a
// premise that is already known to be broken.
var startPreflightCases = []struct {
	name   string
	setup  func(*fixture, *testing.T)
	phrase string
}{
	{
		name: "repository directory missing",
		setup: func(f *fixture, _ *testing.T) {
			f.Settings.RepoDir = filepath.Join(f.root, "no-such-repo")
			f.Repo.Dir = f.Settings.RepoDir
		},
		phrase: "仓库目录不存在",
	},
	{
		name: "not a checkout",
		setup: func(f *fixture, t *testing.T) {
			if err := os.Remove(filepath.Join(f.repo, "pnpm-workspace.yaml")); err != nil {
				t.Fatalf("remove workspace manifest: %v", err)
			}
		},
		phrase: "看起来不是 DeepSeek Harness 仓库",
	},
	{
		name: "build not ready",
		setup: func(f *fixture, t *testing.T) {
			if err := os.Remove(filepath.Join(f.repo, filepath.FromSlash(buildRecordRel))); err != nil {
				t.Fatalf("remove build record: %v", err)
			}
		},
		phrase: "仓库尚未构建",
	},
	{
		name: "pnpm is not on PATH",
		setup: func(f *fixture, _ *testing.T) {
			f.LookPath = func(name string) (string, error) {
				if name == "pnpm" {
					return "", errors.New("executable file not found in $PATH")
				}
				return "/fake/bin/" + name, nil
			}
		},
		phrase: "找不到 pnpm",
	},
	{
		name: "pinned Node release is not installed",
		setup: func(f *fixture, t *testing.T) {
			// A different release is installed and the settings keep pinning
			// the default: the resolver must refuse rather than silently use a
			// runtime the operator did not ask for.
			f.seedNodeInstallation(t, "25.1.1")
			f.Settings.NodeVersion = config.TestedNodeVersion
		},
		phrase: "找不到 Node " + config.TestedNodeVersion,
	},
}

// TestStartRefusesAnUnmetPrecondition walks every precondition the start checks
// before it spawns: each is a Preflight failure, and none may reach the
// launcher or leave a record behind.
func TestStartRefusesAnUnmetPrecondition(t *testing.T) {
	for _, testCase := range startPreflightCases {
		t.Run(testCase.name, func(t *testing.T) {
			f := newFixture(t)
			testCase.setup(f, t)

			_, err := f.Start(context.Background())
			wantCode(t, err, exitcode.Preflight)
			wantContains(t, err, testCase.phrase)
			f.wantNoSpawn(t)
			f.wantNoSignals(t)
			f.wantNoRecordOnDisk(t)
		})
	}
}

// TestStartReportsASpawnFailure pins the last step that can fail before the
// readiness wait: the operating system refuses to start the child. The failure
// is reported as a Failure (not a precondition), nothing was created, so nothing
// may be signalled and no record may be written.
func TestStartReportsASpawnFailure(t *testing.T) {
	f := newFixture(t)
	f.Spawn = func(string, []string, string, []string, *os.File) (int, func(context.Context) bool, error) {
		return 0, nil, errors.New("fork/exec /fake/bin/pnpm: resource temporarily unavailable")
	}

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "resource temporarily unavailable")
	f.wantNoRecordOnDisk(t)
	f.wantNoSignals(t)
	if !strings.Contains(f.out.String(), "正在后台启动 DSH Web") {
		t.Fatalf("stdout = %q, want the launch attempt reported", f.out.String())
	}
	// A spawn that never happened has nothing to clean up, so the failure is
	// reported on its own terms.
	if strings.Contains(f.errOut.String(), "正在清理本次启动的进程") {
		t.Fatalf("stderr = %q, want no cleanup message for a child that never ran", f.errOut.String())
	}
}

// TestStartHandsTheServerTheExactCommand pins what the launcher is asked for.
//
// The fictional launcher does not read its arguments, so before this test a
// wrong port, a missing --no-open or a wrong working directory passed every
// assertion in the package. Each value here is behaviour: --port decides which
// address the record and every later port probe describe, --no-open keeps a
// background start from opening a browser, and the working directory is what
// makes pnpm find the checkout's own workspace.
func TestStartHandsTheServerTheExactCommand(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	call := f.wantSpawn(t)
	if !strings.HasSuffix(call.path, "pnpm") {
		t.Fatalf("path = %q, want the resolved pnpm", call.path)
	}
	want := []string{"--dir", f.repo, "dsh", "web", "--port", fmt.Sprint(f.Settings.Port), "--no-open"}
	if strings.Join(call.args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", call.args, want)
	}
	if call.dir != f.repo {
		t.Fatalf("dir = %q, want the checkout %q", call.dir, f.repo)
	}
	// The child finds the Node runtime dshctl resolved because its bin
	// directory comes first on PATH; the operator's own PATH is inherited
	// behind it.
	wantEnvPathPrefix(t, call.env, filepath.Dir(nodeInstallationFor(t, f)))
	if result.SpawnedPID == 0 {
		t.Fatal("the start reported no pid")
	}
}

// nodeInstallationFor reports the fake node binary the fixture resolves, so a
// test can assert on the bin directory the child is handed without repeating how
// the fixture lays the tree out.
//
// It asks the resolver rather than the PATH lookup: a release a version manager
// holds is resolved without consulting PATH at all, so the two answers are not
// the same thing and the assertion has to be about the one the start uses.
func nodeInstallationFor(t *testing.T, f *fixture) string {
	t.Helper()
	return f.resolvedNode(t).NodePath
}

// TestStartPrintsTheAnnouncedAddress pins the line that turns a successful start
// into something the operator can open. The address is not derivable from the
// port: it carries the access token, which only the server's own output has.
func TestStartPrintsTheAnnouncedAddress(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	announced := fmt.Sprintf("http://127.0.0.1:%d/?token=startme", f.Settings.Port)
	if err := f.LogFile.Line("dsh web: " + announced); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	if _, err := f.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.Contains(f.out.String(), "访问地址: "+announced+"\n") {
		t.Fatalf("stdout = %q, want the access address line", f.out.String())
	}
}

// TestStartSaysNothingAboutAnAddressItDoesNotKnow pins the other half: a server
// that announced no address must not produce an empty "访问地址:" line, which
// reads like a broken start.
func TestStartSaysNothingAboutAnAddressItDoesNotKnow(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true

	if _, err := f.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if strings.Contains(f.out.String(), "访问地址") {
		t.Fatalf("stdout = %q, want no address line for an unknown address", f.out.String())
	}
}

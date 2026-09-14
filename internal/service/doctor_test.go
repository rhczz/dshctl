package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/detach"
	"github.com/rhczz/dshctl/internal/lock"
	"github.com/rhczz/dshctl/internal/run"
	statepkg "github.com/rhczz/dshctl/internal/state"
)

// TestDoctorReportsEveryRowInOrder drives the diagnosis over the shapes a real
// machine takes and asserts the complete row set: every check name, its status,
// its detail, and the order.
//
// The order and the count are part of the contract, not incidental. An operator
// reads the report top to bottom and a script may take the Nth line, so a check
// that silently disappeared — or moved above the one it depends on — would
// change the meaning of the output. Every branch that a test does not pin here
// is a branch that can regress without a single failure being reported.
func TestDoctorReportsEveryRowInOrder(t *testing.T) {
	recordLive := func(f *fixture, pid int) statepkg.Record {
		return statepkg.Record{
			PID: pid, SpawnedPID: pid, StartedAt: fixtureStartTime,
			Port: f.Settings.Port, Phase: statepkg.PhaseRunning,
		}
	}

	cases := []struct {
		name string
		// setup arranges the machine; it runs after the fixture exists.
		setup func(*fixture, *testing.T)
		// change applies the differences from a healthy, idle fixture, so each
		// case states only what it is about.
		change func(*fixture, *[]Check)
		// detailPrefix asserts a row's detail by prefix, for the rows whose
		// exact text belongs to another package.
		detailPrefix map[string]string
		// exact names rows whose detail is compared byte for byte against the
		// expectation the change replaced them with, overriding the general
		// exemption for rows the fixture cannot reproduce.
		exact []string
	}{
		{
			name:  "healthy and idle",
			setup: func(*fixture, *testing.T) {},
		},
		{
			name: "a checkout with uncommitted changes",
			setup: func(f *fixture, _ *testing.T) {
				f.host.mu.Lock()
				f.host.gitStatus = " M apps/cli/src/main.ts\n"
				f.host.mu.Unlock()
			},
			change: func(_ *fixture, checks *[]Check) {
				replaceCheck(*checks, "仓库版本", CheckOK, "main@abc1234 (有未提交改动)")
			},
		},
		{
			name: "a checkout that is not a git repository",
			setup: func(f *fixture, t *testing.T) {
				if err := os.RemoveAll(filepath.Join(f.repo, ".git")); err != nil {
					t.Fatalf("remove .git: %v", err)
				}
			},
			change: func(f *fixture, checks *[]Check) {
				// The same slot holds either the version or the reason the
				// repository could not be read; it is never both.
				replaceRow(*checks, "仓库版本", Check{Name: "仓库目录", Status: CheckFail, Detail: f.repo + " 不是 git 仓库"})
			},
		},
		{
			name: "a repository directory that only has package.json",
			setup: func(f *fixture, t *testing.T) {
				if err := os.Remove(filepath.Join(f.repo, "pnpm-workspace.yaml")); err != nil {
					t.Fatalf("remove workspace manifest: %v", err)
				}
			},
			change: func(f *fixture, checks *[]Check) {
				replaceRow(*checks, "仓库版本", Check{Name: "仓库目录", Status: CheckFail, Detail: f.repo +
					" 缺少 package.json 或 pnpm-workspace.yaml，不像是 DeepSeek Harness checkout"})
			},
		},
		{
			name: "dependencies not installed",
			setup: func(f *fixture, t *testing.T) {
				if err := os.RemoveAll(filepath.Join(f.repo, "node_modules")); err != nil {
					t.Fatalf("remove node_modules: %v", err)
				}
			},
			change: func(f *fixture, checks *[]Check) {
				dependencies := filepath.Join(f.repo, "node_modules")
				replaceCheck(*checks, "依赖", CheckFail, dependencies+" 不存在，请先执行 pnpm install")
				replaceCheck(*checks, "构建产物", CheckFail, "缺少 "+f.Repo.BuildRecordPath()+"，请运行 dshctl build")
			},
		},
		{
			name: "the server is running",
			setup: func(f *fixture, t *testing.T) {
				f.startServer(t, 4321, "")
			},
			change: func(f *fixture, checks *[]Check) {
				replaceCheck(*checks, "端口", CheckOK, fmt.Sprint(f.Settings.Port)+" 由 dshctl 启动的服务占用 (pid=4321)")
				replaceCheck(*checks, "运行记录", CheckOK, recordLive(f, 4321).Describe())
			},
		},
		{
			name: "the recorded server owns the port but is not answering",
			setup: func(f *fixture, t *testing.T) {
				f.host.add(4321, "pnpm --dir repo dsh web", fixtureStartTime)
				f.host.listen(4321)
				f.host.ready = false
				if err := f.Record.Save(statepkg.Record{
					PID: 4321, SpawnedPID: 4321, StartedAt: fixtureStartTime,
					Port: f.Settings.Port, Phase: statepkg.PhaseRunning,
				}); err != nil {
					t.Fatalf("save record: %v", err)
				}
			},
			change: func(f *fixture, checks *[]Check) {
				replaceCheck(*checks, "端口", CheckWarn, fmt.Sprint(f.Settings.Port)+" 由 dshctl 的服务占用 (pid=4321)，端口尚未就绪")
				replaceCheck(*checks, "运行记录", CheckOK, recordLive(f, 4321).Describe())
			},
		},
		{
			name: "another harness server owns the port",
			setup: func(f *fixture, _ *testing.T) {
				f.host.serving(4242, "node /opt/other/deepseek-harness/apps/cli/lib/bin.js web --port 3080")
			},
			change: func(f *fixture, checks *[]Check) {
				replaceCheck(*checks, "端口", CheckWarn, fmt.Sprint(f.Settings.Port)+
					" 被其他进程占用 (pid=4242: node /opt/other/deepseek-harness/apps/cli/lib/bin.js web --port 3080)")
			},
		},
		{
			name: "an unrelated program owns the port",
			setup: func(f *fixture, _ *testing.T) {
				f.host.serving(7777, "python3 -m http.server 3080")
			},
			change: func(f *fixture, checks *[]Check) {
				replaceCheck(*checks, "端口", CheckWarn, fmt.Sprint(f.Settings.Port)+
					" 被一个 dshctl 无法确认归属的进程占用 (pid=7777: python3 -m http.server 3080)")
			},
		},
		{
			name: "the recorded process is alive without the port",
			setup: func(f *fixture, t *testing.T) {
				f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
				if err := f.Record.Save(recordLive(f, 4242)); err != nil {
					t.Fatalf("save record: %v", err)
				}
			},
			change: func(f *fixture, checks *[]Check) {
				replaceCheck(*checks, "端口", CheckOK, fmt.Sprint(f.Settings.Port)+" 空闲")
				replaceCheck(*checks, "运行记录", CheckOK, recordLive(f, 4242).Describe())
			},
		},
		{
			name: "a survivor of an interrupted start serves the port",
			setup: func(f *fixture, t *testing.T) {
				seedInterruptedStart(t, f, 8000, 8001, false)
			},
			change: func(f *fixture, checks *[]Check) {
				replaceCheck(*checks, "端口", CheckWarn, fmt.Sprint(f.Settings.Port)+
					" 上是上次启动被中断后仍存活的服务 (pid=8001);运行 dshctl start 或 dshctl stop 可恢复管理")
				replaceCheck(*checks, "运行记录", CheckWarn, statepkg.Record{
					PID: 8000, SpawnedPID: 8000, StartedAt: fixtureStartTime,
					Port: f.Settings.Port, Phase: statepkg.PhaseRunning,
				}.Describe())
			},
		},
		{
			name: "a stale record of a process that is gone",
			setup: func(f *fixture, t *testing.T) {
				if err := f.Record.Save(statepkg.Record{
					PID: 999999, StartedAt: 1_600_000_000, Port: f.Settings.Port, Phase: statepkg.PhaseRunning,
				}); err != nil {
					t.Fatalf("save record: %v", err)
				}
			},
			change: func(_ *fixture, checks *[]Check) {
				replaceCheck(*checks, "运行记录", CheckWarn,
					"记录 pid=999999 已不存在或已被复用(陈旧记录，下次 start/stop 会清理)")
			},
		},
		{
			name: "an unreadable record",
			setup: func(f *fixture, t *testing.T) {
				f.seedCorruptRecord(t, corruptRecordContent)
			},
			change: func(f *fixture, checks *[]Check) {
				replaceCheck(*checks, "运行记录", CheckWarn, f.Record.Path+" 无法解析，下次 start/stop 会重建它")
			},
		},
		{
			name: "node below the supported minimum",
			setup: func(f *fixture, t *testing.T) {
				f.seedNodeInstallation(t, "18.20.4")
			},
			change: func(f *fixture, checks *[]Check) {
				replaceCheck(*checks, "Node", CheckWarn, filepath.Join(f.root, ".nvm", "versions", "node",
					"v18.20.4", "bin", "node")+" (18.20.4, nvm)，低于 "+
					config.MinNodeVersion+"，Web 端可能出现 \"Failed to load plugins\"")
			},
			// The Node row's detail is exempt from the byte-for-byte comparison
			// in general, so the version and the remedy this branch must carry
			// are pinned here explicitly instead of being silently skipped.
			exact: []string{"Node"},
		},
		{
			name: "the pinned node release is not installed",
			setup: func(f *fixture, _ *testing.T) {
				// A different release is installed and the settings pin the
				// default one, so the resolver has to refuse rather than
				// silently use a runtime the operator did not ask for.
				f.seedNodeInstallation(t, "25.1.1")
				f.Settings.NodeVersion = config.DefaultNodeVersion
			},
			change: func(_ *fixture, checks *[]Check) {
				replaceCheck(*checks, "Node", CheckFail, nodeNotInstalledDetail)
			},
			detailPrefix: map[string]string{"Node": nodeNotInstalledDetail},
		},
		{
			name: "pnpm is not on PATH",
			setup: func(f *fixture, _ *testing.T) {
				f.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
			},
			change: func(_ *fixture, checks *[]Check) {
				replaceCheck(*checks, "pnpm", CheckFail, "找不到 pnpm，请先安装并确保它在 PATH 中")
			},
		},
		{
			name: "pnpm exists but will not run",
			setup: func(f *fixture, _ *testing.T) {
				f.host.fail = func(cmd run.Command) error {
					if filepath.Base(cmd.Name) == "pnpm" && hasArgument(cmd, "--version") {
						return errors.New("pnpm: executable format error")
					}
					return nil
				}
			},
			change: func(_ *fixture, checks *[]Check) {
				replaceCheck(*checks, "pnpm", CheckWarn, "/fake/bin/pnpm 存在但无法执行")
			},
		},
		{
			name: "the lock file is residue",
			setup: func(f *fixture, t *testing.T) {
				if err := os.MkdirAll(f.Settings.LockFile(), 0o755); err != nil {
					t.Fatalf("create lock residue: %v", err)
				}
			},
			change: func(f *fixture, checks *[]Check) {
				replaceCheck(*checks, "操作锁", CheckWarn, "锁路径 "+f.Settings.LockFile()+" 不是普通文件，无法检查")
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			f := newFixture(t)
			testCase.setup(f, t)
			want := baselineChecks(t, f)
			if testCase.change != nil {
				testCase.change(f, &want)
			}
			checks := f.Doctor(context.Background())
			wantChecks(t, checks, want)
			for name, prefix := range testCase.detailPrefix {
				if got := checkNamed(t, checks, name); !strings.HasPrefix(got.Detail, prefix) {
					t.Fatalf("%s detail = %q, want it to start with %q", name, got.Detail, prefix)
				}
			}
			for _, name := range testCase.exact {
				got := checkNamed(t, checks, name)
				wanted := checkNamed(t, want, name)
				if got.Detail != wanted.Detail {
					t.Fatalf("%s detail = %q, want %q", name, got.Detail, wanted.Detail)
				}
			}
		})
	}
}

// nodeNotInstalledDetail is the first line of the resolver's refusal. The rest
// of the message lists the ways out, which the Node package's own tests pin.
const nodeNotInstalledDetail = "找不到 Node " + config.DefaultNodeVersion + "(已查找 nvm 与 fnm 的安装目录)"

// TestDoctorReportsEveryRowOfAFreshStateDirectory pins the two warnings that
// only appear before dshctl has ever written its settings: the settings document
// does not exist yet, so the next run will write defaults into it. The state
// directory itself exists — dshctl creates it on the way in — which is why the
// warning is about the file, not the directory.
func TestDoctorReportsEveryRowOfAFreshStateDirectory(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.state, 0o755); err != nil {
		t.Fatalf("create state: %v", err)
	}
	want := baselineChecks(t, f)
	replaceCheck(want, "配置文件", CheckWarn, f.Settings.ConfigPath+" 尚未创建，将写入默认值")
	// The baseline provisions the document; this fixture is one whose settings
	// have never been written.
	if err := os.Remove(f.Settings.ConfigPath); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	wantChecks(t, f.Doctor(context.Background()), want)
}

// TestDoctorWarnsAboutAStateDirectoryThatWasNeverCreated pins the warning that
// tells a first-time operator where dshctl keeps its state. It is a warning and
// not a failure: the next mutating command creates the directory.
func TestDoctorWarnsAboutAStateDirectoryThatWasNeverCreated(t *testing.T) {
	f := newFixture(t)
	want := baselineChecks(t, f)
	replaceCheck(want, "状态目录", CheckWarn, f.state+" 尚未创建，首次运行会自动创建")
	replaceCheck(want, "配置文件", CheckWarn, f.Settings.ConfigPath+" 尚未创建，将写入默认值")
	// The baseline provisions the machine; this fixture is one dshctl has never
	// run against, so both are removed again before the diagnosis.
	if err := os.RemoveAll(f.state); err != nil {
		t.Fatalf("remove state: %v", err)
	}
	wantChecks(t, f.Doctor(context.Background()), want)

	if _, err := os.Lstat(f.state); !os.IsNotExist(err) {
		t.Fatalf("doctor created the state directory (err=%v)", err)
	}
}

// TestDoctorReportsAMissingRepository pins the rows that describe a checkout
// which is not there at all. The version row and the repository failure share
// one slot, and every row that depends on the checkout fails with it.
func TestDoctorReportsAMissingRepository(t *testing.T) {
	f := newFixture(t)
	want := baselineChecks(t, f)
	if err := os.RemoveAll(f.repo); err != nil {
		t.Fatalf("remove repo: %v", err)
	}
	// The version row and the repository failure share one slot: a missing
	// checkout is reported in its place, not beside it.
	replaceRow(want, "仓库版本", Check{Name: "仓库目录", Status: CheckFail, Detail: f.repo + " 不存在"})
	replaceCheck(want, "依赖", CheckFail, filepath.Join(f.repo, "node_modules")+" 不存在，请先执行 pnpm install")
	replaceCheck(want, "构建产物", CheckFail, "缺少 "+f.Repo.BuildRecordPath()+"，请运行 dshctl build")

	checks := f.Doctor(context.Background())
	wantChecks(t, checks, want)
	// A missing repository is a hard failure, so the exit code of `doctor` is
	// non-zero.
	if !ChecksFailed(checks) {
		t.Fatalf("doctor passed a missing repository: %+v", checks)
	}
}

// TestDoctorReportsALockHeldByALiveProcess pins the third lock row: a pid is
// readable, so the report can name the operation that is in the way.
func TestDoctorReportsALockHeldByALiveProcess(t *testing.T) {
	f := newFixture(t)
	held, err := lock.Acquire(context.Background(), f.Settings.LockFile(), 2*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	want := baselineChecks(t, f)
	replaceCheck(want, "操作锁", CheckWarn,
		"被 pid="+strconv.Itoa(os.Getpid())+" 持有，另一个 dshctl 操作正在进行")
	wantChecks(t, f.Doctor(context.Background()), want)
}

// baselineChecks is the row set a healthy fixture produces: a state directory
// that exists, a settings document on disk, a clean checkout, and no service
// running. Each case in TestDoctorReportsEveryRowInOrder states only how it
// differs from this, which is what keeps a thirteen-row expectation readable.
func baselineChecks(t *testing.T, f *fixture) []Check {
	t.Helper()
	// A first run creates both; the machine under test is one dshctl has already
	// used.
	if err := f.Settings.Provision(); err != nil {
		t.Fatalf("provision the state directory: %v", err)
	}
	node := nodeInstallationFor(t, f)
	return []Check{
		{Name: "状态目录", Status: CheckOK, Detail: f.state},
		{Name: "配置文件", Status: CheckOK, Detail: f.Settings.ConfigPath},
		// One slot holds either the checked-out revision or the reason the
		// repository could not be read, never both.
		{Name: "仓库版本", Status: CheckOK, Detail: "main@abc1234"},
		{Name: "依赖", Status: CheckOK, Detail: filepath.Join(f.repo, "node_modules") + " 已安装"},
		{Name: "构建产物", Status: CheckOK, Detail: f.Repo.BuildRecordPath()},
		{Name: "Node", Status: CheckOK, Detail: node + " (" + config.DefaultNodeVersion + ", nvm)"},
		{Name: "pnpm", Status: CheckOK, Detail: "/fake/bin/pnpm 11.0.0"},
		{Name: "端口", Status: CheckOK, Detail: strconv.Itoa(f.Settings.Port) + " 空闲"},
		{Name: "运行记录", Status: CheckOK, Detail: "不存在(尚未启动过服务)"},
		{Name: "操作锁", Status: CheckOK, Detail: "空闲"},
		// The log has not been written yet, which the size lookup reports as
		// zero rather than as an error.
		{Name: "日志", Status: CheckOK, Detail: f.Settings.LogPath + " (0 B)"},
		{Name: "进程分离方式", Status: CheckOK, Detail: detach.Describe()},
	}
}

// replaceCheck rewrites one expected row in place, leaving the order alone.
func replaceCheck(checks []Check, name, status, detail string) {
	for index := range checks {
		if checks[index].Name == name {
			checks[index].Status = status
			checks[index].Detail = detail
			return
		}
	}
	panic("no expected check named " + name)
}

// replaceRow substitutes one expected row for another, which is how a check that
// only exists when an earlier one succeeded is modelled. The count changes with
// it: the version row is not reported at all when the repository could not be
// read.
func replaceRow(checks []Check, name string, row Check) {
	for index := range checks {
		if checks[index].Name == name {
			checks[index] = row
			return
		}
	}
	panic("no expected check named " + name)
}

// wantChecks asserts the exact rows the diagnosis returned: names, statuses,
// details and order.
func wantChecks(t *testing.T, got, want []Check) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("doctor reported %d checks, want %d:\ngot  %s\nwant %s",
			len(got), len(want), renderChecks(got), renderChecks(want))
	}
	for index := range want {
		if got[index] == want[index] {
			continue
		}
		// A few details carry a value the fixture cannot predict (a process
		// separation description, a log size that depends on what the fixture
		// wrote). Those are compared by name and status, with the detail
		// asserted separately where it matters.
		if got[index].Name == want[index].Name && got[index].Status == want[index].Status &&
			unpredictableDetail(want[index]) {
			continue
		}
		t.Fatalf("check %d = %+v, want %+v\ngot  %s\nwant %s",
			index, got[index], want[index], renderChecks(got), renderChecks(want))
	}
}

// unpredictableDetail reports whether a row's detail is a value the fixture
// cannot reproduce byte for byte: the process separation description belongs to
// the platform, the log size to whatever the fixture wrote, and a resolver
// refusal carries a multi-line remedy whose exact wording its own package pins.
// Those rows are compared by name and status, and the detail is asserted as a
// prefix by the case that cares.
func unpredictableDetail(check Check) bool {
	switch check.Name {
	case "进程分离方式", "日志", "Node":
		return true
	}
	return false
}

// renderChecks renders a row set for failure messages.
func renderChecks(checks []Check) string {
	var builder strings.Builder
	for _, check := range checks {
		builder.WriteString("\n  [")
		builder.WriteString(check.Status)
		builder.WriteString("] ")
		builder.WriteString(check.Name)
		builder.WriteString(": ")
		builder.WriteString(check.Detail)
	}
	return builder.String()
}

package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/host"
)

// hostFacts builds process facts for the rendering tests.
func hostFacts(command, source string) host.Facts {
	return host.Facts{PID: 1, Alive: true, Command: command, Source: source}
}

// TestPrintStatusNamesTheState pins the human-readable report for every state,
// so an operator always learns what was observed.
// TestTheStatusReportNamesOnlyInstancesThatMatter pins what a bare report is
// worth reading: an instance that is simply not running is left out of the notes
// beside the report.
//
// Every start leaves a record behind, so after a few stop-and-start rounds a
// report that listed every record would bury the servers that are up under the
// ports that were switched off — the same failure as not reporting them at all,
// one layer further out. The instances themselves stay in the report, because a
// caller that wants every one of them is the reason the list exists.
func TestTheStatusReportNamesOnlyInstancesThatMatter(t *testing.T) {
	statuses := []Status{
		{State: StateRunning, Port: 3080},
		{State: StateStopped, Port: 3081},
		{State: StateOrphan, Port: 3082, ListenerPID: 42},
		{State: StateRunning, Port: 3083},
	}
	report := NewStatusReport(statuses)

	if report.Status.Port != 3080 {
		t.Fatalf("report.Status.Port = %d, want the first instance", report.Status.Port)
	}
	want := []int{3082, 3083}
	got := make([]int, 0, len(report.Others))
	for _, status := range report.Others {
		got = append(got, status.Port)
	}
	if !equalInts(got, want) {
		t.Fatalf("the notes name ports %v, want %v", got, want)
	}
	if len(report.Ports) != len(statuses) {
		t.Fatalf("the report lists %d instances, want all %d", len(report.Ports), len(statuses))
	}
}

func TestPrintStatusNamesTheState(t *testing.T) {
	cases := []struct {
		name   string
		status Status
		want   []string
	}{
		{
			name:   "running",
			status: Status{State: StateRunning, URL: "http://127.0.0.1:3080", ListenerPID: 42, URLFromRecord: "http://127.0.0.1:3080/?token=x", RepoDir: "/repo", LogPath: "/log"},
			want:   []string{"运行中", "http://127.0.0.1:3080", "42", "token=x", "/repo", "/log"},
		},
		{
			name:   "starting",
			status: Status{State: StateStarting, RecordedPID: 42, LogPath: "/log"},
			want:   []string{"启动中", "42", "/log"},
		},
		{
			name:   "foreign",
			status: Status{State: StateForeign, URL: "http://127.0.0.1:3080", ListenerCommand: "nginx", LogPath: "/log"},
			want:   []string{"端口被占用", "nginx", "/log"},
		},
		{
			name:   "unmanaged",
			status: Status{State: StateOrphan, URL: "http://127.0.0.1:3080", ListenerCommand: "python3", Port: 3080, ListenerPID: 9},
			want:   []string{"无法确认归属", "python3", "手动处理"},
		},
		{
			name:   "stopped",
			status: Status{State: StateStopped, LogPath: "/log"},
			want:   []string{"未运行", "/log"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := PrintStatus(&buffer, testCase.status); err != nil {
				t.Fatalf("PrintStatus: %v", err)
			}
			for _, want := range testCase.want {
				if !strings.Contains(buffer.String(), want) {
					t.Fatalf("output %q is missing %q", buffer.String(), want)
				}
			}
		})
	}
}

// TestServeExitCode pins the mapping scripts branch on: starting counts as
// success because the service exists and is managed.
func TestServeExitCode(t *testing.T) {
	cases := []struct {
		state string
		want  int
	}{
		{StateRunning, exitcode.OK},
		{StateStarting, exitcode.OK},
		{StateStopped, exitcode.NotRunning},
		{StateForeign, exitcode.NotRunning},
		{StateOrphan, exitcode.NotRunning},
	}
	for _, testCase := range cases {
		status := Status{State: testCase.state}
		if got := status.ServeExitCode(); got != testCase.want {
			t.Fatalf("ServeExitCode(%q) = %d, want %d", testCase.state, got, testCase.want)
		}
		if status.Owning() != (testCase.want == exitcode.OK) {
			t.Fatalf("Owning(%q) disagrees with ServeExitCode", testCase.state)
		}
	}
}

// TestPrintChecksLabelsEveryStatus pins that the three statuses are visually
// distinct.
func TestPrintChecksLabelsEveryStatus(t *testing.T) {
	var buffer bytes.Buffer
	err := PrintChecks(&buffer, []Check{
		{Name: "端口", Status: CheckOK, Detail: "3080 空闲"},
		{Name: "Node", Status: CheckWarn, Detail: "版本偏低"},
		{Name: "依赖", Status: CheckFail, Detail: "node_modules 不存在"},
	})
	if err != nil {
		t.Fatalf("PrintChecks: %v", err)
	}
	for _, want := range []string{"[OK  ] 端口", "[警告] Node", "[失败] 依赖"} {
		if !strings.Contains(buffer.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, buffer.String())
		}
	}
}

// TestChecksFailed pins that only failures block.
func TestChecksFailed(t *testing.T) {
	if ChecksFailed([]Check{{Name: "a", Status: CheckOK}, {Name: "b", Status: CheckWarn}}) {
		t.Fatal("warnings must not fail the diagnosis")
	}
	if !ChecksFailed([]Check{{Name: "a", Status: CheckOK}, {Name: "b", Status: CheckFail}}) {
		t.Fatal("a failure must fail the diagnosis")
	}
	if ChecksFailed(nil) {
		t.Fatal("an empty diagnosis must not fail")
	}
}

// TestHumanBytes pins the size rendering.
func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:                      "0 B",
		512:                    "512 B",
		2048:                   "2.0 KB",
		5 * 1024 * 1024:        "5.0 MB",
		3 * 1024 * 1024 * 1024: "3.0 GB",
	}
	for size, want := range cases {
		if got := humanBytes(size); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", size, got, want)
		}
	}
}

// TestDescribeFacts pins the diagnostic rendering of a process.
func TestDescribeFacts(t *testing.T) {
	if got := describeFacts(hostFacts("node server.js", "ps")); got != "node server.js" {
		t.Fatalf("describeFacts = %q", got)
	}
	if got := describeFacts(hostFacts("", "signal")); !strings.Contains(got, "signal") {
		t.Fatalf("describeFacts = %q, want the source named", got)
	}
	if got := describeFacts(hostFacts("", "")); got != "未知进程" {
		t.Fatalf("describeFacts = %q", got)
	}
}

package conformance

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
)

// scenario is one command line with the world it runs in. Anything a scenario
// needs beyond a fresh fixture is declared in setup, so a golden failure points
// at the scenario rather than at shared state.
type scenario struct {
	name     string
	args     []string
	setup    func(*fixture)
	probes   bool
	realPATH bool
	refs     bool
}

var scenarios = []scenario{
	{name: "help", args: []string{"-h"}},
	{name: "help-command", args: []string{"help", "start"}},
	{name: "help-unknown", args: []string{"help", "frobnicate"}},
	{name: "version", args: []string{"version"}},
	{name: "version-json", args: []string{"version", "--json"}},
	{name: "unknown-command", args: []string{"frobnicate"}},
	{name: "unknown-global-flag", args: []string{"--frobnicate", "status"}},
	{name: "port-out-of-range", args: []string{"--port", "0", "status"}},
	{name: "logs-zero-lines", args: []string{"logs", "-n", "0"}},
	{name: "status-fresh", args: []string{"status"}, probes: true},
	{name: "status-json-fresh", args: []string{"status", "--json"}, probes: true},
	{name: "status-named-port", args: []string{"--port", "39481", "status"}, probes: true},
	{name: "status-without-probe-tools", args: []string{"status"}},
	{name: "url-fresh", args: []string{"url"}, probes: true},
	{name: "logs-fresh", args: []string{"logs"}},
	{name: "logs-build-fresh", args: []string{"logs", "--build"}},
	{name: "doctor-fresh", args: []string{"doctor"}, probes: true},
	{name: "verbose-sources", args: []string{"-v", "status"}, probes: true},
	{name: "stop-nothing-running", args: []string{"stop"}, probes: true},
	{name: "start-without-checkout", args: []string{"start"}, probes: true},
	{name: "build-without-checkout", args: []string{"build"}, probes: true},
	{name: "update-without-checkout", args: []string{"update"}, probes: true},
	{name: "rollback-without-history", args: []string{"rollback"}, probes: true},
	{name: "status-with-config", args: []string{"status"}, probes: true, setup: statusWithConfig},
	{
		name:  "config-unknown-key",
		args:  []string{"status"},
		setup: unknownKeyConfig,
	},
	{
		name:  "config-bad-json",
		args:  []string{"status"},
		setup: brokenConfig,
	},
	{
		name:     "timeline-without-origin",
		args:     []string{"timeline"},
		setup:    checkoutWithoutOrigin,
		probes:   true,
		realPATH: true,
		refs:     true,
	},
	{
		name:     "update-local-branch",
		args:     []string{"update", "master"},
		setup:    checkoutWithoutOrigin,
		probes:   true,
		realPATH: true,
		refs:     true,
	},
}

func unknownKeyConfig(f *fixture) {
	f.mkdirState()
	f.write("config.json", "{\"port\": 3080, \"frobnicate\": true}\n")
}

func brokenConfig(f *fixture) {
	f.mkdirState()
	f.write("config.json", "{not json\n")
}

func statusWithConfig(f *fixture) {
	f.mkdirState()
	missing := filepath.Join(f.root, "missing-checkout")
	f.write("config.json", "{\"repoDir\": "+strconv.Quote(missing)+"}\n")
}

func checkoutWithoutOrigin(f *fixture) {
	f.checkout()
	f.setenv("DSH_REPO_DIR", f.repoDir)
	// The move resolves the runtime before it refuses the selector; pinning the
	// tools keeps the refusal the only thing the scenario observes.
	f.stubTool("node")
	f.stubTool("pnpm")
}

func TestConformance(t *testing.T) {
	goldens := map[string]expectation{}
	if !*update {
		goldens = loadGoldens(t)
	}
	covered := map[string]bool{}
	for _, sc := range scenarios {
		if covered[sc.name] {
			t.Fatalf("scenario %s is declared twice", sc.name)
		}
		covered[sc.name] = true
		t.Run(sc.name, func(t *testing.T) {
			f := newFixture(t)
			if sc.setup != nil {
				sc.setup(f)
			}
			got := f.run(sc.args, sc.probes, sc.realPATH)
			want := expectation{
				Exit:   got.Exit,
				Stdout: got.Stdout,
				Stderr: got.Stderr,
				Files:  f.snapshot(),
			}
			if sc.refs {
				want.Refs = f.refs()
			}
			if *update {
				goldens[sc.name] = want
				return
			}
			recorded, ok := goldens[sc.name]
			if !ok {
				t.Fatalf("no golden for this scenario; run with -update")
			}
			compare(t, recorded, want)
		})
	}
	if *update {
		// Recording under a -run filter would replace the file with the handful
		// of scenarios that ran, which silently drops every other golden. The
		// recording is only whole when every declared scenario took part.
		if len(goldens) != len(scenarios) {
			t.Fatalf("recorded %d of %d scenarios; re-record without a -run filter", len(goldens), len(scenarios))
		}
		saveGoldens(t, goldens)
		return
	}
	for name := range goldens {
		if !covered[name] {
			t.Errorf("golden %s has no scenario left", name)
		}
	}
}

// TestGoldensAreComplete keeps the recorded files parseable and free of
// scenarios that no longer run, so a stale reference cannot hide in testdata.
func TestGoldensAreComplete(t *testing.T) {
	goldens := loadGoldens(t)
	if len(goldens) != len(scenarios) {
		t.Errorf("recorded %d scenarios, declared %d", len(goldens), len(scenarios))
	}
	for _, sc := range scenarios {
		if _, ok := goldens[sc.name]; !ok {
			t.Errorf("scenario %s has no golden", sc.name)
		}
	}
}

// TestNormalizeLeavesRealContentAlone is the guard on the normalizer: it may
// replace what changes between runs and nothing else.
func TestNormalizeLeavesRealContentAlone(t *testing.T) {
	text := "错误: 缺少 pnpm，请先安装\n检查通过\n"
	if got := normalize("/tmp/whatever", text); got != text {
		t.Fatalf("normalizer changed real content:\n%s", got)
	}
	raw, err := json.Marshal(map[string]any{"pid": 1234, "version": "v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	got := normalize("/tmp/whatever", string(raw))
	if got != `{"pid":<PID>,"version":"<V>"}` {
		t.Fatalf("volatile fields survive normalization: %s", got)
	}
	step := normalize("/tmp/whatever", "步骤 解析目标: 耗时 12ms")
	if step != "步骤 解析目标: 耗时 <DUR>" {
		t.Fatalf("a step duration survives normalization: %s", step)
	}
}

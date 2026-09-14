package nodejs

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/run"
)

// The probe is how a PATH hit is identified: there is no directory name to
// trust, so the binary itself is asked. These cases pin what it accepts, what it
// refuses, and what it costs.

// TestProbeReadsTheReleaseFromEveryOutputShape pins the reader that turns
// `node -v` into a release. Anything after the version — a warning on the same
// line, a second line, a stray carriage return — is not part of it.
func TestProbeReadsTheReleaseFromEveryOutputShape(t *testing.T) {
	cases := map[string]string{
		"v24.20.0\n":           "24.20.0",
		"24.20.0\n":            "24.20.0",
		"  v24.20.0  \n":       "24.20.0",
		"v24.20.0\r\n":         "24.20.0",
		"v24.20.0 extra\n":     "24.20.0",
		"v24.20.0\nv22.19.0\n": "24.20.0",
		"v24.20.0-rc.1\n":      "24.20.0-rc.1",
	}
	for stdout, want := range cases {
		t.Run(strings.TrimSpace(stdout), func(t *testing.T) {
			m := newMachine(t)
			binary := filepath.Join(m.home, "usr", "bin", nodeBinaryName)
			m.pathHit = binary
			m.answers[versionProbe] = probeAnswer{stdout: stdout}
			m.answers[execPathProbe] = probeAnswer{stdout: binary + "\n"}

			got, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Version != want {
				t.Fatalf("Version = %q, want %q from %q", got.Version, want, stdout)
			}
		})
	}
}

// TestProbeRefusesOutputWithoutARelease pins that a binary which answers with
// something that is not a release is refused rather than used under a made-up
// version: every later decision — the gate, the record, the configuration —
// would be built on that invented value.
func TestProbeRefusesOutputWithoutARelease(t *testing.T) {
	for _, stdout := range []string{"", "\n", "   \n", "v", "v\n", "not a version\n"} {
		m := newMachine(t)
		binary := filepath.Join(m.home, "usr", "bin", nodeBinaryName)
		m.pathHit = binary
		m.answers[versionProbe] = probeAnswer{stdout: stdout}

		_, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
		failure := wantFailure(t, err)
		if failure.Err == nil {
			t.Fatalf("Resolve with %q reported no reason", stdout)
		}
		observation := failure.Observations[0]
		if observation.Path != binary || observation.Version != "" {
			t.Fatalf("observation = %+v, want it to name %q and no release", observation, binary)
		}
		// The exec-path question is not worth asking a binary that could not
		// answer the first one.
		m.wantProbes(t, versionProbe)
	}
}

// TestProbeReportsAFailedVersionProbe pins the shape of the failure when the
// binary exists but cannot run: the operator learns which file is broken and
// why, and the failure is carried as the cause rather than flattened into a
// sentence.
func TestProbeReportsAFailedVersionProbe(t *testing.T) {
	m := newMachine(t)
	binary := filepath.Join(m.home, "usr", "bin", nodeBinaryName)
	m.pathHit = binary
	m.answers[versionProbe] = probeAnswer{err: errors.New("permission denied")}

	_, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
	failure := wantFailure(t, err)
	if failure.Err == nil || !strings.Contains(failure.Err.Error(), "permission denied") {
		t.Fatalf("Err = %v, want the probe's own failure", failure.Err)
	}
	if observation := failure.Observations[0]; observation.Path != binary {
		t.Fatalf("observation = %+v, want it to name %q", observation, binary)
	}
}

// TestProbeAsksTheResolvedBinary pins that the questions are put to the binary
// PATH resolved and not to the name "node": a resolver that ran `node -v` would
// re-consult PATH, which is exactly the indirection being avoided.
func TestProbeAsksTheResolvedBinary(t *testing.T) {
	m := newMachine(t)
	binary := filepath.Join(m.home, "opt", "bin", nodeBinaryName)
	m.nodeOnPATH(binary, "24.20.0")

	asked := &recordingOutput{inner: m}
	resolver := m.resolver()
	resolver.Output = asked
	if _, err := resolver.Resolve(context.Background(), Preferences{Home: m.home}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(asked.names) != 2 {
		t.Fatalf("probes = %v, want two", asked.names)
	}
	for _, name := range asked.names {
		if name != binary {
			t.Fatalf("probed %q, want the resolved binary %q", name, binary)
		}
	}
}

// recordingOutput remembers which binary each probe was put to.
type recordingOutput struct {
	inner run.Outputer
	names []string
}

// Output implements run.Outputer.
func (r *recordingOutput) Output(ctx context.Context, cmd run.Command) (string, error) {
	r.names = append(r.names, cmd.Name)
	return r.inner.Output(ctx, cmd)
}

// TestProbeBoundsTheProbeWindow pins that the questions are asked under a
// deadline. A version-manager shim can decide to install a runtime on first use
// and block on the network; a preflight that waits forever is worse than one
// that fails, so the wait is bounded before the process is started.
func TestProbeBoundsTheProbeWindow(t *testing.T) {
	m := newMachine(t)
	m.nodeOnPATH(filepath.Join(m.home, "usr", "bin", nodeBinaryName), "24.20.0")

	deadlines := &deadlineOutput{inner: m}
	resolver := m.resolver()
	resolver.Output = deadlines
	if _, err := resolver.Resolve(context.Background(), Preferences{Home: m.home}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(deadlines.remaining) != 2 {
		t.Fatalf("deadlines = %v, want one per probe", deadlines.remaining)
	}
	for _, remaining := range deadlines.remaining {
		if remaining <= 0 || remaining > probeTimeout {
			t.Fatalf("probe deadline in %s, want it inside (0, %s]", remaining, probeTimeout)
		}
	}
}

// deadlineOutput records how long each probe was given.
type deadlineOutput struct {
	inner     run.Outputer
	remaining []time.Duration
}

// Output implements run.Outputer.
func (d *deadlineOutput) Output(ctx context.Context, cmd run.Command) (string, error) {
	if deadline, ok := ctx.Deadline(); ok {
		d.remaining = append(d.remaining, time.Until(deadline))
	} else {
		d.remaining = append(d.remaining, 0)
	}
	return d.inner.Output(ctx, cmd)
}

// TestProbeGivesUpWhenTheCallerIsCancelled pins that the caller's context
// reaches the probe: a cancelled start must not keep waiting on a node binary,
// and the failure must be reported rather than swallowed into a version.
func TestProbeGivesUpWhenTheCallerIsCancelled(t *testing.T) {
	m := newMachine(t)
	m.pathHit = filepath.Join(m.home, "usr", "bin", nodeBinaryName)

	resolver := m.resolver()
	resolver.Output = blockingOutput{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := resolver.Resolve(ctx, Preferences{Home: m.home})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled probe must be reported as a failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve did not return after its context was cancelled")
	}
}

// blockingOutput waits until its context ends, standing in for a binary that
// never answers.
type blockingOutput struct{}

// Output implements run.Outputer.
func (blockingOutput) Output(ctx context.Context, _ run.Command) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

// TestProbeFallsBackWhenExecPathIsUnusable pins the two answers that cannot be
// trusted: a relative path, and nothing at all. The binary that PATH named is
// still the best knowledge available, and no forwarding is claimed.
func TestProbeFallsBackWhenExecPathIsUnusable(t *testing.T) {
	for _, stdout := range []string{"node\n", "\n", "   \n"} {
		m := newMachine(t)
		binary := filepath.Join(m.home, "usr", "bin", nodeBinaryName)
		m.pathHit = binary
		m.answers[versionProbe] = probeAnswer{stdout: "v24.20.0\n"}
		m.answers[execPathProbe] = probeAnswer{stdout: stdout}

		got, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
		if err != nil {
			t.Fatalf("Resolve with exec path %q: %v", stdout, err)
		}
		if got.NodePath != binary || got.BinDir != filepath.Dir(binary) || got.ViaShim {
			t.Fatalf("resolved = %+v, want the PATH hit at %q with no forwarding claimed", got, binary)
		}
	}
}

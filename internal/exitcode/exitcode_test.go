package exitcode

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestOfClassifiesErrors pins the mapping every command relies on.
func TestOfClassifiesErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, OK},
		{"plain", errors.New("boom"), Failure},
		{"coded", New(Preflight, "缺少 pnpm"), Preflight},
		{"wrapped coded", fmt.Errorf("start: %w", New(LockTimeout, "busy")), LockTimeout},
		{"silent", SilentExit(NotRunning), NotRunning},
		{"wrapped silent", fmt.Errorf("status: %w", SilentExit(NotRunning)), NotRunning},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Of(testCase.err); got != testCase.want {
				t.Fatalf("Of(%v) = %d, want %d", testCase.err, got, testCase.want)
			}
		})
	}
}

// TestWrapKeepsTheInnermostClassification pins that an outer wrap cannot
// reclassify an already-classified failure.
func TestWrapKeepsTheInnermostClassification(t *testing.T) {
	err := Wrap(Failure, New(Preflight, "缺少 pnpm"))
	if got := Of(err); got != Preflight {
		t.Fatalf("Of = %d, want the preflight classification to survive", got)
	}
	if err := Wrap(Failure, nil); err != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", err)
	}
	plain := Wrap(Usage, errors.New("bad"))
	if Of(plain) != Usage {
		t.Fatalf("Of = %d, want %d", Of(plain), Usage)
	}
}

// TestErrorKeepsTheMessageAndTheCode pins that the message reaches the operator.
func TestErrorKeepsTheMessageAndTheCode(t *testing.T) {
	err := New(Usage, "未知命令 %q", "nope")
	if err.Error() != `未知命令 "nope"` {
		t.Fatalf("Error() = %q", err.Error())
	}
	if Of(fmt.Errorf("cli: %w", err)) != Usage {
		t.Fatal("the code must survive another wrap")
	}
	var coded *Error
	if !errors.As(err, &coded) || coded.Code != Usage {
		t.Fatalf("errors.As did not find the coded error: %v", err)
	}
	if !errors.Is(fmt.Errorf("outer: %w", err), err) {
		t.Fatal("errors.Is must find the wrapped error")
	}
}

// TestSilentCarriesItsCodeWithoutAMessage pins that a silent result prints
// nothing, because the command already reported its own outcome.
func TestSilentCarriesItsCodeWithoutAMessage(t *testing.T) {
	err := SilentExit(NotRunning)
	var silent *Silent
	if !errors.As(err, &silent) || silent.Code != NotRunning {
		t.Fatalf("SilentExit = %#v", err)
	}
	if err.Error() != "" {
		t.Fatalf("a silent result must print nothing, got %q", err.Error())
	}
}

// TestCancellationSurvivesACodedWrap pins the mapping the CLI relies on: a
// cancelled command exits 130 rather than the generic failure code.
func TestCancellationSurvivesACodedWrap(t *testing.T) {
	err := Wrap(Failure, fmt.Errorf("pnpm run build: %w", context.Canceled))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation must survive: %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancellation must not be confused with a deadline")
	}
}

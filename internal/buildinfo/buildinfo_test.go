package buildinfo

import (
	"runtime"
	"strings"
	"testing"
)

// TestPlatformMatchesRuntime pins that the reported platform is the one the
// binary was built for.
func TestPlatformMatchesRuntime(t *testing.T) {
	if got, want := GOOS(), runtime.GOOS; got != want {
		t.Fatalf("GOOS = %q, want %q", got, want)
	}
	want := runtime.GOOS + "/" + runtime.GOARCH
	if got := Platform(); got != want {
		t.Fatalf("Platform = %q, want %q", got, want)
	}
	if !strings.Contains(Platform(), "/") {
		t.Fatalf("Platform = %q, want a GOOS/GOARCH pair", Platform())
	}
}

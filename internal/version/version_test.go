package version

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/buildinfo"
)

// TestGetReportsTheRunningBuild pins the metadata and the platform.
func TestGetReportsTheRunningBuild(t *testing.T) {
	info := Get()
	if info.Version != Version || info.Commit != Commit || info.BuildDate != BuildDate {
		t.Fatalf("Get = %+v, want the compiled-in metadata", info)
	}
	if info.Platform != buildinfo.Platform() {
		t.Fatalf("Platform = %q, want %q", info.Platform, buildinfo.Platform())
	}
	if !strings.Contains(info.Platform, "/") {
		t.Fatalf("Platform = %q, want a GOOS/GOARCH pair", info.Platform)
	}
}

// TestStringNamesEveryField pins the human-readable line.
func TestStringNamesEveryField(t *testing.T) {
	info := Info{Version: "1.2.3", Commit: "abc1234", BuildDate: "2026-01-02T03:04:05Z", Platform: "linux/amd64"}
	text := info.String()
	for _, want := range []string{"dshctl", "1.2.3", "abc1234", "2026-01-02T03:04:05Z", "linux/amd64"} {
		if !strings.Contains(text, want) {
			t.Fatalf("String = %q, missing %q", text, want)
		}
	}
}

// TestJSONFieldNames pins the script-facing keys.
func TestJSONFieldNames(t *testing.T) {
	data, err := json.Marshal(Info{Version: "1.0.0", Commit: "c", BuildDate: "d", Platform: "darwin/arm64"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"version", "commit", "buildDate", "platform"} {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("JSON is missing %q: %s", field, data)
		}
	}
}

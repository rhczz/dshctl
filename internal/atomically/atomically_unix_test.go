//go:build unix

package atomically

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileHonoursTheRequestedPermissions pins that the mode the caller
// asked for is the mode the final file has.
//
// os.CreateTemp always creates its file with 0600, so a test that only ever
// asked for 0600 would pass even if WriteFile dropped its Chmod call entirely:
// a settings document written 0644 would silently stay owner-only, and a shared
// 0640 would come out 0600. These cases ask for bits CreateTemp never produces
// on its own, which is what makes the assertion able to fail. Windows is
// excluded because only the read-only bit is meaningful there.
func TestWriteFileHonoursTheRequestedPermissions(t *testing.T) {
	cases := []struct {
		name string
		perm os.FileMode
	}{
		{"owner only", 0o600},
		{"group readable", 0o640},
		{"world readable", 0o644},
		{"read only", 0o444},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "file.json")
			if err := WriteFile(path, []byte("{}"), testCase.perm); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if info.Mode().Perm() != testCase.perm {
				t.Fatalf("mode = %v, want %v", info.Mode().Perm(), testCase.perm)
			}
		})
	}
}

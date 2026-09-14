package nodejs

import (
	"strings"
	"testing"
)

// Version comparison decides whether an installed Node is good enough and
// whether a directory holds the pinned release, so its answer has to be a total
// order: a comparison that is not transitive can make the resolver pick a
// different installation depending on the order the directories were scanned.

// FuzzCompareIsATotalOrder pins the three properties a comparison must have:
// every value equals itself, the answer is antisymmetric, and it is transitive.
func FuzzCompareIsATotalOrder(f *testing.F) {
	for _, seed := range []string{
		"", "0", "v", "v0", "1", "1.0", "24", "24.20", "24.20.0", "24.20.1",
		"24.9.0", "24.10.0", "v24.20.0", "V24.20.0", "24.20.0-rc.1", "24.20.0+meta",
		"latest", ".", "..", "1.2.3.4", "99999999999999999999", "x", "24.x.0",
		" 24.20.0 ", "-1", "+1",
	} {
		f.Add(seed, seed, seed)
		f.Add(seed, "24.20.0", "0")
		f.Add("24.20.0", seed, "0")
		f.Add("24.20.0", "0", seed)
	}
	f.Fuzz(func(t *testing.T, a, b, c string) {
		if got := Compare(a, a); got != 0 {
			t.Fatalf("Compare(%q, %q) = %d, want 0: a value must equal itself", a, a, got)
		}
		forward := Compare(a, b)
		backward := Compare(b, a)
		if forward != -backward {
			t.Fatalf("Compare(%q, %q) = %d but Compare(%q, %q) = %d: the answer must be antisymmetric",
				a, b, forward, b, a, backward)
		}
		if forward < -1 || forward > 1 {
			t.Fatalf("Compare(%q, %q) = %d, want a sign", a, b, forward)
		}
		if Compare(a, b) >= 0 && Compare(b, c) >= 0 && Compare(a, c) < 0 {
			t.Fatalf("comparison is not transitive: %q >= %q, %q >= %q, but %q < %q",
				a, b, b, c, a, c)
		}
		// Matches is defined as "the two name the same release", so it must
		// agree with the comparison wherever both are meaningful.
		if strings.TrimSpace(a) != "" && strings.TrimSpace(b) != "" {
			if Matches(a, b) != (Compare(a, b) == 0) {
				t.Fatalf("Matches(%q, %q) disagrees with Compare", a, b)
			}
		} else if Matches(a, b) {
			t.Fatalf("Matches(%q, %q) = true for an empty version", a, b)
		}
	})
}

// FuzzParseVersionNeverInventsARelease pins the reader that turns a `node
// --version` line into a version.
//
// The answer is compared against directory names and against the pinned release,
// so a string that never appeared in the output must not come back as a version.
func FuzzParseVersionNeverInventsARelease(f *testing.F) {
	for _, seed := range []string{
		"v24.20.0\n", "24.20.0\n", "v1.0.0-pre\n", "v0.0.0\n", "", "\n",
		"v\n", "vv24.1.0\n", "not a version", "v24.20.0", "v24.20.0 extra\n",
		"v24.20.0\r\n", "  v24.20.0  \n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, output string) {
		version := ParseVersion(output)
		if version == "" {
			return
		}
		if !strings.Contains(output, version) && !strings.Contains(output, strings.TrimPrefix(version, "v")) {
			t.Fatalf("ParseVersion(%q) returned %q, which the output does not carry", output, version)
		}
		if strings.ContainsAny(version, " \t\r\n") {
			t.Fatalf("ParseVersion(%q) returned %q, which contains whitespace", output, version)
		}
	})
}

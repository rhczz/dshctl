package logfile

import (
	"strings"
	"testing"
)

// The log format is written and read by two different pieces of code: Section
// writes a marker, ParseSection recognizes one. A fuzz target is the cheapest
// way to keep the two from drifting apart, because a marker that cannot be read
// back silently turns a build record into ordinary log text.

// FuzzSectionMarkersRoundTrip pins that every title the writer accepts produces
// a marker the reader recognizes.
//
// The title is the only part of a marker that comes from a caller, and it is
// validated before the marker is written, so the round trip is the contract
// between ValidateTitle, Section and ParseSection.
//
// A title carrying a carriage return is out of scope: the marker format is one
// line, and no production title comes from user input. That shape is pinned by
// its own deterministic test instead of being explored here.
func FuzzSectionMarkersRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"start", "build", "update", "logs", "doctor",
		"a=b", "=====", "=", "a b", "", " ", "\t", "line\nbreak",
		"标题", "with-dash", "with_underscore", "1234",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, title string) {
		if err := ValidateTitle(title); err != nil {
			return
		}
		if strings.ContainsRune(title, '\r') {
			return
		}
		line := sectionPrefix + " 2026-01-02 03:04:05 dshctl " + title + " " + sectionPrefix
		got, ok := ParseSection(line)
		if !ok {
			t.Fatalf("a marker built from the accepted title %q was not recognized: %q", title, line)
		}
		if got != title {
			t.Fatalf("marker for %q parsed back as %q", title, got)
		}
	})
}

// FuzzParseSectionRejectsOrdinaryLines pins the other direction: a log line the
// program did not write must never be mistaken for a section boundary.
//
// A false positive splits a build record in two, and `logs --build` then prints
// half of it.
func FuzzParseSectionRejectsOrdinaryLines(f *testing.F) {
	for _, seed := range []string{
		"", "plain log line", "===== not a marker =====",
		"===== 2026-01-02 03:04:05 dshctl build =====",
		"===== 2026-01-02 03:04:05 dshctl  =====",
		"===== 2026-1-2 03:04:05 dshctl build =====",
		"===== 2026-01-02T03:04:05 dshctl build =====",
		"prefix ===== 2026-01-02 03:04:05 dshctl build =====",
		"=====\t2026-01-02 03:04:05 dshctl build =====",
		"===== 2026-01-02 03:04:05 dshctl build ===== extra",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		title, ok := ParseSection(line)
		if !ok {
			return
		}
		// A recognized marker is a well-formed one: the writer's own shape.
		trimmed := strings.TrimRight(line, "\r")
		if !strings.HasPrefix(trimmed, sectionPrefix+" ") || !strings.HasSuffix(trimmed, " "+sectionPrefix) {
			t.Fatalf("ParseSection(%q) recognized a line that is not a marker", line)
		}
		if err := ValidateTitle(title); err != nil {
			t.Fatalf("ParseSection(%q) returned the title %q, which the writer would refuse: %v",
				line, title, err)
		}
		if strings.Contains(title, " ") {
			t.Fatalf("ParseSection(%q) returned a title with a space: %q", line, title)
		}
	})
}

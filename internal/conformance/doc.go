// Package conformance pins the behavior a rewrite has to preserve.
//
// The v0.3 branch replaces the implementation and rewrites the test suite, so
// the old white-box tests cannot be the specification any more. What survives a
// change of architecture is the command line: exit codes, what a command prints,
// which files it writes, and which git refs it moves. This package drives the
// binary built from the working tree through a matrix of scenarios and compares
// the result against golden files recorded from v0.2.5.
//
// The goldens are the frozen reference. They are regenerated on purpose with
// `go test ./internal/conformance/ -update`; the conformance job re-derives them
// from the reference tag and fails when that regenerating produces a diff, which
// is what keeps a golden from being edited to match a behavior change.
//
// Volatile output is normalized before comparison: temporary paths, timestamps,
// pids and the build stamp. Everything else — including file contents and
// permissions — has to match byte for byte.
package conformance

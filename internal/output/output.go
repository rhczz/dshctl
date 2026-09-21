// Package output is the seam every human-facing line of a command goes through.
//
// A command has two audiences: standard output carries the result the operator
// asked for, and standard error carries what went wrong or what they should know
// without the operation stopping. Keeping both behind one type is what makes
// `--json` a property of the command rather than of each printer: a command
// asked for JSON writes the value instead of the narrative, and the warnings go
// where they always went.
//
// The log file is not an audience here. internal/logging owns it: this package
// never writes to it, and the caller that wants both repeats itself on purpose,
// because the operator's screen and the record of the run are different things.
package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// Reporter writes a command's result and its diagnostics.
type Reporter struct {
	// Out is standard output: the result, and nothing else.
	Out io.Writer
	// Err is standard error: warnings and errors.
	Err io.Writer
}

// New returns a reporter for one command's streams.
func New(out, err io.Writer) *Reporter {
	return &Reporter{Out: out, Err: err}
}

// Result writes one line of the command's result.
func (r *Reporter) Result(line string) {
	fmt.Fprintln(r.Out, line)
}

// Resultf writes one formatted line of the command's result.
func (r *Reporter) Resultf(format string, args ...any) {
	fmt.Fprintf(r.Out, format+"\n", args...)
}

// Warnf reports something the operator has to know without the operation
// stopping. The prefix marks it as advice rather than a result.
func (r *Reporter) Warnf(format string, args ...any) {
	fmt.Fprintf(r.Err, "警告: "+format+"\n", args...)
}

// Errorf writes a diagnostic whose text the caller already framed, so an error
// raised inside an operation is not double-prefixed.
func (r *Reporter) Errorf(format string, args ...any) {
	fmt.Fprintf(r.Err, format+"\n", args...)
}

// JSON writes the command's result as indented JSON.
func (r *Reporter) JSON(value any) error {
	return JSON(r.Out, value)
}

// JSON writes value to w as indented JSON. It is a function as well as a method
// because the command line renders a few values of its own, and both paths have
// to produce the same bytes.
func JSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

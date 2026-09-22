package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// runCapture runs a diagnostic command and returns its stdout.
//
// A non-zero exit status is returned as an error together with whatever output
// the tool produced, so a probe can classify the status itself instead of
// pattern-matching a message.
func runCapture(ctx context.Context, name string, args ...string) (string, int, error) {
	probeCtx, cancel := probeContext(ctx)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, name, args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = nil
	err := cmd.Run()
	status := 0
	if err != nil {
		var exit *exec.ExitError
		if ok := asExitError(err, &exit); ok {
			status = exit.ExitCode()
		} else {
			return out.String(), -1, fmt.Errorf("%s could not be executed: %w", name, err)
		}
	}
	return out.String(), status, nil
}

// asExitError reports whether err is a non-zero exit status.
func asExitError(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}

// scanLineBytes is the longest line the walk below hands to its callback.
const scanLineBytes = 4 * 1024 * 1024

// scanLines calls fn for every non-empty, trimmed line of output, stopping early
// when fn returns false.
//
// The walk deliberately does not use a bufio.Scanner. A Scanner gives up for
// good on the first token larger than its buffer, and the error is easy to miss:
// every row after one oversized line — a truncated write, a corrupt table dump,
// a tool that printed a blob — would be lost without a word. Losing rows is not
// a neutral failure here, because "the tool reported no listener" is exactly how
// a free port is recognized. An unreadable line is therefore dropped and the
// walk continues at the next line.
func scanLines(output string, fn func(line string) bool) {
	reader := bufio.NewReader(strings.NewReader(output))
	for {
		line, err := readScanLine(reader)
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			if !fn(trimmed) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// readScanLine reads one line of at most scanLineBytes, consuming the rest of a
// longer line so that the next read starts at the row after it.
//
// Returns:
//   - the line, which is empty when the line was too long to keep.
//   - the reader's terminal error, if any (io.EOF for an unterminated last line).
func readScanLine(reader *bufio.Reader) (string, error) {
	var builder strings.Builder
	oversized := false
	for {
		chunk, err := reader.ReadString('\n')
		if !oversized {
			if builder.Len()+len(chunk) > scanLineBytes {
				// Drop what was collected: a truncated row read as a whole one
				// is worse than no row at all.
				oversized = true
				builder.Reset()
			} else {
				builder.WriteString(chunk)
			}
		}
		switch {
		case err == nil:
			return builder.String(), nil
		case errors.Is(err, bufio.ErrBufferFull):
			// The line continues past the read buffer; keep consuming it.
		default:
			return builder.String(), err
		}
	}
}

// parsePIDs reads the process ids a tool printed one per line, in order.
//
// The limit is an upper bound, not a budget for one row: a caller that asks for
// no process ids is not handed one, because every pid that comes back is a pid a
// later stop may signal.
func parsePIDs(output string, limit int) []int {
	if limit <= 0 {
		return nil
	}
	// The capacity is deliberately small rather than the limit: a caller — or a
	// fuzzer — may pass an arbitrarily large bound, and reserving it would
	// allocate for a bound the output can never reach.
	pids := make([]int, 0, 2)
	scanLines(output, func(line string) bool {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return true
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			return true
		}
		pids = append(pids, pid)
		return len(pids) < limit
	})
	return pids
}

// parseElapsed converts a `ps -o etime=` value into a duration.
//
// The accepted forms are [[dd-]hh:]mm:ss, which is what both BSD and GNU ps
// print for elapsed time.
//
// Every field must be a plain non-negative number and the total must fit in a
// time.Duration. Duration arithmetic is unchecked, so an oversized field used to
// wrap into a negative elapsed time; a negative elapsed time becomes a start
// timestamp in the future, which no live process can ever match — ownership
// would then be decided by an arithmetic accident.
func parseElapsed(value string) (time.Duration, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	var days int64
	if index := strings.IndexByte(trimmed, '-'); index >= 0 {
		parsed, ok := parseElapsedField(trimmed[:index])
		if !ok {
			return 0, false
		}
		days = parsed
		trimmed = trimmed[index+1:]
	}
	parts := strings.Split(trimmed, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	numbers := make([]int64, 0, len(parts))
	for _, part := range parts {
		parsed, ok := parseElapsedField(part)
		if !ok {
			return 0, false
		}
		numbers = append(numbers, parsed)
	}
	var hours, minutes, seconds int64
	switch len(numbers) {
	case 2:
		minutes, seconds = numbers[0], numbers[1]
	case 3:
		hours, minutes, seconds = numbers[0], numbers[1], numbers[2]
	}
	if minutes > 59 || seconds > 59 {
		return 0, false
	}
	return elapsedDuration(days, hours, minutes, seconds)
}

// parseElapsedField reads one numeric field of an elapsed-time value.
//
// A field is digits only: a sign, a space left over from the split, or a value
// no day count could use is not a field ps printed.
func parseElapsedField(field string) (int64, bool) {
	text := strings.TrimSpace(field)
	if text == "" {
		return 0, false
	}
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// elapsedDuration sums the fields of an elapsed time, refusing any value that
// does not fit in a time.Duration.
//
// Both the multiplication and the addition are checked before they happen: once
// either wraps, the result is a negative duration, and a negative duration is a
// start time in the future.
func elapsedDuration(days, hours, minutes, seconds int64) (time.Duration, bool) {
	fields := [...]struct {
		value int64
		unit  time.Duration
	}{
		{days, 24 * time.Hour},
		{hours, time.Hour},
		{minutes, time.Minute},
		{seconds, time.Second},
	}
	total := time.Duration(0)
	for _, field := range fields {
		if field.value < 0 {
			return 0, false
		}
		if field.value > int64(math.MaxInt64)/int64(field.unit) {
			return 0, false
		}
		product := time.Duration(field.value) * field.unit
		if total > time.Duration(math.MaxInt64)-product {
			return 0, false
		}
		total += product
	}
	return total, true
}

// parsePortAddress reports whether a tool's "host:port" field names port.
//
// The address forms that appear in netstat and ss output are covered: "*:3080",
// "0.0.0.0:3080", "[::]:3080", "127.0.0.1:3080", and the ss wildcard "*:3080".
func parsePortAddress(field string, port int) bool {
	index := strings.LastIndexByte(field, ':')
	if index < 0 || index == len(field)-1 {
		return false
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(field[index+1:]))
	if err != nil {
		return false
	}
	return parsed == port
}

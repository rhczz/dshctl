//go:build unix

package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Listening reports which process is listening on a loopback port.
//
// Probes are tried in order of how much they can tell us: lsof names the owning
// process, ss does the same on Linux, and netstat is a last resort that may only
// be able to say "something is listening". A probe that exists but fails is
// reported as an error rather than as an empty answer, because "I could not
// look" must never be mistaken for "the port is free".
//
// A negative verdict — "nothing matched" — is only an answer from a probe that
// reads the whole table (ss, netstat). lsof answers per query and silently omits
// sockets it is not allowed to inspect, so its silence is confirmed by the table
// probes before it is believed; when lsof is the only tool installed, its word
// is the best evidence available and is taken, with the failure to confirm
// accepted as the price of a host that has nothing else.
func (h *Host) Listening(ctx context.Context, port int) (PortResult, error) {
	// The failures are kept as errors, not as text: a cancellation or a
	// permission refusal that happened inside a probe has to survive to the
	// caller, or every classification above this point sees an anonymous string.
	var failures []error
	type probeFunc func(context.Context, int) (PortResult, bool, error)
	known := map[string]probeFunc{
		"lsof":    h.listenViaLsof,
		"ss":      h.listenViaSS,
		"netstat": h.listenViaNetstat,
	}
	var probes []struct {
		name string
		run  probeFunc
	}
	unreadable := false
	for _, name := range h.tools.Port {
		run, ok := known[name]
		if !ok {
			// A configured tool this build cannot read is reported, not
			// skipped: quietly ignoring it would turn a typo in the product's
			// configuration into "the port is unused".
			failures = append(failures, fmt.Errorf("unknown probe tool %q", name))
			unreadable = true
			continue
		}
		probes = append(probes, struct {
			name string
			run  probeFunc
		}{name, run})
	}
	for _, probe := range probes {
		result, handled, err := probe.run(ctx, port)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !handled {
			continue
		}
		if result.Listening && result.PID == 0 {
			// "Something listens, but the probe that reads the whole table
			// cannot say who." The two kinds of probe read the kernel through
			// different interfaces, and a socket created a moment ago can be in
			// the table and not yet in the per-process scan — which is exactly
			// the moment a start is in, waiting for the server it has just
			// spawned. Ask the probe that *can* name owners once more before
			// reporting the owner as unknown: the alternative is refusing a
			// start over a server that is plainly its own.
			if named, ok, err := h.listenViaLsof(ctx, port); err == nil && ok && named.PID > 0 {
				return named, nil
			}
		}
		return result, nil
	}
	if len(failures) > 0 {
		// A probe that exists but could not answer: the port state is unknown.
		// An unreadable tool name is the same kind of answer as a tool that is
		// not installed — there is no way to look — so it keeps the sentinel
		// callers use to say so. The failures are joined rather than flattened
		// into a string, so a caller can still ask whether a cancellation or a
		// permission refusal is what happened.
		joined := errors.Join(failures...)
		if unreadable {
			return PortResult{}, fmt.Errorf("%w: %w", ErrUnsupported, joined)
		}
		return PortResult{}, fmt.Errorf("the state of port %d cannot be decided: %w", port, joined)
	}
	// Nothing answered and nothing failed. That is either "the first tool that
	// exists declined" or "no configured tool is installed" — and which tool
	// comes first is what tells the two apart: a per-query tool's silence is
	// taken as its word, while a table probe that says nothing did not answer at
	// all.
	if h.firstAvailableTool() == "lsof" {
		return PortResult{}, nil
	}
	return PortResult{}, fmt.Errorf("%w: no %s was found, so the state of port %d cannot be decided", ErrUnsupported, strings.Join(knownNames(h.tools.Port), "、"), port)
}

// listenViaLsof names the owning process on any Unix.
func (h *Host) listenViaLsof(ctx context.Context, port int) (PortResult, bool, error) {
	path, ok := h.tool("lsof")
	if !ok {
		return PortResult{}, false, nil
	}
	output, status, err := runCapture(ctx, path, "-nP", "-ti", fmt.Sprintf("tcp:%d", port), "-sTCP:LISTEN")
	if err != nil {
		return PortResult{}, false, err
	}
	// lsof documents status 1 as "nothing matched". Everything else is a real
	// failure and must not be read as an empty answer — including a negative
	// status, which is how a tool that was killed (a probe timeout, a signal)
	// reports itself. Treating that as "nothing matched" made an occupied port
	// look free, which is the one mistake this whole package exists to prevent.
	if status != 0 && status != 1 {
		return PortResult{}, false, fmt.Errorf("%s exited with status %d", "lsof", status)
	}
	pids := parsePIDs(output, 1)
	if len(pids) == 0 {
		// "Nothing matched" from lsof is not proof: without privileges it omits
		// sockets it cannot map to a process. Decline the answer so a probe that
		// reads the whole table has the chance to speak.
		return PortResult{}, false, nil
	}
	return PortResult{Listening: true, PID: pids[0]}, true, nil
}

// listenViaSS names the owning process on Linux without lsof.
func (h *Host) listenViaSS(ctx context.Context, port int) (PortResult, bool, error) {
	path, ok := h.tool("ss")
	if !ok {
		return PortResult{}, false, nil
	}
	output, status, err := runCapture(ctx, path, "-Hltnp", "sport", "=", ":"+strconv.Itoa(port))
	if err != nil {
		return PortResult{}, false, err
	}
	if status != 0 {
		return PortResult{}, false, fmt.Errorf("%s exited with status %d", "ss", status)
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if !parsePortAddress(fields[3], port) {
			continue
		}
		return PortResult{Listening: true, PID: parseSSPID(line)}, true, nil
	}
	return PortResult{}, true, nil
}

// listenViaNetstat detects a listener when nothing else is installed. It may not
// be able to name the process, in which case it reports PID 0.
func (h *Host) listenViaNetstat(ctx context.Context, port int) (PortResult, bool, error) {
	path, ok := h.tool("netstat")
	if !ok {
		return PortResult{}, false, nil
	}
	output, status, err := runCapture(ctx, path, netstatArgs()...)
	if err != nil {
		return PortResult{}, false, err
	}
	if status != 0 {
		return PortResult{}, false, fmt.Errorf("%s exited with status %d", "netstat", status)
	}
	if !parseNetstatListener(output, port) {
		return PortResult{}, true, nil
	}
	// netstat does not report the owning process, so the caller learns that
	// something listens but not who: that is exactly the state a start must
	// refuse to act on.
	return PortResult{Listening: true}, true, nil
}

// parseNetstatListener reports whether a TCP listener entry names port.
//
// It understands the BSD layout (`tcp4 0 0 127.0.0.1.3080 *.* LISTEN`) and the
// Linux layout (`tcp 0 0 127.0.0.1:3080 0.0.0.0:* LISTEN`). BSD netstat joins the
// host and the port with a dot, Linux with a colon, so the port is compared
// against whichever separator the line uses.
func parseNetstatListener(output string, port int) bool {
	found := false
	scanLines(output, func(line string) bool {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return true
		}
		if !strings.HasPrefix(fields[0], "tcp") {
			return true
		}
		state := fields[len(fields)-1]
		if !strings.EqualFold(state, "LISTEN") && !strings.EqualFold(state, "LISTENING") {
			return true
		}
		if !addressEndsWithPort(fields[3], port) {
			return true
		}
		found = true
		return false
	})
	return found
}

// addressEndsWithPort reports whether a netstat local-address field names port,
// accepting both the colon and the dot separator.
func addressEndsWithPort(field string, port int) bool {
	if parsePortAddress(field, port) {
		return true
	}
	index := strings.LastIndexByte(field, '.')
	if index < 0 || index == len(field)-1 {
		return false
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(field[index+1:]))
	return err == nil && parsed == port
}

// parseSSPID extracts the pid ss prints after "users:((".
//
// ss prints one entry per socket as ("name",pid=N,fd=M), and the name is
// arbitrary: a process may be called "pid=9" or live in a directory whose path
// says pid=7. Reading the first "pid=" in the line therefore attributes the
// socket to whatever the name happens to spell — and that number is a process a
// later stop may signal. The field is only trusted where it is: outside the
// quoted program name.
func parseSSPID(line string) int {
	for index := 0; index < len(line); {
		switch line[index] {
		case '"':
			// Skip the quoted program name, honouring the backslash escapes ss
			// puts in it.
			index++
			for index < len(line) && line[index] != '"' {
				if line[index] == '\\' {
					index++
				}
				index++
			}
			index++
		case 'p':
			if strings.HasPrefix(line[index:], "pid=") {
				return readPIDField(line[index+len("pid="):])
			}
			index++
		default:
			index++
		}
	}
	return 0
}

// readPIDField reads the digits that follow a pid= field, or 0 when the field
// does not name a process.
//
// A negative or overflowing value is reported as unknown rather than as a
// number assembled from a fragment: a pid is a positive integer, and a value
// this cannot read must not become a signal aimed at something else.
func readPIDField(rest string) int {
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	pid, err := strconv.Atoi(rest[:end])
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// Inspect reports what the operating system will say about a process.
//
// Unix answers through ps, which reports the elapsed time rather than the start
// timestamp. Subtracting it from the current time gives the start time in Unix
// seconds, which is all the state record's fingerprint needs.
func (h *Host) Inspect(ctx context.Context, pid int) Facts {
	facts := Facts{PID: pid}
	if pid <= 0 {
		return facts
	}
	if !h.Alive(ctx, pid) {
		return facts
	}
	facts.Alive = true

	// A direct kernel read is preferred where it exists: it needs no external
	// tool and cannot be denied by a policy that still allows signals.
	if startedAt, ok := startTimeFromProc(pid); ok {
		facts.StartedAt = startedAt
		facts.Source = "proc"
	}

	path, found := h.tool(h.tools.Process)
	if !found {
		// No ps is not evidence of anything about the process.
		facts.Source = firstNonEmpty(facts.Source, "signal")
		return facts
	}
	output, status, err := runCapture(ctx, path, "-p", strconv.Itoa(pid), "-o", "etime=", "-o", "args=")
	if err != nil {
		// The tool could not be run, which says nothing about the process.
		facts.Source = firstNonEmpty(facts.Source, "signal")
		return facts
	}
	if status != 0 {
		// ps was asked about a pid and declined: it no longer names a process we
		// can describe. Confirm existence directly rather than concluding death.
		facts.Alive = h.Alive(ctx, pid)
		facts.Source = firstNonEmpty(facts.Source, "signal")
		return facts
	}

	psFacts := parsePSFacts(output)
	if psFacts.zombie {
		// A zombie has exited and is only waiting to be collected.
		facts.Alive = false
	}
	if facts.StartedAt == 0 {
		facts.StartedAt = psFacts.startedAt
	}
	if psFacts.command != "" {
		facts.Command = psFacts.command
	}
	facts.Source = firstNonEmpty(facts.Source, "ps")
	return facts
}

// psFacts is what a `ps -o etime= -o args=` line carries.
type psFacts struct {
	startedAt int64
	command   string
	zombie    bool
}

// parsePSFacts reads one line of `ps -o etime= -o args=` output.
//
// The elapsed time has one-second granularity, so it is only a fallback for
// platforms without a direct process read.
func parsePSFacts(output string) psFacts {
	line := strings.TrimSpace(strings.SplitN(output, "\n", 2)[0])
	if line == "" {
		return psFacts{}
	}
	fields := strings.SplitN(line, " ", 2)
	var facts psFacts
	if elapsed, ok := parseElapsed(fields[0]); ok {
		facts.startedAt = time.Now().Add(-elapsed).Unix()
	}
	if len(fields) > 1 {
		facts.command = strings.TrimSpace(fields[1])
	}
	facts.zombie = isDefunct(facts.command)
	return facts
}

// isDefunct reports whether a ps command column names a zombie.
//
// Only the marker counts, and only as the whole command: ps replaces the command
// of a process that has exited with <defunct> (BSD prints it alone or after the
// bracketed name, GNU after the bracketed name), so the marker is the command.
// Matching the bare word instead made every live process whose *arguments*
// mention it — a grep for a log line, an editor with a file called defunct, a
// tool with a --defunct flag — read as a zombie, and Inspect turns a zombie into
// Alive == false: a running server reported as gone is one that never gets
// restarted.
func isDefunct(command string) bool {
	return strings.Contains(command, defunctMarker)
}

// defunctMarker is what ps prints in place of the command of a zombie.
const defunctMarker = "<defunct>"

// firstNonEmpty returns the first value that is set.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// Alive reports whether the process exists.
//
// This is a kernel query — signal 0 — and deliberately depends on nothing else.
// An earlier version answered through Inspect, which needs the ps tool, so a
// process whose PATH did not contain ps was reported as gone. That is the exact
// confusion this package exists to avoid: not being able to look is not the same
// as the process not being there.
//
// A process owned by another user answers EPERM, which still proves it exists.
// A zombie also answers successfully; the ps fallback below is what rules those
// out, and callers that started the process themselves have a better answer
// available (see detach.Process.Exited).
func (h *Host) Alive(_ context.Context, pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Signal delivers a termination request.
func (h *Host) Signal(pid int, request Request) error {
	if pid <= 0 {
		return fmt.Errorf("refusing to signal pid %d", pid)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("pid %d was not found: %w", pid, err)
	}
	signal := syscall.SIGTERM
	if request == Force {
		signal = syscall.SIGKILL
	}
	if err := process.Signal(signal); err != nil {
		return fmt.Errorf("pid %d could not be sent %v: %w", pid, signal, err)
	}
	return nil
}

// tool resolves a platform tool on PATH.
func (h *Host) tool(name string) (string, bool) {
	lookup := h.lookPath
	if lookup == nil {
		return name, true
	}
	path, err := lookup(name)
	if err != nil {
		return "", false
	}
	return path, true
}

// firstAvailableTool names the first configured tool that is installed, or an
// empty string when none is.
func (h *Host) firstAvailableTool() string {
	for _, name := range h.tools.Port {
		if _, ok := h.tool(name); ok {
			return name
		}
	}
	return ""
}

// knownNames lists the configured tools for the "cannot look" message, so the
// operator is told what was tried rather than what the build could have tried.
func knownNames(names []string) []string {
	if len(names) == 0 {
		return []string{"any port probe tool"}
	}
	return names
}

// isLinux reports whether the binary targets Linux, which decides how netstat
// formats its tables.

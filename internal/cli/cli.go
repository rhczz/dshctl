// Package cli parses the command line and dispatches to the service layer.
//
// Global flags appear before the command name; everything after the command name
// belongs to that command. The precedence is
// flag > environment variable > config file > built-in default.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/run"
	"github.com/rhczz/dshctl/internal/service"
	"github.com/rhczz/dshctl/internal/version"
)

// Env is everything a command needs from the process.
type Env struct {
	// Stdout and Stderr are the human-facing streams.
	Stdout io.Writer
	// Stderr carries warnings and errors.
	Stderr io.Writer
	// Getenv reads the environment.
	Getenv func(string) string
	// Executor runs external commands.
	Executor run.Executor
	// LookPath resolves an executable on PATH.
	LookPath func(string) (string, error)
	// Version is the running build's metadata.
	Version version.Info
	// Settings is the resolved configuration, filled before the command runs.
	Settings config.Settings
	// Verbose reports whether the operator asked for a configuration echo.
	Verbose bool
}

// Command is one subcommand.
type Command struct {
	// Name is the token that selects the command.
	Name string
	// Summary is the one-line description used in the command list.
	Summary string
	// Usage is the argument tail shown after the name, in the spelling the
	// command line accepts ("[--json]", "[-n N] [<tag>|<commit>]"). It is
	// empty for a command that takes no arguments, and it is what makes the
	// top-level help answer "how do I call this" without a second command.
	Usage string
	// Help is the full help text.
	Help string
	// Run executes the command with the arguments that followed its name.
	Run func(ctx context.Context, env *Env, args []string) error
}

// globals holds the flags accepted before the command name.
type globals struct {
	configPath  string
	repoDir     string
	nodeVersion string
	logLevel    string
	port        *int
	verbose     bool
	help        bool
	version     bool
}

// Main runs the command line and returns the process exit code.
//
// Parameters:
//   - ctx: cancellation comes from SIGINT or SIGTERM.
//   - args: arguments after the program name.
//   - stdout, stderr: standard streams.
//   - getenv: environment lookup.
//
// Returns:
//   - the process exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	env := &Env{
		Stdout:   stdout,
		Stderr:   stderr,
		Getenv:   getenv,
		Executor: run.NewRunner(),
		LookPath: run.LookPath,
		Version:  version.Get(),
	}

	parsed, rest, err := parseGlobals(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n\n", err)
		Usage(stderr)
		return exitcode.Usage
	}
	env.Verbose = parsed.verbose
	if parsed.version {
		fmt.Fprintln(stdout, env.Version.String())
		return exitcode.OK
	}

	commands := Commands()
	name := "start"
	if len(rest) > 0 {
		name = rest[0]
	}
	if name == "help" {
		return runHelp(stdout, stderr, commands, rest)
	}
	command := findCommand(commands, name)
	if command == nil {
		fmt.Fprintf(stderr, "error: unknown command %q\n\n", name)
		Usage(stderr)
		return exitcode.Usage
	}

	// Help must not touch the filesystem, so it is answered before the
	// configuration is resolved.
	if parsed.help && len(rest) == 0 {
		Usage(stdout)
		return exitcode.OK
	}
	var commandArgs []string
	if len(rest) > 1 {
		commandArgs = rest[1:]
	}
	if parsed.help || hasHelpFlag(commandArgs) {
		printCommandHelp(stdout, *command)
		return exitcode.OK
	}

	if command.Name != "version" {
		settings, err := loadSettings(parsed, getenv)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitcode.Of(err)
		}
		env.Settings = settings
		if env.Verbose {
			for _, line := range settings.Describe() {
				fmt.Fprintln(stderr, line)
			}
		}
	}

	if err := command.Run(ctx, env, commandArgs); err != nil {
		var silent *exitcode.Silent
		if errors.As(err, &silent) {
			return silent.Code
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return exitcode.Interrupted
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitcode.Of(err)
	}
	return exitcode.OK
}

// runHelp answers `dshctl help [command]`.
func runHelp(stdout, stderr io.Writer, commands []Command, rest []string) int {
	if len(rest) < 2 {
		Usage(stdout)
		return exitcode.OK
	}
	command := findCommand(commands, rest[1])
	if command == nil {
		fmt.Fprintf(stderr, "error: unknown command %q\n", rest[1])
		return exitcode.Usage
	}
	printCommandHelp(stdout, *command)
	return exitcode.OK
}

// parseGlobals extracts the flags accepted before a command name.
//
// Accept and store share one switch on purpose: each case both accepts the
// flag and carries its value out, so an accepted flag cannot silently lose its
// value — the failure mode where two hand-kept lists drift apart and an
// operator's flag is heard by nobody.
//
// Returns:
//   - the parsed global values.
//   - the remaining arguments, starting at the command name.
//   - an error naming an unknown or malformed global flag.
func parseGlobals(args []string) (globals, []string, error) {
	var parsed globals
	at := 0
	// take returns the value the current argument carries, whether it came
	// inline (`--config=/x`) or as the following argument. An empty value is
	// refused here: an empty flag would otherwise look set while deciding
	// nothing, and the layered sources below it go unreported.
	take := func(name string) (string, error) {
		arg := args[at]
		_, value, hasValue := strings.Cut(arg, "=")
		if !hasValue {
			if at+1 >= len(args) {
				return "", fmt.Errorf("flag %s needs a value", name)
			}
			at++
			value = args[at]
		}
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("flag %s cannot be empty", name)
		}
		return value, nil
	}
	for ; at < len(args); at++ {
		arg := args[at]
		if arg == "--" {
			return parsed, args[at+1:], nil
		}
		if !strings.HasPrefix(arg, "-") {
			return parsed, args[at:], nil
		}
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "-h", "--help":
			parsed.help = true
		case "-V", "--version":
			parsed.version = true
		case "-v", "--verbose":
			parsed.verbose = true
		case "--config":
			value, err := take(name)
			if err != nil {
				return parsed, nil, err
			}
			parsed.configPath = value
		case "--repo":
			value, err := take(name)
			if err != nil {
				return parsed, nil, err
			}
			parsed.repoDir = value
		case "--node":
			value, err := take(name)
			if err != nil {
				return parsed, nil, err
			}
			parsed.nodeVersion = value
		case "--port":
			value, err := take(name)
			if err != nil {
				return parsed, nil, err
			}
			port, err := strconv.Atoi(value)
			if err != nil {
				return parsed, nil, fmt.Errorf("flag --port is not a number: %q", value)
			}
			parsed.port = &port
		case "--log-level":
			value, err := take(name)
			if err != nil {
				return parsed, nil, err
			}
			parsed.logLevel = value
		default:
			return parsed, nil, fmt.Errorf("unknown global flag: %s", arg)
		}
	}
	return parsed, nil, nil
}

// loadSettings resolves the effective settings with the global flags layered on
// top of the environment and the config file.
//
// A non-empty parsed value is the whole story of "the flag was set": the parser
// refuses empty values at the boundary, so an empty field means "not passed",
// not "passed empty".
func loadSettings(parsed globals, getenv func(string) string) (config.Settings, error) {
	overrides := config.Overrides{Port: parsed.port}
	if parsed.configPath != "" {
		configPath := parsed.configPath
		overrides.ConfigPath = &configPath
	}
	if parsed.repoDir != "" {
		repoDir := parsed.repoDir
		overrides.RepoDir = &repoDir
	}
	if parsed.nodeVersion != "" {
		nodeVersion := parsed.nodeVersion
		overrides.NodeVersion = &nodeVersion
	}
	if parsed.logLevel != "" {
		logLevel := parsed.logLevel
		overrides.LogLevel = &logLevel
	}
	return config.Load(getenv, overrides)
}

// findCommand returns the command with the given name.
func findCommand(commands []Command, name string) *Command {
	for index := range commands {
		if commands[index].Name == name {
			return &commands[index]
		}
	}
	return nil
}

// hasHelpFlag reports whether a command's own arguments ask for its help.
func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

// newFlagSet builds the flag set for one command.
//
// The usage text answers the mistake that is easy to make: global flags are
// accepted before the command name only, so `dshctl status --port 4000` fails
// with "flag provided but not defined". Saying where the flag belongs turns a
// puzzling error into an obvious one.
func newFlagSet(env *Env, name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	flags.Usage = func() {
		fmt.Fprintf(env.Stderr, "usage: dshctl [global flags] %s [command flags]\n", name)
		fmt.Fprintln(env.Stderr, "global flags (--repo/--port/--node/--config/-v) go before the command name.")
		flags.PrintDefaults()
	}
	return flags
}

// parseFlags parses a command's flags and classifies failures.
//
// Returns:
//   - true when the operator asked for help, so the command must not run.
//   - a Usage error when parsing failed or the command was given arguments it
//     does not accept.
func parseFlags(flags *flag.FlagSet, args []string) (bool, error) {
	help, rest, err := parseFlagsWithArgs(flags, args)
	if err != nil {
		return false, err
	}
	if help {
		return true, nil
	}
	if len(rest) > 0 {
		return false, exitcode.Wrap(exitcode.Usage,
			fmt.Errorf("command %s takes no positional arguments: %s", flags.Name(), strings.Join(rest, " ")))
	}
	return false, nil
}

// parseFlagsWithArgs parses a command's flags and hands back the positional
// arguments, for the commands that take one (a version).
func parseFlagsWithArgs(flags *flag.FlagSet, args []string) (bool, []string, error) {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil, nil
		}
		return false, nil, exitcode.Wrap(exitcode.Usage, err)
	}
	return false, flags.Args(), nil
}

// versionSelector validates the one optional positional argument the version
// commands accept.
//
// The selector is never allowed to look like a flag: `update --help` would
// otherwise be answered by the flag parser, and any future git option spelled
// like a version would reach git as one.
func versionSelector(args []string, command string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) > 1 {
		return "", exitcode.Wrap(exitcode.Usage,
			fmt.Errorf("command %s takes one version argument: %s", command, strings.Join(args, " ")))
	}
	selector := strings.TrimSpace(args[0])
	if selector == "" || strings.HasPrefix(selector, "-") {
		return "", exitcode.Wrap(exitcode.Usage,
			fmt.Errorf("command %s's version argument is invalid: %q", command, args[0]))
	}
	return selector, nil
}

// printJSON encodes a value as two-space indented JSON — the shape every
// --json document uses.
func printJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// printCommandHelp writes one command's help.
//
// The usage line comes first and is built from the same Usage the top-level
// help lists, so "how do I call this" is answered before the details.
//
// The shell owns this rendering: a use case returns a value, and whether the
// operator asked for JSON or for the narrative decides which one is printed.
func printCommandHelp(w io.Writer, command Command) {
	fmt.Fprintf(w, "dshctl %s — %s\n\n", command.Name, command.Summary)
	fmt.Fprintf(w, "usage: dshctl [global flags] %s", command.Name)
	if command.Usage != "" {
		fmt.Fprintf(w, " %s", command.Usage)
	}
	fmt.Fprint(w, "\n\n")
	if command.Help != "" {
		fmt.Fprintln(w, strings.TrimSpace(command.Help))
	}
}

// newApp builds the application for one command.
func newApp(env *Env) *service.Service {
	application := service.New(env.Settings, service.Dependencies{
		Exec:     env.Executor,
		Emit:     service.TextEmitter{Out: env.Stdout, Err: env.Stderr},
		Version:  env.Version,
		LogLevel: env.Settings.LogLevel,
	})
	// The command line owns the process-wide lookups, so they are injected here
	// rather than read from the environment inside the app.
	application.LookPath = env.LookPath
	return application
}

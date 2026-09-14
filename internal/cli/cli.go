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
	"github.com/rhczz/dshctl/internal/paths"
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
	// Help is the full help text.
	Help string
	// Run executes the command with the arguments that followed its name.
	Run func(ctx context.Context, env *Env, args []string) error
}

// globals holds the flags accepted before the command name.
type globals struct {
	configPath  string
	configSet   bool
	repoDir     string
	repoSet     bool
	nodeVersion string
	nodeSet     bool
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
		fmt.Fprintf(stderr, "错误: %v\n\n", err)
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
		fmt.Fprintf(stderr, "错误: 未知命令 %q\n\n", name)
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
			fmt.Fprintf(stderr, "错误: %v\n", err)
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
		fmt.Fprintf(stderr, "错误: %v\n", err)
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
		fmt.Fprintf(stderr, "错误: 未知命令 %q\n", rest[1])
		return exitcode.Usage
	}
	printCommandHelp(stdout, *command)
	return exitcode.OK
}

// parseGlobals extracts the flags accepted before a command name.
//
// Returns:
//   - the parsed global values.
//   - the remaining arguments, starting at the command name.
//   - an error naming an unknown or malformed global flag.
func parseGlobals(args []string) (globals, []string, error) {
	var parsed globals
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			return parsed, args[index+1:], nil
		}
		if !strings.HasPrefix(arg, "-") {
			return parsed, args[index:], nil
		}
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "-h", "--help":
			parsed.help = true
		case "-V", "--version":
			parsed.version = true
		case "-v", "--verbose":
			parsed.verbose = true
		case "--config", "--repo", "--node", "--port":
			if !hasValue {
				if index+1 >= len(args) {
					return parsed, nil, fmt.Errorf("参数 %s 需要一个值", name)
				}
				index++
				value = args[index]
			}
			if strings.TrimSpace(value) == "" {
				return parsed, nil, fmt.Errorf("参数 %s 的值不能为空", name)
			}
			switch name {
			case "--config":
				parsed.configPath = value
				parsed.configSet = true
			case "--repo":
				parsed.repoDir = value
				parsed.repoSet = true
			case "--node":
				parsed.nodeVersion = value
				parsed.nodeSet = true
			case "--port":
				port, err := strconv.Atoi(value)
				if err != nil {
					return parsed, nil, fmt.Errorf("参数 --port 不是数字: %q", value)
				}
				parsed.port = &port
			}
		default:
			return parsed, nil, fmt.Errorf("未知的全局参数: %s", arg)
		}
	}
	return parsed, nil, nil
}

// loadSettings resolves the effective settings with the global flags layered on
// top of the environment and the config file.
func loadSettings(parsed globals, getenv func(string) string) (config.Settings, error) {
	effective := getenv
	if parsed.configSet {
		effective = func(key string) string {
			if key == paths.EnvConfigFile {
				return parsed.configPath
			}
			return getenv(key)
		}
	}
	overrides := config.Overrides{Port: parsed.port}
	if parsed.repoSet {
		repoDir := parsed.repoDir
		overrides.RepoDir = &repoDir
	}
	if parsed.nodeSet {
		nodeVersion := parsed.nodeVersion
		overrides.NodeVersion = &nodeVersion
	}
	return config.Load(effective, overrides)
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
		fmt.Fprintf(env.Stderr, "用法: dshctl [全局参数] %s [命令参数]\n", name)
		fmt.Fprintf(env.Stderr, "全局参数(--repo/--port/--node/--config/-v)须写在命令名之前。\n")
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
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil
		}
		return false, exitcode.Wrap(exitcode.Usage, err)
	}
	if flags.NArg() > 0 {
		return false, exitcode.Wrap(exitcode.Usage,
			fmt.Errorf("命令 %s 不接受位置参数: %s", flags.Name(), strings.Join(flags.Args(), " ")))
	}
	return false, nil
}

// printCommandHelp writes one command's help.
func printCommandHelp(w io.Writer, command Command) {
	fmt.Fprintf(w, "dshctl %s — %s\n\n", command.Name, command.Summary)
	if command.Help != "" {
		fmt.Fprintln(w, strings.TrimSpace(command.Help))
	}
}

// newService builds the service for one command.
func newService(env *Env) *service.Service {
	application := service.New(env.Settings, service.Dependencies{
		Exec:    env.Executor,
		Out:     env.Stdout,
		Err:     env.Stderr,
		Version: env.Version,
	})
	// The command line owns the process-wide lookups, so they are injected here
	// rather than read from the environment inside the service.
	application.LookPath = env.LookPath
	application.Getenv = env.Getenv
	return application
}

// printJSON writes a value as indented JSON.
func printJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

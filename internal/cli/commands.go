package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/service"
)

// helpExitCodes is the exit-code line the help text prints. It is a constant of
// its own so the README check can require the document and the help to agree on
// the same table: a code that exists in one and not the other is the kind of
// drift an operator only discovers from a shell script that branched wrong.
const helpExitCodes = "0,1,2,3,4,5"

// Commands returns the command registry. Adding a command means appending one
// entry here and implementing its Run.
func Commands() []Command {
	return []Command{
		{
			Name:    "start",
			Summary: "start DSH Web in the background (no install/build)",
			Help:    "Start DSH Web in the background and wait for the port, without running\ninstall/build.\n\nThe checkout, node, pnpm and the build artifacts are checked first. A port held by\nanother program refuses the start; so does a process dshctl cannot claim, because\ndshctl never ends a process it did not start.\n\nWhen the settings document names no repoDir, a successful start writes the\ncheckout it used into the document; when the document already names one, a line\nsays so and nothing is rewritten. A service that is already running only records\nthe checkout in its runtime record, without restarting.\n\nEnvironment: DSH_REPO_DIR, DSH_PORT, DSH_NODE_VERSION.\n--json: the whole run as one document (ok, the command's own result, and what it\nsaid along the way).\nExit codes: 0 success (including already running), 4 a precondition failed.",
			Run:     runStart,
		},
		{
			Name:    "stop",
			Summary: "stop DSH Web (every instance without --port)",
			Help:    "Stop DSH Web.\n\nWithout --port every service this state directory manages is stopped; with --port\nor DSH_PORT only that one. Only a process the runtime record names and whose start\ntime still matches is ended; a port held by another program is reported and left\nalone, never killed by mistake.\n\n--json: the whole run as one document (ok, the command's own result, and what it\nsaid along the way).\nExit codes: 0 success (nothing to do counts), 4 a service could not be confirmed\nas stopped because its ownership could not be established.",
			Run:     runStop,
		},
		{
			Name:    "restart",
			Summary: "restart DSH Web (every running instance without --port)",
			Help:    "Restart DSH Web. Without --port every running service of this state directory\nis restarted; with a port only that one. Stopping and starting happen under one\noperation lock, so no other dshctl operation can slip between them; a port whose\nownership cannot be established refuses the restart before anything is stopped.\n\n--json: the whole run as one document (ok, the command's own result, and what it\nsaid along the way).\nExit codes: 0 success (nothing running counts), 4 a precondition failed.",
			Run:     runRestart,
		},
		{
			Name:    "status",
			Summary: "report the running state (every instance without --port)",
			Usage:   "[--json]",
			Help:    "Report the running state. The port answers \"is anything listening\"; the runtime\nrecord answers \"is it ours\".\n\nWithout --port every service of this state directory is reported, the configured\nport first; with a port only that one.\n\n  --json   structured output for scripts (status is the configured port, ports is\n           every observed instance)\n\nExit codes: 0 running (or starting), 3 not running or the port is held by another\nprogram, 4 the port cannot be probed.",
			Run:     runStatus,
		},
		{
			Name:    "url",
			Summary: "print the token-carrying address",
			Help:    "Print the address dsh web announced last (with its token), ready to paste into a\nbrowser.\n\nWithout --port one line per running instance; with a port only that one. An\ninstance that runs but has not announced an address yet is explained on standard\nerror.\nExit codes: 0 at least one address was printed, 3 there was none.",
			Run:     runURL,
		},
		{
			Name:    "logs",
			Summary: "show the log (including build/update/rollback records)",
			Usage:   "[-n <N>] [-f|--follow] [--build]",
			Help:    "Show the log. Server output, build output and update output share one file.\n\n  -n <lines>     print the last N lines (default 200; below 1 means the default)\n  -f, --follow   keep following the output (across log rotation)\n  --build        only the last build/update/rollback record, for a failed deploy\n\nExit codes: 0 success, 1 the log file is missing or unreadable.",
			Run:     runLogs,
		},
		{
			Name:    "build",
			Summary: "run pnpm run build in the checkout",
			Help:    "Remove the residue of deleted packages, then run pnpm run build; the output is\nshown live and written to the log at the same time.\n\nWhen the settings document names no repoDir, a successful build writes this\ncheckout into it. A running service refuses the build, because it would replace\nartifacts that service is using.\n\n--json: the whole run as one document (ok, the command's own result, and what it\nsaid along the way).\nExit codes: 0 success, 1 the build failed, 4 a precondition failed (checkout,\ndependencies or artifacts missing, or a service is running).",
			Run:     runBuild,
		},
		{
			Name:    "timeline",
			Summary: "show how far this checkout is from origin/master",
			Usage:   "[--json]",
			Help:    "Compare this checkout with the remote origin/master: how far behind or ahead it\nis, the tags inside the gap, the recent commits, and the deployment history dshctl\nrecorded itself.\n\nIt runs git fetch first — the only reporting command that writes .git (remote\ntracking refs; it never touches the state directory or the worktree). When the\nfetch fails it still prints what is known locally, marks it as unconfirmed, and\nexits 4: stale remote information is never called \"up to date\".\n\n  --json   structured output for scripts\n\nExit codes: 0 normal (behind, ahead and diverged are all normal), 4 not a\ncheckout, no origin, the fetch failed, or git could not be read.",
			Run:     runTimeline,
		},
		{
			Name:    "update",
			Summary: "move to a version, rebuild, and restart what was running",
			Usage:   "[--json] [latest|<tag>|<commit>]",
			Help:    "The move is: resolve the target → stop the service (only if it was running) →\ngit switch → remove residue → pnpm install → pnpm run build → start it again.\n\nTargets:\n  dshctl update              the tip of origin/master (same as latest)\n  dshctl update latest       the same\n  dshctl update <tag>        the commit that tag points at (detached HEAD)\n  dshctl update <commit>     that commit (full or abbreviated hash)\n\nA local branch name is not a version: local master may be behind origin/master, so\nuse latest for the remote tip and a tag or hash for a specific commit.\n\nEvery check (target resolution, clean worktree) happens before the service is\nstopped; a target that is already the current version restarts nothing. A worktree\nwith tracked changes refuses the move (untracked files are left alone). When the\nswitch succeeds but install/build fails, the service stays down and dshctl\nrollback returns.\n\nWhen the settings document names no repoDir, a successful move writes this\ncheckout into it.\n\n--json: the whole run as one document (ok, the command's own result, and what it\nsaid along the way).\nExit codes: 0 success (including nothing to update), 1 the switch, install or build\nfailed, 4 a precondition failed.",
			Run:     runUpdate,
		},
		{
			Name:    "rollback",
			Summary: "return to a position dshctl deployed before, without the network",
			Usage:   "[--json] [-n <N>] [<tag>|<commit>]",
			Help:    "The move is the same as update (resolve the target → stop → git switch → remove\nresidue → install → build → start again), but the target comes from the deployment\nhistory dshctl recorded or from a version you name, and there is no network: going\nback to a known position is the firefighting path and has to work offline.\n\n  dshctl rollback            the position before the last update/rollback\n  dshctl rollback -n 3       three steps back\n  dshctl rollback <tag>      the commit that tag points at\n  dshctl rollback <commit>   that commit (full or abbreviated hash)\n\n-n and a version cannot be given together. A worktree with tracked changes refuses\nthe move (untracked files are left alone).\n\n--json: the whole run as one document (ok, the command's own result, and what it\nsaid along the way).\nExit codes: 0 success (including nothing to roll back), 1 the switch, install or\nbuild failed, 4 no history to walk, a step count out of range, an unresolvable\nversion, or a failed precondition.",
			Run:     runRollback,
		},
		{
			Name:    "doctor",
			Summary: "inspect the environment",
			Usage:   "[--json]",
			Help:    "A read-only inspection: state directory, settings document, checkout revision,\ndependencies and build artifacts, Node, pnpm, port ownership, runtime record,\noperation lock, log size, and the toolchain this binary was built with.\n\n  --json   structured output for scripts\n\nExit codes: 0 nothing blocking (warnings do not change the exit code), 1 something\nfailed.",
			Run:     runDoctor,
		},
		{
			Name:    "version",
			Summary: "print the build metadata",
			Usage:   "[--json]",
			Help:    "Print the version, the commit, the build time and the target platform. The JSON form also carries the Go toolchain and module.",
			Run:     runVersion,
		},
	}
}

// usageColumn is where a command's summary starts in the top-level help.
// Invocations longer than this put their summary on the next line, so the
// column stays readable and no CJK text has to be padded by byte count.
const usageColumn = 34

// Usage writes the top-level help.
//
// The first screen has to be enough to use the tool: what each command is
// called, which arguments it takes, what it does, and the examples for the
// first run. Details (exit codes, failure modes) stay in `dshctl help <命令>`.
func Usage(w io.Writer) {
	fmt.Fprint(w, "dshctl — manage the DeepSeek Harness Web server on this machine\n\nusage:\n  dshctl [global flags] <command> [command flags]\n  dshctl                        the same as dshctl start\n  dshctl help [command]         the full help of one command\n\n  global flags go before the command name.\n\ncommands:\n")
	for _, command := range Commands() {
		invocation := strings.TrimSpace("dshctl " + command.Name + " " + command.Usage)
		if len(invocation) <= usageColumn {
			fmt.Fprintf(w, "  %-*s  %s\n", usageColumn, invocation, command.Summary)
			continue
		}
		fmt.Fprintf(w, "  %s\n  %-*s%s\n", invocation, usageColumn+2, "", command.Summary)
	}
	fmt.Fprintf(w, "\ncommon:\n  dshctl --repo ~/projects/deepseek-harness start   first run: name the checkout and start\n  dshctl status                                     see the running state\n  dshctl url                                        get the token-carrying address\n  dshctl logs -f                                    follow the log\n  dshctl timeline                                   how far behind, and which tags\n  dshctl update                                     update to the tip of origin/master\n  dshctl update dsh-v0.1.6-alpha.2                  update to a tag\n  dshctl rollback                                   return to the previous position\n\nflags:\n  --json           JSON output for scripts (status/timeline/doctor/version)\n  -n <N>           logs: print the last N lines (default 200; below 1 means the default)\n  -f, --follow     logs: keep following (across rotation)\n  --build          logs: only the last build/update/rollback record\n  -n <N>           rollback: how many positions to walk back (default 1)\n\nglobal flags:\n  --repo <path>    override the checkout (environment DSH_REPO_DIR)\n  --port <port>    name one port (environment DSH_PORT); without it, status/stop/\n                   restart/url act on every service this state directory manages\n  --node <version> Node release for this run only (environment DSH_NODE_VERSION)\n  --config <file>  override the settings document (environment DSHCTL_CONFIG)\n  -v, --verbose    print the resolved settings and where each came from\n  -h, --help       this help\n  -V, --version    the build metadata\n\nNode:     taken from PATH by default, written into the document after the first\n          successful start; below 24.12.0 is refused\n          precedence: --node > DSH_NODE_VERSION > nodeVersion in the document > PATH\n\nstate dir: $DSHCTL_STATE_DIR or $DSH_HOME/dshctl or ~/.dsh/dshctl\nexit codes: 0 success/running, 1 failure, 2 usage or settings error, 3 not running, 4 a precondition failed, 5 lock timeout\n\none command's full help (flags, exit codes, notes): dshctl help <command>\n")
}

// runStart implements `dshctl start`.
func runStart(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "start")
	asJSON := jsonFlag(flags)
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	if *asJSON {
		return runJSON(env, "start", func(app *service.Service) (any, error) {
			return app.Start(ctx)
		}, nil)
	}
	_, err = newApp(env).Start(ctx)
	return err
}

// runStop implements `dshctl stop`.
func runStop(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "stop")
	asJSON := jsonFlag(flags)
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	if *asJSON {
		return runJSON(env, "stop", func(app *service.Service) (any, error) {
			return app.StopAll(ctx)
		}, func(result any) error {
			if stopped, ok := result.(service.StopAllResult); ok && stopped.Unverifiable {
				return exitcode.New(exitcode.Preflight, "an instance's ownership could not be established, so not every stop is confirmed")
			}
			return nil
		})
	}
	result, err := newApp(env).StopAll(ctx)
	if err != nil {
		return err
	}
	if result.Unverifiable {
		return exitcode.SilentExit(exitcode.Preflight)
	}
	return nil
}

// runRestart implements `dshctl restart`.
func runRestart(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "restart")
	asJSON := jsonFlag(flags)
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	if *asJSON {
		return runJSON(env, "restart", func(app *service.Service) (any, error) {
			return app.RestartAll(ctx)
		}, nil)
	}
	_, err = newApp(env).RestartAll(ctx)
	return err
}

// runStatus implements `dshctl status`.
func runStatus(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "status")
	asJSON := flags.Bool("json", false, "print JSON")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	statuses, err := newApp(env).Statuses(ctx)
	if err != nil {
		return err
	}
	report := service.NewStatusReport(statuses)
	if *asJSON {
		if err := printJSON(env.Stdout, report); err != nil {
			return err
		}
	} else if err := printStatuses(env.Stdout, env.Stderr, report); err != nil {
		return err
	}
	// The exit code answers the question the command was asked: the port the
	// configuration names, or the one that was named on the command line. Other
	// instances are reported beside it, never instead of it.
	if serveExitCode(report.Status) != exitcode.OK {
		return exitcode.SilentExit(exitcode.NotRunning)
	}
	return nil
}

// runURL implements `dshctl url`.
func runURL(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "url")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	report, err := newApp(env).URLReport(ctx)
	if err != nil {
		return err
	}
	if err := printURLs(env.Stdout, env.Stderr, report); err != nil {
		return err
	}
	// `url` succeeds exactly when it handed out an address, which is the promise
	// a pipeline depends on: no output and exit 3 has to mean "there is nothing
	// to talk to", or a script cannot tell a working server from a broken one.
	// A port that has no address while another instance does is named on
	// standard error rather than passed off as this command's answer.
	if len(report.Addresses) > 0 {
		return nil
	}
	if !report.Status.Owning() && !report.Status.Survivor {
		return exitcode.New(exitcode.NotRunning, "DSH Web is not running (%s), so there is no address", service.StatusSummary(report.Status))
	}
	return exitcode.SilentExit(exitcode.NotRunning)
}

// runLogs implements `dshctl logs`.
func runLogs(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "logs")
	follow := flags.Bool("f", false, "keep following the output")
	flags.BoolVar(follow, "follow", false, "keep following the output")
	lines := flags.Int("n", service.DefaultLogLines, "print the last N lines")
	buildOnly := flags.Bool("build", false, "only the last build/update/rollback record")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	if *buildOnly && *follow {
		return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--build and --follow cannot be used together"))
	}
	return newApp(env).Logs(ctx, service.LogsOptions{
		Lines:     *lines,
		Follow:    *follow,
		BuildOnly: *buildOnly,
	})
}

// runBuild implements `dshctl build`.
func runBuild(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "build")
	asJSON := jsonFlag(flags)
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	if *asJSON {
		return runJSON(env, "build", func(app *service.Service) (any, error) {
			return nil, app.RunBuild(ctx)
		}, nil)
	}
	return newApp(env).RunBuild(ctx)
}

// runTimeline implements `dshctl timeline`.
func runTimeline(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "timeline")
	asJSON := flags.Bool("json", false, "print JSON")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	report, err := newApp(env).Timeline(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		if err := printJSON(env.Stdout, report); err != nil {
			return err
		}
	} else if err := printTimeline(env.Stdout, report); err != nil {
		return err
	}
	// A failed fetch is a failed preflight, even though the locally known
	// report was printed: a script must be able to tell "confirmed against the
	// remote" from "the remote could not be consulted".
	if !report.Fetched {
		return exitcode.SilentExit(exitcode.Preflight)
	}
	return nil
}

// runUpdate implements `dshctl update [latest|<tag>|<sha>]`.
func runUpdate(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "update")
	asJSON := jsonFlag(flags)
	help, rest, err := parseFlagsWithArgs(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	target, err := versionSelector(rest, flags.Name())
	if err != nil {
		return err
	}
	if *asJSON {
		return runJSON(env, "update", func(app *service.Service) (any, error) {
			return nil, app.RunUpdate(ctx, target)
		}, nil)
	}
	return newApp(env).RunUpdate(ctx, target)
}

// runRollback implements `dshctl rollback [<tag>|<sha>] [-n <步数>]`.
func runRollback(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "rollback")
	asJSON := jsonFlag(flags)
	steps := flags.Int("n", 0, "how many positions to walk back (default 1)")
	help, rest, err := parseFlagsWithArgs(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	target, err := versionSelector(rest, flags.Name())
	if err != nil {
		return err
	}
	given := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == "n" {
			given = true
		}
	})
	if given && target != "" {
		return exitcode.Wrap(exitcode.Usage, fmt.Errorf("rollback's -n and a version cannot be used together"))
	}
	if given && *steps < 1 {
		return exitcode.Wrap(exitcode.Usage, fmt.Errorf("rollback's -n must be a positive number: %d", *steps))
	}
	if target != "" {
		if *asJSON {
			return runJSON(env, "rollback", func(app *service.Service) (any, error) {
				return nil, app.RunRollback(ctx, target, 0)
			}, nil)
		}
		return newApp(env).RunRollback(ctx, target, 0)
	}
	if !given {
		*steps = 1
	}
	if *asJSON {
		return runJSON(env, "rollback", func(app *service.Service) (any, error) {
			return nil, app.RunRollback(ctx, "", *steps)
		}, nil)
	}
	return newApp(env).RunRollback(ctx, "", *steps)
}

// runDoctor implements `dshctl doctor`.
func runDoctor(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "doctor")
	asJSON := flags.Bool("json", false, "print JSON")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	checks := newApp(env).Doctor(ctx)
	if *asJSON {
		if err := printJSON(env.Stdout, checks); err != nil {
			return err
		}
	} else if err := printChecks(env.Stdout, checks); err != nil {
		return err
	}
	if service.ChecksFailed(checks) {
		return exitcode.SilentExit(exitcode.Failure)
	}
	return nil
}

// runVersion implements `dshctl version`.
func runVersion(_ context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "version")
	asJSON := flags.Bool("json", false, "print JSON")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	if *asJSON {
		return printJSON(env.Stdout, env.Version)
	}
	_, err = fmt.Fprintln(env.Stdout, env.Version.String())
	return err
}

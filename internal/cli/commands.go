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
			Summary: i18nLine(MsgStartSummary),
			Help:    i18nLine(MsgStartHelp),
			Run:     runStart,
		},
		{
			Name:    "stop",
			Summary: i18nLine(MsgStopSummary),
			Help:    i18nLine(MsgStopHelp),
			Run:     runStop,
		},
		{
			Name:    "restart",
			Summary: i18nLine(MsgRestartSummary),
			Help:    i18nLine(MsgRestartHelp),
			Run:     runRestart,
		},
		{
			Name:    "status",
			Summary: i18nLine(MsgStatusSummary),
			Usage:   "[--json]",
			Help:    i18nLine(MsgStatusHelp),
			Run:     runStatus,
		},
		{
			Name:    "url",
			Summary: i18nLine(MsgURLSummary),
			Help:    i18nLine(MsgURLHelp),
			Run:     runURL,
		},
		{
			Name:    "logs",
			Summary: i18nLine(MsgLogsSummary),
			Usage:   "[-n <N>] [-f|--follow] [--build]",
			Help:    i18nLine(MsgLogsHelp),
			Run:     runLogs,
		},
		{
			Name:    "build",
			Summary: i18nLine(MsgBuildSummary),
			Help:    i18nLine(MsgBuildHelp),
			Run:     runBuild,
		},
		{
			Name:    "timeline",
			Summary: i18nLine(MsgTimelineSummary),
			Usage:   "[--json]",
			Help:    i18nLine(MsgTimelineHelp),
			Run:     runTimeline,
		},
		{
			Name:    "update",
			Summary: i18nLine(MsgUpdateSummary),
			Usage:   "[--json] [latest|<tag>|<commit>]",
			Help:    i18nLine(MsgUpdateHelp),
			Run:     runUpdate,
		},
		{
			Name:    "rollback",
			Summary: i18nLine(MsgRollbackSummary),
			Usage:   "[--json] [-n <N>] [<tag>|<commit>]",
			Help:    i18nLine(MsgRollbackHelp),
			Run:     runRollback,
		},
		{
			Name:    "doctor",
			Summary: i18nLine(MsgDoctorSummary),
			Usage:   "[--json]",
			Help:    i18nLine(MsgDoctorHelp),
			Run:     runDoctor,
		},
		{
			Name:    "version",
			Summary: i18nLine(MsgVersionSummary),
			Usage:   "[--json]",
			Help:    i18nLine(MsgVersionHelp),
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
	fmt.Fprint(w, i18nLine(MsgUsageHeader))
	for _, command := range Commands() {
		invocation := strings.TrimSpace("dshctl " + command.Name + " " + command.Usage)
		if len(invocation) <= usageColumn {
			fmt.Fprintf(w, "  %-*s  %s\n", usageColumn, invocation, command.Summary)
			continue
		}
		fmt.Fprintf(w, "  %s\n  %-*s%s\n", invocation, usageColumn+2, "", command.Summary)
	}
	fmt.Fprintf(w, i18nLine(MsgUsageTail), i18nLine(MsgExitCodes))
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
				return exitcode.New(exitcode.Preflight, "%s", i18nLine(MsgStopIncomplete))
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
	asJSON := flags.Bool("json", false, i18nLine(MsgFlagJSON))
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
		return exitcode.New(exitcode.NotRunning, "%s",
			i18nLine(MsgURLNotRunning, service.StatusSummary(report.Status)))
	}
	return exitcode.SilentExit(exitcode.NotRunning)
}

// runLogs implements `dshctl logs`.
func runLogs(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "logs")
	follow := flags.Bool("f", false, i18nLine(MsgFlagFollow))
	flags.BoolVar(follow, "follow", false, i18nLine(MsgFlagFollow))
	lines := flags.Int("n", service.DefaultLogLines, i18nLine(MsgFlagLines))
	buildOnly := flags.Bool("build", false, i18nLine(MsgFlagBuildOnly))
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	if *buildOnly && *follow {
		return exitcode.Wrap(exitcode.Usage, fmt.Errorf("%s", i18nLine(MsgLogsFlagConflict)))
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
	asJSON := flags.Bool("json", false, i18nLine(MsgFlagJSON))
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
	steps := flags.Int("n", 0, i18nLine(MsgFlagSteps))
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
		return exitcode.Wrap(exitcode.Usage, fmt.Errorf("%s", i18nLine(MsgRollbackConflict)))
	}
	if given && *steps < 1 {
		return exitcode.Wrap(exitcode.Usage, fmt.Errorf("%s", i18nLine(MsgRollbackSteps, *steps)))
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
	asJSON := flags.Bool("json", false, i18nLine(MsgFlagJSON))
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
	asJSON := flags.Bool("json", false, i18nLine(MsgFlagJSON))
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

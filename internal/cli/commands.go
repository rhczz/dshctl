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
const helpExitCodes = "0 成功/运行中, 1 失败, 2 用法或配置错误, 3 未运行, 4 前置检查失败, 5 锁超时"

// Commands returns the command registry. Adding a command means appending one
// entry here and implementing its Run.
func Commands() []Command {
	return []Command{
		{
			Name:    "start",
			Summary: "后台启动 DSH Web(不执行 install/build)",
			Help: `后台启动 DSH Web 并等待端口就绪，不执行 install/build。

启动前会检查仓库、node、pnpm 与构建产物。端口被其他程序占用时拒绝启动；
端口上是一个 dshctl 无法确认归属的进程时同样拒绝，dshctl 不会结束它没有
启动过的进程。

配置里没有写明 repoDir 时，成功启动会把本次使用的仓库目录写入配置；配置里
已经写明时只打印一行说明，绝不改写。服务已在运行时只记录运行记录里的仓库
目录，不会重新启动。

环境变量: DSH_REPO_DIR, DSH_PORT, DSH_NODE_VERSION。
--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功(含已在运行), 4 前置检查失败。`,
			Run: runStart,
		},
		{
			Name:    "stop",
			Summary: "停止 DSH Web(不加 --port 时停止全部实例)",
			Help: `停止 DSH Web。

不加 --port 时停止本状态目录管理的每一个服务；--port 或 DSH_PORT 指定端口时
只停止该端口上的服务。只有运行记录中记录、且启动时间仍然吻合的进程会被结束；
端口被其他程序占用时只提示，绝不误杀。

--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功(无事可做也算), 4 不点名时有服务因无法确认归属而未被停止。`,
			Run: runStop,
		},
		{
			Name:    "restart",
			Summary: "重启 DSH Web(不加 --port 时重启全部实例)",
			Help: `重启 DSH Web。不加 --port 时重启本状态目录中正在运行的每一个服务，
指定端口时只重启该端口。停与启在同一个操作锁内完成，其他 dshctl 操作无法
插入两者之间；任何一个端口无法确认归属时会在停任何服务之前拒绝。

--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功(没有运行中的实例也算), 4 前置检查失败(有实例无法确认归属)。`,
			Run: runRestart,
		},
		{
			Name:    "status",
			Summary: "查看运行状态(不加 --port 时报告全部实例)",
			Usage:   "[--json]",
			Help: `查看运行状态。端口是"有没有服务"的判据，运行记录是"是不是我们的"的判据。

不加 --port 时报告本状态目录管理的每一个服务，第一个是配置里那个端口；指定
端口时只报告该端口。

  --json   以 JSON 输出，便于脚本消费(status 为配置端口，ports 为全部实例)

退出码: 0 运行中(含启动中), 3 未运行或端口被其他进程占用, 4 端口无法探测。`,
			Run: runStatus,
		},
		{
			Name:    "url",
			Summary: "打印带 token 的访问地址",
			Help: `打印 dsh web 最近一次公布的访问地址(含 token)，可直接粘贴到浏览器。

不加 --port 时每个运行中的实例打印一行；指定端口时只打印该端口。某个实例在
运行却还没公布地址时会在标准错误上说明。
退出码: 0 至少打印了一个地址, 3 一个地址都没有。`,
			Run: runURL,
		},
		{
			Name:    "logs",
			Summary: "查看日志(含 build/update/rollback 记录)",
			Usage:   "[-n <N>] [-f|--follow] [--build]",
			Help: `查看日志。服务输出、构建输出、更新输出共用同一份日志。

  -n <行数>      打印最后 N 行(默认 200，小于 1 视为默认值)
  -f, --follow   持续跟随输出(跨日志轮转继续跟随)
  --build        只显示最近一次 build/update/rollback 记录，用于排查上次部署

退出码: 0 成功, 1 日志文件不存在或无法读取。`,
			Run: runLogs,
		},
		{
			Name:    "build",
			Summary: "在仓库内执行 pnpm run build",
			Help: `先清理已删除包的残留目录，再执行 pnpm run build；
输出实时显示并同时写入日志。

配置里没有写明 repoDir 时，构建成功后会把这个 checkout 写入配置。服务正在运行时
拒绝构建(会替换它正在使用的产物)。

--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功, 1 构建失败, 4 前置检查失败(仓库/依赖/产物缺失，或服务在运行)。`,
			Run: runBuild,
		},
		{
			Name:    "timeline",
			Summary: "查看当前版本与 origin/master 的差距",
			Usage:   "[--json]",
			Help: `对比当前 checkout 与远程 origin/master：落后/领先的提交数、差距内的
tag、最近的提交列表，以及 dshctl 自己记录过的部署历史。

执行时会先 git fetch（唯一会写 .git 远程跟踪引用的报告命令，不写状态目录、
不改工作区）。fetch 失败时仍打印本地已知状态，但明确标注“远程未确认”，
并以退出码 4 结束——绝不把过期的远程信息当作“已是最新”。

  --json   以 JSON 输出，便于脚本消费

退出码: 0 正常(落后/领先/分叉都算正常), 4 不是 checkout、没有 origin、
fetch 失败或 git 读取失败。`,
			Run: runTimeline,
		},
		{
			Name:    "update",
			Summary: "更新到指定版本并重建，自动停/启服务",
			Usage:   "[--json] [latest|<tag>|<commit>]",
			Help: `更新流程: 解析目标版本 → 停止服务(原本在运行才停) → git 切换 →
清理残留 → pnpm install → pnpm run build → 恢复启动。

目标版本:
  dshctl update              更新到 origin/master 最新(等价 latest)
  dshctl update latest       同上
  dshctl update <tag>        切换到该 tag 所在的提交(detached HEAD)
  dshctl update <commit>     切换到该 commit(支持完整或缩写 hash)

本地分支名不是版本: 本地 master 可能落后于 origin/master，要远程最新用
latest，要具体提交用 tag 或 hash。

所有检查(版本解析、工作区是否干净)都在停止服务之前完成；目标就是当前版本时
不会重启服务。工作区有已跟踪文件的未提交修改时拒绝执行(未跟踪文件不受影响)。
切换成功但 install/build 失败时服务保持停止，可用 dshctl rollback 退回。

配置里没有写明 repoDir 时，更新成功后会把这个 checkout 写入配置。

--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功(含无需更新), 1 切换/install/构建失败, 4 前置检查失败。`,
			Run: runUpdate,
		},
		{
			Name:    "rollback",
			Summary: "回退到之前部署过的位置，不联网",
			Usage:   "[--json] [-n <N>] [<tag>|<commit>]",
			Help: `回退流程与 update 相同(解析目标 → 停服 → git 切换 → 清理 → install →
构建 → 恢复启动)，但目标来自 dshctl 记录的部署历史或指定的版本，且不联网：
回到已知位置是救火路径，必须能在断网时工作。

  dshctl rollback            回到上一次 update/rollback 之前所在的位置
  dshctl rollback -n 3       回退 3 步
  dshctl rollback <tag>      回到该 tag 所在的提交
  dshctl rollback <commit>   回到该 commit(支持完整或缩写 hash)

-n 与版本参数不能同时给出。工作区有已跟踪文件的未提交修改时拒绝执行
(未跟踪文件不受影响)。

--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功(含无需回退), 1 切换/install/构建失败, 4 没有历史可退、步数越界、
版本无法解析或前置检查失败。`,
			Run: runRollback,
		},
		{
			Name:    "doctor",
			Summary: "体检环境",
			Usage:   "[--json]",
			Help: `只读体检: 状态目录、配置文件、仓库版本、依赖与构建产物、Node、pnpm、
端口归属、运行记录、操作锁、日志大小。

  --json   以 JSON 输出，便于脚本消费

退出码: 0 无阻塞项(警告不影响退出码), 1 存在失败项。`,
			Run: runDoctor,
		},
		{
			Name:    "version",
			Summary: "打印版本信息",
			Usage:   "[--json]",
			Help: `打印版本、提交、构建时间与目标平台。

  --json   以 JSON 输出`,
			Run: runVersion,
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
	fmt.Fprint(w, `dshctl — 管理本机运行的 DeepSeek Harness Web 服务

用法:
  dshctl [全局参数] <命令> [命令参数]
  dshctl                        等价于 dshctl start
  dshctl help [命令]            查看某个命令的完整帮助

  全局参数必须写在命令名之前。

命令:
`)
	for _, command := range Commands() {
		invocation := strings.TrimSpace("dshctl " + command.Name + " " + command.Usage)
		if len(invocation) <= usageColumn {
			fmt.Fprintf(w, "  %-*s  %s\n", usageColumn, invocation, command.Summary)
			continue
		}
		fmt.Fprintf(w, "  %s\n  %-*s%s\n", invocation, usageColumn+2, "", command.Summary)
	}
	fmt.Fprint(w, `
常用:
  dshctl --repo ~/projects/deepseek-harness start   第一次：指定仓库并启动
  dshctl status                                     看运行状态
  dshctl url                                        拿带 token 的访问地址
  dshctl logs -f                                    跟随日志
  dshctl timeline                                   更新前先看落后多少、有哪些 tag
  dshctl update                                     更新到 origin/master 最新
  dshctl update dsh-v0.1.6-alpha.2                  更新到指定 tag
  dshctl rollback                                   退回上一次 update/rollback 之前

参数:
  --json           以 JSON 输出，便于脚本消费(status/timeline/doctor/version)
  -n <N>           logs: 打印最后 N 行(默认 200，小于 1 视为默认值)
  -f, --follow     logs: 持续跟随输出(跨日志轮转继续跟随)
  --build          logs: 只显示最近一次 build/update/rollback 记录
  -n <N>           rollback: 回退几步(默认 1)

全局参数:
  --repo <路径>     覆盖仓库目录(环境变量 DSH_REPO_DIR)
  --port <端口>     指定端口(环境变量 DSH_PORT); status/stop/restart/url 不带它
                    时作用于本状态目录管理的全部服务
  --node <版本>     指定 Node 版本, 仅本次生效(环境变量 DSH_NODE_VERSION)
  --config <文件>   覆盖配置文件路径(环境变量 DSHCTL_CONFIG)
  -v, --verbose     打印生效配置及其来源
  -h, --help        显示帮助
  -V, --version     打印版本

Node:     默认按 PATH 解析, 首次成功启动后写入配置; 低于 24.12.0 一律拒绝
          优先级: --node > DSH_NODE_VERSION > 配置文件 nodeVersion > PATH

状态目录: $DSHCTL_STATE_DIR 或 $DSH_HOME/dshctl 或 ~/.dsh/dshctl
退出码:   `+helpExitCodes+`

每个命令的完整说明(参数、退出码、注意事项): dshctl help <命令>
`)
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
				return exitcode.New(exitcode.Preflight, "有实例无法确认归属，未能确认全部停止")
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
	asJSON := flags.Bool("json", false, "以 JSON 输出")
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
		return exitcode.New(exitcode.NotRunning,
			"DSH Web 未在运行(%s)，没有可访问的地址", service.StatusSummary(report.Status))
	}
	return exitcode.SilentExit(exitcode.NotRunning)
}

// runLogs implements `dshctl logs`.
func runLogs(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "logs")
	follow := flags.Bool("f", false, "持续跟随输出")
	flags.BoolVar(follow, "follow", false, "持续跟随输出")
	lines := flags.Int("n", service.DefaultLogLines, "打印最后 N 行")
	buildOnly := flags.Bool("build", false, "只显示最近一次 build/update/rollback 记录")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	if *buildOnly && *follow {
		return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--build 与 --follow 不能同时使用"))
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
	asJSON := flags.Bool("json", false, "以 JSON 输出")
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
	steps := flags.Int("n", 0, "回退的步数(默认 1)")
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
		return exitcode.Wrap(exitcode.Usage, fmt.Errorf("命令 rollback 的 -n 与版本参数不能同时使用"))
	}
	if given && *steps < 1 {
		return exitcode.Wrap(exitcode.Usage, fmt.Errorf("命令 rollback 的 -n 必须是正整数: %d", *steps))
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
	asJSON := flags.Bool("json", false, "以 JSON 输出")
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
	asJSON := flags.Bool("json", false, "以 JSON 输出")
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

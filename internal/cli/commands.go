package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/service"
)

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
退出码: 0 成功(含已在运行), 4 前置检查失败。`,
			Run: runStart,
		},
		{
			Name:    "stop",
			Summary: "停止 DSH Web",
			Help: `停止 DSH Web。

只有运行记录中记录、且启动时间仍然吻合的进程会被结束。端口被其他程序
占用时只提示，绝不误杀。`,
			Run: runStop,
		},
		{
			Name:    "restart",
			Summary: "重启 DSH Web",
			Help:    `在同一个操作锁内先停止再启动，其他 dshctl 操作无法插入两者之间。`,
			Run:     runRestart,
		},
		{
			Name:    "status",
			Summary: "查看运行状态",
			Help: `查看运行状态。端口是"有没有服务"的判据，运行记录是"是不是我们的"的判据。

  --json   以 JSON 输出，便于脚本消费

退出码: 0 运行中(含启动中), 3 未运行或端口被其他进程占用, 4 端口无法探测。`,
			Run: runStatus,
		},
		{
			Name:    "url",
			Summary: "打印带 token 的访问地址",
			Help:    `打印 dsh web 最近一次公布的访问地址(含 token)，可直接粘贴到浏览器。`,
			Run:     runURL,
		},
		{
			Name:    "logs",
			Summary: "查看日志(含 build/update 记录)",
			Help: `查看日志。服务输出、构建输出、更新输出共用同一份日志。

  -n <行数>      打印最后 N 行(默认 200，小于 1 视为默认值)
  -f, --follow   持续跟随输出(跨日志轮转继续跟随)
  --build        只显示最近一次 build/update 记录，用于排查上次构建失败`,
			Run: runLogs,
		},
		{
			Name:    "build",
			Summary: "在仓库内执行 pnpm run build",
			Help: `先清理已删除包的残留目录，再执行 pnpm run build；
输出实时显示并同时写入日志。

配置里没有写明 repoDir 时，构建成功后会把这个 checkout 写入配置。服务正在运行时
拒绝构建(会替换它正在使用的产物)。`,
			Run: runBuild,
		},
		{
			Name:    "update",
			Summary: "git pull + pnpm install + pnpm run build(自动停/启服务)",
			Help: `更新流程: 停止服务(原本在运行才停) → git pull --ff-only → 清理残留
→ pnpm install → pnpm run build → 恢复启动。

git pull 失败时旧构建仍然完好，会恢复启动旧版本并报告 pull 的错误；
pnpm install 或构建失败时服务保持停止，日志中保留失败原因。

配置里没有写明 repoDir 时，更新成功后会把这个 checkout 写入配置。`,
			Run: runUpdate,
		},
		{
			Name:    "doctor",
			Summary: "体检环境",
			Help: `只读体检: 状态目录、配置文件、仓库版本、依赖与构建产物、Node、pnpm、
端口归属、运行记录、操作锁、日志大小。

  --json   以 JSON 输出，便于脚本消费

退出码: 0 无阻塞项(警告不影响退出码), 1 存在失败项。`,
			Run: runDoctor,
		},
		{
			Name:    "version",
			Summary: "打印版本信息",
			Help: `打印版本、提交、构建时间与目标平台。

  --json   以 JSON 输出`,
			Run: runVersion,
		},
	}
}

// Usage writes the top-level help.
func Usage(w io.Writer) {
	fmt.Fprint(w, `dshctl — 管理本机运行的 DeepSeek Harness Web 服务

用法:
  dshctl [全局参数] <命令> [命令参数]
  dshctl                      等价于 dshctl start

命令:
`)
	for _, command := range Commands() {
		fmt.Fprintf(w, "  %-9s %s\n", command.Name, command.Summary)
	}
	fmt.Fprint(w, `
全局参数:
  --repo <路径>     覆盖仓库目录(环境变量 DSH_REPO_DIR)
  --port <端口>     覆盖监听端口(环境变量 DSH_PORT)
  --node <版本>     指定 Node 版本, 仅本次生效(环境变量 DSH_NODE_VERSION)
  --config <文件>   覆盖配置文件路径(环境变量 DSHCTL_CONFIG)
  -v, --verbose     打印生效配置及其来源
  -h, --help        显示帮助
  -V, --version     打印版本

Node:     默认按 PATH 解析, 首次成功启动后写入配置; 低于 24.12.0 一律拒绝
          优先级: --node > DSH_NODE_VERSION > 配置文件 nodeVersion > PATH

状态目录: $DSHCTL_STATE_DIR 或 $DSH_HOME/dshctl 或 ~/.dsh/dshctl
退出码:   0 成功/运行中, 1 失败, 2 用法或配置错误, 3 未运行, 4 前置检查失败, 5 锁超时
`)
}

// runStart implements `dshctl start`.
func runStart(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "start")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	_, err = newService(env).Start(ctx)
	return err
}

// runStop implements `dshctl stop`.
func runStop(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "stop")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	_, err = newService(env).Stop(ctx)
	return err
}

// runRestart implements `dshctl restart`.
func runRestart(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "restart")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	_, err = newService(env).Restart(ctx)
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
	status, err := newService(env).Status(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		if err := printJSON(env.Stdout, status); err != nil {
			return err
		}
	} else if err := service.PrintStatus(env.Stdout, status); err != nil {
		return err
	}
	if status.ServeExitCode() != exitcode.OK {
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
	address, err := newService(env).WebURL(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(env.Stdout, address)
	return err
}

// runLogs implements `dshctl logs`.
func runLogs(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "logs")
	follow := flags.Bool("f", false, "持续跟随输出")
	flags.BoolVar(follow, "follow", false, "持续跟随输出")
	lines := flags.Int("n", service.DefaultLogLines, "打印最后 N 行")
	buildOnly := flags.Bool("build", false, "只显示最近一次 build/update 记录")
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
	return newService(env).Logs(ctx, service.LogsOptions{
		Lines:     *lines,
		Follow:    *follow,
		BuildOnly: *buildOnly,
	})
}

// runBuild implements `dshctl build`.
func runBuild(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "build")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	return newService(env).RunBuild(ctx)
}

// runUpdate implements `dshctl update`.
func runUpdate(ctx context.Context, env *Env, args []string) error {
	flags := newFlagSet(env, "update")
	help, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	return newService(env).RunUpdate(ctx)
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
	checks := newService(env).Doctor(ctx)
	if *asJSON {
		if err := printJSON(env.Stdout, checks); err != nil {
			return err
		}
	} else if err := service.PrintChecks(env.Stdout, checks); err != nil {
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

package cli

import "github.com/rhczz/dshctl/internal/i18n"

// This file is the shell's word list: the text a command prints, the help it
// offers, and the errors only a command line can produce. It lives here because
// the words belong to the front-end that speaks them; internal/i18n is the
// capability (resolution, catalogs, merge, audit) and knows none of them.

// Message ids. The cli.* namespace belongs to this file.
const (
	// Status rendering.
	MsgPortHeading           = "cli.status.port-heading"
	MsgNoAddressOnPort       = "cli.url.no-address-on-port"
	MsgStatusRunningBlock    = "cli.status.running-block"
	MsgStatusTokenLine       = "cli.status.token-line"
	MsgStatusConfiguredIs    = "cli.status.configured-is"
	MsgStatusNoRecordedRepo  = "cli.status.no-recorded-checkout"
	MsgStatusCheckoutLog     = "cli.status.checkout-and-log"
	MsgStatusStartingBlock   = "cli.status.starting-block"
	MsgStatusForeignBlock    = "cli.status.foreign-block"
	MsgStatusOrphanBlock     = "cli.status.orphan-block"
	MsgStatusSurvivorHint    = "cli.status.survivor-hint"
	MsgStatusOrphanHint      = "cli.status.orphan-hint"
	MsgStatusUnobservable    = "cli.status.unobservable"
	MsgStatusRecordPid       = "cli.status.record-pid"
	MsgStatusUnobservableTip = "cli.status.unobservable-hint"
	MsgStatusGeneric         = "cli.status.generic"
	MsgStatusRecordLive      = "cli.status.record-live"
	MsgStatusStaleRecord     = "cli.status.stale-record"
	MsgStatusLog             = "cli.status.log"
	// Doctor rendering.
	MsgCheckOK   = "cli.check.ok"
	MsgCheckWarn = "cli.check.warn"
	MsgCheckFail = "cli.check.fail"
	MsgCheckLine = "cli.check.line"

	// Command summaries, shown in the top-level help.
	MsgStartSummary    = "cli.cmd.start.summary"
	MsgStopSummary     = "cli.cmd.stop.summary"
	MsgRestartSummary  = "cli.cmd.restart.summary"
	MsgStatusSummary   = "cli.cmd.status.summary"
	MsgURLSummary      = "cli.cmd.url.summary"
	MsgLogsSummary     = "cli.cmd.logs.summary"
	MsgBuildSummary    = "cli.cmd.build.summary"
	MsgTimelineSummary = "cli.cmd.timeline.summary"
	MsgUpdateSummary   = "cli.cmd.update.summary"
	MsgRollbackSummary = "cli.cmd.rollback.summary"
	MsgDoctorSummary   = "cli.cmd.doctor.summary"
	MsgVersionSummary  = "cli.cmd.version.summary"

	// Command help.
	MsgStartHelp    = "cli.cmd.start.help"
	MsgStopHelp     = "cli.cmd.stop.help"
	MsgRestartHelp  = "cli.cmd.restart.help"
	MsgStatusHelp   = "cli.cmd.status.help"
	MsgURLHelp      = "cli.cmd.url.help"
	MsgLogsHelp     = "cli.cmd.logs.help"
	MsgBuildHelp    = "cli.cmd.build.help"
	MsgTimelineHelp = "cli.cmd.timeline.help"
	MsgUpdateHelp   = "cli.cmd.update.help"
	MsgRollbackHelp = "cli.cmd.rollback.help"
	MsgDoctorHelp   = "cli.cmd.doctor.help"
	MsgVersionHelp  = "cli.cmd.version.help"

	// Flag descriptions.
	MsgFlagJSON      = "cli.flag.json"
	MsgFlagFollow    = "cli.flag.follow"
	MsgFlagLines     = "cli.flag.lines"
	MsgFlagBuildOnly = "cli.flag.build-only"
	MsgFlagSteps     = "cli.flag.steps"

	// Errors and framing.
	MsgErrorPrefix      = "cli.error.prefix"
	MsgErrorPrefixBlank = "cli.error.prefix-blank"
	MsgUnknownCommand   = "cli.error.unknown-command"
	MsgUnknownHelpTopic = "cli.error.unknown-help-topic"
	MsgGlobalNeedsValue = "cli.error.global-needs-value"
	MsgGlobalEmptyValue = "cli.error.global-empty-value"
	MsgPortNotANumber   = "cli.error.port-not-a-number"
	MsgUnknownGlobal    = "cli.error.unknown-global"
	MsgUsageLine        = "cli.usage.command-line"
	MsgUsageGlobals     = "cli.usage.globals-first"
	MsgStopIncomplete   = "cli.error.stop-incomplete"
	MsgURLNotRunning    = "cli.error.url-not-running"
	MsgLogsFlagConflict = "cli.error.logs-flag-conflict"
	MsgRollbackConflict = "cli.error.rollback-conflict"
	MsgRollbackSteps    = "cli.error.rollback-steps"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgPortHeading: {
		EN: "\nport %d:",
		ZH: "\n端口 %d:",
	},
	MsgNoAddressOnPort: {
		EN: "no address on port %d (%s)",
		ZH: "端口 %d 上没有可访问的地址(%s)",
	},
	MsgStatusRunningBlock: {
		EN: "state: running\naddress: %s\nPID:  %d",
		ZH: "状态: 运行中\n地址: %s\nPID:  %d",
	},
	MsgStatusTokenLine: {
		EN: "visit: %s",
		ZH: "访问: %s",
	},
	MsgStatusConfiguredIs: {
		EN: " (configured as %s)",
		ZH: " (配置中为 %s)",
	},
	MsgStatusNoRecordedRepo: {
		EN: " (configured value; the record names no checkout)",
		ZH: " (配置值；运行记录未记录仓库目录)",
	},
	MsgStatusCheckoutLog: {
		EN: "checkout: %s%s\nlog: %s",
		ZH: "仓库: %s%s\n日志: %s",
	},
	MsgStatusStartingBlock: {
		EN: "state: starting or stopping (the port is not ready)\nPID:  %d\nlog: %s",
		ZH: "状态: 启动中或关闭中(端口未就绪)\nPID:  %d\n日志: %s",
	},
	MsgStatusForeignBlock: {
		EN: "state: the port is held by a process dshctl did not start\naddress: %s\nprocess: %s\nlog: %s",
		ZH: "状态: 端口被占用（非 dshctl 启动的进程）\n地址: %s\n进程: %s\n日志: %s",
	},
	MsgStatusOrphanBlock: {
		EN: "state: the port is held by a process dshctl cannot claim\naddress: %s\nprocess: %s",
		ZH: "状态: 端口被一个 dshctl 无法确认归属的进程占用\n地址: %s\n进程: %s",
	},
	MsgStatusSurvivorHint: {
		EN: "hint: this is a survivor of an interrupted start; run dshctl start or dshctl stop to manage it again",
		ZH: "提示: 这是上次启动被中断后仍存活的服务;运行 dshctl start 或 dshctl stop 可恢复管理",
	},
	MsgStatusOrphanHint: {
		EN: "hint: dshctl will not end it; confirm it is safe to stop and handle it yourself",
		ZH: "提示: dshctl 不会结束它;确认可以安全停止后请手动处理",
	},
	MsgStatusUnobservable: {
		EN: "state: port %d cannot be probed: %s",
		ZH: "状态: 无法探测端口 %d 的状态: %s",
	},
	MsgStatusRecordPid: {
		EN: "record: pid=%d",
		ZH: "记录: pid=%d",
	},
	MsgStatusUnobservableTip: {
		EN: "hint: without a port probe (lsof/ss/netstat) this instance cannot be judged; fix that and retry",
		ZH: "提示: 端口探测工具(lsof/ss/netstat)不可用时无法判断该实例，请修复后重试",
	},
	MsgStatusGeneric: {
		EN: "state: %s",
		ZH: "状态: %s",
	},
	MsgStatusRecordLive: {
		EN: "record: pid=%d is still alive but is not listening on port %d; dshctl stop ends it",
		ZH: "记录: pid=%d 仍然存活，但没有监听端口 %d;可用 dshctl stop 结束它",
	},
	MsgStatusStaleRecord: {
		EN: "stale record: pid=%d is gone or has been reused",
		ZH: "陈旧记录: pid=%d 已不存在或已被复用",
	},
	MsgStatusLog: {
		EN: "log: %s",
		ZH: "日志: %s",
	},
	MsgCheckOK: {
		EN: "OK  ",
		ZH: "OK  ",
	},
	MsgCheckWarn: {
		EN: "warn",
		ZH: "警告",
	},
	MsgCheckFail: {
		EN: "fail",
		ZH: "失败",
	},
	MsgCheckLine: {
		EN: "[%s] %s: %s",
		ZH: "[%s] %s: %s",
	},
	MsgStartSummary: {
		EN: "start DSH Web in the background (no install/build)",
		ZH: "后台启动 DSH Web(不执行 install/build)",
	},
	MsgStopSummary: {
		EN: "stop DSH Web (every instance without --port)",
		ZH: "停止 DSH Web(不加 --port 时停止全部实例)",
	},
	MsgRestartSummary: {
		EN: "restart DSH Web (every running instance without --port)",
		ZH: "重启 DSH Web(不加 --port 时重启全部实例)",
	},
	MsgStatusSummary: {
		EN: "report the running state (every instance without --port)",
		ZH: "查看运行状态(不加 --port 时报告全部实例)",
	},
	MsgURLSummary: {
		EN: "print the token-carrying address",
		ZH: "打印带 token 的访问地址",
	},
	MsgLogsSummary: {
		EN: "show the log (including build/update/rollback records)",
		ZH: "查看日志(含 build/update/rollback 记录)",
	},
	MsgBuildSummary: {
		EN: "run pnpm run build in the checkout",
		ZH: "在仓库内执行 pnpm run build",
	},
	MsgTimelineSummary: {
		EN: "show how far this checkout is from origin/master",
		ZH: "查看当前版本与 origin/master 的差距",
	},
	MsgUpdateSummary: {
		EN: "move to a version, rebuild, and restart what was running",
		ZH: "更新到指定版本并重建，自动停/启服务",
	},
	MsgRollbackSummary: {
		EN: "return to a position dshctl deployed before, without the network",
		ZH: "回退到之前部署过的位置，不联网",
	},
	MsgDoctorSummary: {
		EN: "inspect the environment",
		ZH: "体检环境",
	},
	MsgVersionSummary: {
		EN: "print the build metadata",
		ZH: "打印版本信息",
	},

	MsgStartHelp: {
		EN: `Start DSH Web in the background and wait for the port, without running
install/build.

The checkout, node, pnpm and the build artifacts are checked first. A port held by
another program refuses the start; so does a process dshctl cannot claim, because
dshctl never ends a process it did not start.

When the settings document names no repoDir, a successful start writes the
checkout it used into the document; when the document already names one, a line
says so and nothing is rewritten. A service that is already running only records
the checkout in its runtime record, without restarting.

Environment: DSH_REPO_DIR, DSH_PORT, DSH_NODE_VERSION.
--json: the whole run as one document (ok, the command's own result, and what it
said along the way).
Exit codes: 0 success (including already running), 4 a precondition failed.`,
		ZH: `后台启动 DSH Web 并等待端口就绪，不执行 install/build。

启动前会检查仓库、node、pnpm 与构建产物。端口被其他程序占用时拒绝启动；
端口上是一个 dshctl 无法确认归属的进程时同样拒绝，dshctl 不会结束它没有
启动过的进程。

配置里没有写明 repoDir 时，成功启动会把本次使用的仓库目录写入配置；配置里
已经写明时只打印一行说明，绝不改写。服务已在运行时只记录运行记录里的仓库
目录，不会重新启动。

环境变量: DSH_REPO_DIR, DSH_PORT, DSH_NODE_VERSION。
--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功(含已在运行), 4 前置检查失败。`,
	},
	MsgStopHelp: {
		EN: `Stop DSH Web.

Without --port every service this state directory manages is stopped; with --port
or DSH_PORT only that one. Only a process the runtime record names and whose start
time still matches is ended; a port held by another program is reported and left
alone, never killed by mistake.

--json: the whole run as one document (ok, the command's own result, and what it
said along the way).
Exit codes: 0 success (nothing to do counts), 4 a service could not be confirmed
as stopped because its ownership could not be established.`,
		ZH: `停止 DSH Web。

不加 --port 时停止本状态目录管理的每一个服务；--port 或 DSH_PORT 指定端口时
只停止该端口上的服务。只有运行记录中记录、且启动时间仍然吻合的进程会被结束；
端口被其他程序占用时只提示，绝不误杀。

--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功(无事可做也算), 4 不点名时有服务因无法确认归属而未被停止。`,
	},
	MsgRestartHelp: {
		EN: `Restart DSH Web. Without --port every running service of this state directory
is restarted; with a port only that one. Stopping and starting happen under one
operation lock, so no other dshctl operation can slip between them; a port whose
ownership cannot be established refuses the restart before anything is stopped.

--json: the whole run as one document (ok, the command's own result, and what it
said along the way).
Exit codes: 0 success (nothing running counts), 4 a precondition failed.`,
		ZH: `重启 DSH Web。不加 --port 时重启本状态目录中正在运行的每一个服务，
指定端口时只重启该端口。停与启在同一个操作锁内完成，其他 dshctl 操作无法
插入两者之间；任何一个端口无法确认归属时会在停任何服务之前拒绝。

--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功(没有运行中的实例也算), 4 前置检查失败(有实例无法确认归属)。`,
	},
	MsgStatusHelp: {
		EN: `Report the running state. The port answers "is anything listening"; the runtime
record answers "is it ours".

Without --port every service of this state directory is reported, the configured
port first; with a port only that one.

  --json   structured output for scripts (status is the configured port, ports is
           every observed instance)

Exit codes: 0 running (or starting), 3 not running or the port is held by another
program, 4 the port cannot be probed.`,
		ZH: `查看运行状态。端口是"有没有服务"的判据，运行记录是"是不是我们的"的判据。

不加 --port 时报告本状态目录管理的每一个服务，第一个是配置里那个端口；指定
端口时只报告该端口。

  --json   以 JSON 输出，便于脚本消费(status 为配置端口，ports 为全部实例)

退出码: 0 运行中(含启动中), 3 未运行或端口被其他进程占用, 4 端口无法探测。`,
	},
	MsgURLHelp: {
		EN: `Print the address dsh web announced last (with its token), ready to paste into a
browser.

Without --port one line per running instance; with a port only that one. An
instance that runs but has not announced an address yet is explained on standard
error.
Exit codes: 0 at least one address was printed, 3 there was none.`,
		ZH: `打印 dsh web 最近一次公布的访问地址(含 token)，可直接粘贴到浏览器。

不加 --port 时每个运行中的实例打印一行；指定端口时只打印该端口。某个实例在
运行却还没公布地址时会在标准错误上说明。
退出码: 0 至少打印了一个地址, 3 一个地址都没有。`,
	},
	MsgLogsHelp: {
		EN: `Show the log. Server output, build output and update output share one file.

  -n <lines>     print the last N lines (default 200; below 1 means the default)
  -f, --follow   keep following the output (across log rotation)
  --build        only the last build/update/rollback record, for a failed deploy

Exit codes: 0 success, 1 the log file is missing or unreadable.`,
		ZH: `查看日志。服务输出、构建输出、更新输出共用同一份日志。

  -n <行数>      打印最后 N 行(默认 200，小于 1 视为默认值)
  -f, --follow   持续跟随输出(跨日志轮转继续跟随)
  --build        只显示最近一次 build/update/rollback 记录，用于排查上次部署

退出码: 0 成功, 1 日志文件不存在或无法读取。`,
	},
	MsgBuildHelp: {
		EN: `Remove the residue of deleted packages, then run pnpm run build; the output is
shown live and written to the log at the same time.

When the settings document names no repoDir, a successful build writes this
checkout into it. A running service refuses the build, because it would replace
artifacts that service is using.

--json: the whole run as one document (ok, the command's own result, and what it
said along the way).
Exit codes: 0 success, 1 the build failed, 4 a precondition failed (checkout,
dependencies or artifacts missing, or a service is running).`,
		ZH: `先清理已删除包的残留目录，再执行 pnpm run build；
输出实时显示并同时写入日志。

配置里没有写明 repoDir 时，构建成功后会把这个 checkout 写入配置。服务正在运行时
拒绝构建(会替换它正在使用的产物)。

--json: 把整次运行输出为一份文档（ok、命令自己的结果、以及运行过程中说过的话）。
退出码: 0 成功, 1 构建失败, 4 前置检查失败(仓库/依赖/产物缺失，或服务在运行)。`,
	},
	MsgTimelineHelp: {
		EN: `Compare this checkout with the remote origin/master: how far behind or ahead it
is, the tags inside the gap, the recent commits, and the deployment history dshctl
recorded itself.

It runs git fetch first — the only reporting command that writes .git (remote
tracking refs; it never touches the state directory or the worktree). When the
fetch fails it still prints what is known locally, marks it as unconfirmed, and
exits 4: stale remote information is never called "up to date".

  --json   structured output for scripts

Exit codes: 0 normal (behind, ahead and diverged are all normal), 4 not a
checkout, no origin, the fetch failed, or git could not be read.`,
		ZH: `对比当前 checkout 与远程 origin/master：落后/领先的提交数、差距内的
tag、最近的提交列表，以及 dshctl 自己记录过的部署历史。

执行时会先 git fetch（唯一会写 .git 远程跟踪引用的报告命令，不写状态目录、
不改工作区）。fetch 失败时仍打印本地已知状态，但明确标注“远程未确认”，
并以退出码 4 结束——绝不把过期的远程信息当作“已是最新”。

  --json   以 JSON 输出，便于脚本消费

退出码: 0 正常(落后/领先/分叉都算正常), 4 不是 checkout、没有 origin、
fetch 失败或 git 读取失败。`,
	},
	MsgUpdateHelp: {
		EN: `The move is: resolve the target → stop the service (only if it was running) →
git switch → remove residue → pnpm install → pnpm run build → start it again.

Targets:
  dshctl update              the tip of origin/master (same as latest)
  dshctl update latest       the same
  dshctl update <tag>        the commit that tag points at (detached HEAD)
  dshctl update <commit>     that commit (full or abbreviated hash)

A local branch name is not a version: local master may be behind origin/master, so
use latest for the remote tip and a tag or hash for a specific commit.

Every check (target resolution, clean worktree) happens before the service is
stopped; a target that is already the current version restarts nothing. A worktree
with tracked changes refuses the move (untracked files are left alone). When the
switch succeeds but install/build fails, the service stays down and dshctl
rollback returns.

When the settings document names no repoDir, a successful move writes this
checkout into it.

--json: the whole run as one document (ok, the command's own result, and what it
said along the way).
Exit codes: 0 success (including nothing to update), 1 the switch, install or build
failed, 4 a precondition failed.`,
		ZH: `更新流程: 解析目标版本 → 停止服务(原本在运行才停) → git 切换 →
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
	},
	MsgRollbackHelp: {
		EN: `The move is the same as update (resolve the target → stop → git switch → remove
residue → install → build → start again), but the target comes from the deployment
history dshctl recorded or from a version you name, and there is no network: going
back to a known position is the firefighting path and has to work offline.

  dshctl rollback            the position before the last update/rollback
  dshctl rollback -n 3       three steps back
  dshctl rollback <tag>      the commit that tag points at
  dshctl rollback <commit>   that commit (full or abbreviated hash)

-n and a version cannot be given together. A worktree with tracked changes refuses
the move (untracked files are left alone).

--json: the whole run as one document (ok, the command's own result, and what it
said along the way).
Exit codes: 0 success (including nothing to roll back), 1 the switch, install or
build failed, 4 no history to walk, a step count out of range, an unresolvable
version, or a failed precondition.`,
		ZH: `回退流程与 update 相同(解析目标 → 停服 → git 切换 → 清理 → install →
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
	},
	MsgDoctorHelp: {
		EN: `A read-only inspection: state directory, settings document, checkout revision,
dependencies and build artifacts, Node, pnpm, port ownership, runtime record,
operation lock, log size, and the toolchain this binary was built with.

  --json   structured output for scripts

Exit codes: 0 nothing blocking (warnings do not change the exit code), 1 something
failed.`,
		ZH: `只读体检: 状态目录、配置文件、仓库版本、依赖与构建产物、Node、pnpm、
端口归属、运行记录、操作锁、日志大小、以及编译这个二进制所用的工具链。

  --json   以 JSON 输出，便于脚本消费

退出码: 0 无阻塞项(警告不影响退出码), 1 存在失败项。`,
	},
	MsgVersionHelp: {
		EN: `Print the version, the commit, the build time and the target platform. ` +
			`The JSON form also carries the Go toolchain and module.`,
		ZH: `打印版本、提交、构建时间与目标平台；JSON 形式另外带上编译用的 Go 工具链与 module。`,
	},

	MsgFlagJSON: {
		EN: "print JSON",
		ZH: "以 JSON 输出",
	},
	MsgFlagFollow: {
		EN: "keep following the output",
		ZH: "持续跟随输出",
	},
	MsgFlagLines: {
		EN: "print the last N lines",
		ZH: "打印最后 N 行",
	},
	MsgFlagBuildOnly: {
		EN: "only the last build/update/rollback record",
		ZH: "只显示最近一次 build/update/rollback 记录",
	},
	MsgFlagSteps: {
		EN: "how many positions to walk back (default 1)",
		ZH: "回退的步数(默认 1)",
	},

	MsgErrorPrefix: {
		EN: "error: %v",
		ZH: "错误: %v",
	},
	MsgErrorPrefixBlank: {
		EN: "error: %v\n\n",
		ZH: "错误: %v\n\n",
	},
	MsgUnknownCommand: {
		EN: "error: unknown command %q\n\n",
		ZH: "错误: 未知命令 %q\n\n",
	},
	MsgUnknownHelpTopic: {
		EN: "error: unknown command %q\n",
		ZH: "错误: 未知命令 %q\n",
	},
	MsgGlobalNeedsValue: {
		EN: "flag %s needs a value",
		ZH: "参数 %s 需要一个值",
	},
	MsgGlobalEmptyValue: {
		EN: "flag %s cannot be empty",
		ZH: "参数 %s 的值不能为空",
	},
	MsgPortNotANumber: {
		EN: "flag --port is not a number: %q",
		ZH: "参数 --port 不是数字: %q",
	},
	MsgUnknownGlobal: {
		EN: "unknown global flag: %s",
		ZH: "未知的全局参数: %s",
	},
	MsgUsageLine: {
		EN: "usage: dshctl [global flags] %s [command flags]",
		ZH: "用法: dshctl [全局参数] %s [命令参数]",
	},
	MsgUsageGlobals: {
		EN: "global flags (--repo/--port/--node/--config/-v) go before the command name.",
		ZH: "全局参数(--repo/--port/--node/--config/-v)须写在命令名之前。",
	},
	MsgStopIncomplete: {
		EN: "an instance's ownership could not be established, so not every stop is confirmed",
		ZH: "有实例无法确认归属，未能确认全部停止",
	},
	MsgURLNotRunning: {
		EN: "DSH Web is not running (%s), so there is no address",
		ZH: "DSH Web 未在运行(%s)，没有可访问的地址",
	},
	MsgLogsFlagConflict: {
		EN: "--build and --follow cannot be used together",
		ZH: "--build 与 --follow 不能同时使用",
	},
	MsgRollbackConflict: {
		EN: "rollback's -n and a version cannot be used together",
		ZH: "命令 rollback 的 -n 与版本参数不能同时使用",
	},
	MsgRollbackSteps: {
		EN: "rollback's -n must be a positive number: %d",
		ZH: "命令 rollback 的 -n 必须是正整数: %d",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

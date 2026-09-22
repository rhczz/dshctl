package service

import "github.com/rhczz/dshctl/internal/i18n"

// This file is the kernel's word list: every sentence about an instance the
// engine can say, in every language this build speaks.
//
// It lives here rather than in internal/i18n on purpose. That package is the
// capability — resolution, the catalog type, merge, audit — and this is the
// resource: the words the lifecycle knows how to say. A front-end adds its own
// catalog the same way (a front-end declares its own ids under its own
// namespace), and the shell merges them at startup.

// Message ids. The state.* namespace belongs to this file; a front-end uses its
// own (cli.*, http.*) so a merge cannot collide.
const (
	// The one-line description of an observed state, used by status, stop and
	// url so they describe the same observation in the same words.
	MsgStateRunning      = "state.running"
	MsgStateStarting     = "state.starting"
	MsgStateOccupied     = "state.occupied"
	MsgStateSurvivor     = "state.survivor"
	MsgStateUnmanaged    = "state.unmanaged"
	MsgStateUnobservable = "state.unobservable"
	MsgStateNotListening = "state.not-listening"
	MsgStateStopped      = "state.stopped"

	MsgAdoptUnrecorded          = "service.start.adopt-unrecorded"
	MsgAdoptRecovered           = "service.start.adopt-recovered"
	MsgAlreadyRunning           = "service.start.already-running"
	MsgStarting                 = "service.start.starting"
	MsgPortForeign              = "service.start.port-foreign"
	MsgPortUnclaimed            = "service.start.port-unclaimed"
	MsgPortUnclaimedTip         = "service.start.port-unclaimed-tip"
	MsgRecordLiveElsewhere      = "service.start.record-live-elsewhere"
	MsgRecordLiveElsewhereTip   = "service.start.record-live-elsewhere-tip"
	MsgAdoptFailed              = "service.start.adopt-failed"
	MsgLaunching                = "service.start.launching"
	MsgStartFailed              = "service.start.failed"
	MsgCloseLogFailed           = "service.start.close-log-failed"
	MsgRecordWrapperFailed      = "service.start.record-wrapper-failed"
	MsgFingerprintUnreadable    = "service.start.fingerprint-unreadable"
	MsgFingerprintUnreadableTip = "service.start.fingerprint-unreadable-tip"
	MsgListenerGoneEarly        = "service.start.listener-gone"
	MsgRecordUpdateFailed       = "service.start.record-update-failed"
	MsgListenerGoneAfter        = "service.start.listener-gone-after"
	MsgStartSucceeded           = "service.start.succeeded"
	MsgStartAnnounced           = "service.start.announced"
	MsgConfigWriteFailed        = "service.start.config-write-failed"
	MsgConfigWroteRepo          = "service.start.config-wrote-repo"
	MsgConfigWroteNode          = "service.start.config-wrote-node"
	MsgRunningOtherCheckout     = "service.start.running-other-checkout"
	MsgUsingRepoOverride        = "service.start.using-repo-override"
	MsgEnvRepoOverride          = "service.start.env-repo-override"
	MsgUsingNodeOverride        = "service.start.using-node-override"
	MsgEnvNodeOverride          = "service.start.env-node-override"
	MsgCleaningUp               = "service.start.cleaning-up"
	MsgCleanupDone              = "service.start.cleanup-done"
	MsgCleanupPortBusy          = "service.start.cleanup-port-busy"
	MsgWithLog                  = "service.start.with-log"
	MsgGroupSurvivedForce       = "service.start.group-survived-force"
	MsgStartLogTooLarge         = "service.start.log-too-large-url"
	MsgStopAdoptFailed          = "service.stop.adopt-failed"
	MsgStopAdopted              = "service.stop.adopted"
	MsgStopForeignSkipped       = "service.stop.foreign-skipped"
	MsgStopUnclaimedSkipped     = "service.stop.unclaimed-skipped"
	MsgStopUnclaimedTip         = "service.stop.unclaimed-tip"
	MsgStopNotRunning           = "service.stop.not-running"
	MsgStopStopping             = "service.stop.stopping"
	MsgStopStrangerOnPort       = "service.stop.stranger-on-port"
	MsgStopStopped              = "service.stop.stopped"
	MsgStopRecycled             = "service.stop.recycled"
	MsgStopRecycledForce        = "service.stop.recycled-force"
	MsgStopSurvivedForce        = "service.stop.survived-force"
	MsgStopRecordKept           = "service.stop.record-kept"
	MsgRestartOccupant          = "service.stop.restart-occupant"
	MsgRowStateDir              = "service.doctor.row.state-dir"
	MsgRowConfig                = "service.doctor.row.config"
	MsgRowRepo                  = "service.doctor.row.repo"
	MsgRowRepoVersion           = "service.doctor.row.repo-version"
	MsgRowDeps                  = "service.doctor.row.deps"
	MsgRowArtifacts             = "service.doctor.row.artifacts"
	MsgRowNode                  = "service.doctor.row.node"
	MsgRowPnpm                  = "service.doctor.row.pnpm"
	MsgRowPort                  = "service.doctor.row.port"
	MsgRowRecord                = "service.doctor.row.record"
	MsgRowServiceRepo           = "service.doctor.row.service-repo"
	MsgRowLock                  = "service.doctor.row.lock"
	MsgRowLog                   = "service.doctor.row.log"
	MsgRowDetach                = "service.doctor.row.detach"
	MsgRowBuildInfo             = "service.doctor.row.build-info"
	MsgDirNotCreated            = "service.doctor.state-dir-not-created"
	MsgConfigNotCreated         = "service.doctor.config-not-created"
	MsgRepoMissingDetail        = "service.doctor.repo-missing"
	MsgRepoNotGitDetail         = "service.doctor.repo-not-git"
	MsgRepoNotCheckoutDetail    = "service.doctor.repo-not-checkout"
	MsgRepoDirty                = "service.doctor.repo-dirty"
	MsgDepsInstalled            = "service.doctor.deps-installed"
	MsgDepsMissing              = "service.doctor.deps-missing"
	MsgArtifactsMissing         = "service.doctor.artifacts-missing"
	MsgNodeViaShim              = "service.doctor.node-via-shim"
	MsgPnpmNotRunnable          = "service.doctor.pnpm-not-runnable"
	MsgStatusUnobservableDoctor = "service.doctor.unobservable"
	MsgPortOwnedRunning         = "service.doctor.port-owned-running"
	MsgPortOwnedStarting        = "service.doctor.port-owned-starting"
	MsgPortForeignDoctor        = "service.doctor.port-foreign"
	MsgPortSurvivorDoctor       = "service.doctor.port-survivor"
	MsgPortUnclaimedDoctor      = "service.doctor.port-unclaimed"
	MsgPortFree                 = "service.doctor.port-free"
	MsgRecordCorrupt            = "service.doctor.record-corrupt"
	MsgRecordStaleDoctor        = "service.doctor.record-stale"
	MsgRecordMissing            = "service.doctor.record-missing"
	MsgServiceOtherRepo         = "service.doctor.service-other-repo"
	MsgLockFree                 = "service.doctor.lock-free"
	MsgLockHeldBy               = "service.doctor.lock-held"
	MsgLockUnreadable           = "service.doctor.lock-unreadable"
	MsgRecordUnreadable         = "service.repo.record-unreadable"
	MsgPortServiceUnconfirmable = "service.repo.port-unconfirmable"
	MsgPruneRemoved             = "service.build.prune-removed"
	MsgBuildRepoMissing         = "service.build.repo-missing"
	MsgBuildNotCheckout         = "service.build.not-checkout"
	MsgBuildNodeModules         = "service.build.node-modules"
	MsgBuildSection             = "service.build.section"
	MsgBuildRunningShort        = "service.build.running-short"
	MsgUpdateSection            = "service.update.section"
	MsgRollbackSection          = "service.rollback.section"
	MsgUpdateVerb               = "service.update.verb"
	MsgRollbackVerb             = "service.rollback.verb"
	MsgUpdateAdoptFailed        = "service.update.adopt-failed"
	MsgUpdateAdopted            = "service.update.adopted"
	MsgUpdateOccupant           = "service.update.occupant"
	MsgUpdateRepoMissing        = "service.update.repo-missing"
	MsgUpdateNotGit             = "service.update.not-git"
	MsgUpdateNotCheckout        = "service.update.not-checkout"
	MsgUpdateResolveTarget      = "service.update.resolve-target"
	MsgUpdateNoOp               = "service.update.no-op"
	MsgUpdateUnverifiable       = "service.update.unverifiable"
	MsgUpdateUnverifiableTip    = "service.update.unverifiable-tip"
	MsgUpdateStopping           = "service.update.stopping"
	MsgUpdateSwitchFailed       = "service.update.switch-failed"
	MsgUpdateRestore            = "service.update.restore"
	MsgUpdateRestoreFailed      = "service.update.restore-failed"
	MsgUpdateFailed             = "service.update.failed"
	MsgInstallSection           = "service.update.install-section"
	MsgInstallFailed            = "service.update.install-failed"
	MsgInstallFailedNote        = "service.update.install-failed-note"
	MsgInstallSucceeded         = "service.update.install-succeeded"
	MsgBuildFailedNote          = "service.update.build-failed-note"
	MsgBuildSucceededNote       = "service.update.build-succeeded-note"
	MsgMoveDone                 = "service.update.done"
	MsgUpdateRestarting         = "service.update.restarting"
	MsgUpdateNoOriginLatest     = "service.update.no-origin-latest"
	MsgUpdateFetchFailed        = "service.update.fetch-failed"
	MsgHistoryUnreadable        = "service.update.history-unreadable"
	MsgHistoryUnreadableTip     = "service.update.history-unreadable-tip"
	MsgNoHistory                = "service.update.no-history"
	MsgNoHistoryTip             = "service.update.no-history-tip"
	MsgNoHistorySteps           = "service.update.no-history-steps"
	MsgRecordedPosition         = "service.update.recorded-position"
	MsgTargetOutsideOrigin      = "service.update.target-outside-origin"
	MsgHistoryRebuild           = "service.update.history-rebuild"
	MsgHistoryWriteFailed       = "service.update.history-write-failed"
	MsgShutdownMessage          = "service.update.shutdown-message"
	MsgLogsBuildMissing         = "service.logs.build-missing"
	MsgTimelineNotGit           = "service.timeline.not-git"
	MsgTimelineFetchFailed      = "service.timeline.fetch-failed"
	MsgTimelineHistoryRead      = "service.timeline.history-read"
	MsgPortsRestartOccupant     = "service.ports.restart-occupant"
	// Lifecycle and diagnostics (service.* namespace).
	MsgRepoMissing           = "service.repo.missing"
	MsgRepoNotCheckout       = "service.repo.not-checkout"
	MsgRepoNotGit            = "service.repo.not-git"
	MsgRepoNoOrigin          = "service.repo.no-origin"
	MsgRepoNoOriginLatest    = "service.repo.no-origin-latest"
	MsgRepoNotBuilt          = "service.repo.not-built"
	MsgRepoTrackedChanges    = "service.repo.tracked-changes"
	MsgRepoUsedByServers     = "service.repo.used-by-servers"
	MsgRepoUsedByOtherPorts  = "service.repo.used-by-other-ports"
	MsgRepoDirMissing        = "service.repo.dir-missing"
	MsgNodeModulesMissing    = "service.repo.node-modules-missing"
	MsgPnpmMissing           = "service.pnpm.missing"
	MsgPnpmMissingRemedy     = "service.pnpm.missing-remedy"
	MsgBuildRunning          = "service.build.running"
	MsgBuildFailed           = "service.build.failed"
	MsgBuildSucceeded        = "service.build.succeeded"
	MsgBuildDone             = "service.build.done"
	MsgBuildLogRotated       = "service.build.log-rotated"
	MsgPruneDone             = "service.build.prune-done"
	MsgRecordInvalidPID      = "service.record.invalid-pid"
	MsgLogFileMissing        = "service.logs.missing"
	MsgLogNoBuildSection     = "service.logs.no-build-section"
	MsgLogTooLargeForBuild   = "service.logs.too-large-for-build"
	MsgLogTooLargeForURL     = "service.logs.too-large-for-url"
	MsgAddressStarting       = "service.url.starting"
	MsgAddressNotInRecord    = "service.url.not-in-record"
	MsgAddressSurvivor       = "service.url.survivor"
	MsgAddressSurvivorLog    = "service.url.survivor-log"
	MsgURLNotRunning         = "service.url.not-running"
	MsgProbeToolsMissing     = "service.probe.tools-missing"
	MsgProbeToolsRemedy      = "service.probe.tools-remedy"
	MsgProbeOwnerUnknown     = "service.probe.owner-unknown"
	MsgProbeOwnerUnknownTip  = "service.probe.owner-unknown-tip"
	MsgWaitStartingExited    = "service.wait.starting-exited"
	MsgWaitStartingExitedLog = "service.wait.starting-exited-log"
	MsgPortTakenByOther      = "service.wait.port-taken"
	MsgWaitReadyTimeout      = "service.wait.ready-timeout"
	MsgStopTimeoutPortBusy   = "service.wait.stop-timeout"
	MsgProcessUnknown        = "service.process.unknown"
	MsgProcessSource         = "service.process.source"
	MsgObserveDebug          = "service.observe.debug"
	MsgTextWarning           = "service.text.warning"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgRepoMissing: {
		EN: "the checkout does not exist: %s\nhint: name it with --repo or the %s environment variable",
		ZH: "仓库目录不存在: %s\n提示: 用 --repo 或环境变量 %s 指定仓库路径",
	},
	MsgRepoNotCheckout: {
		EN: "%s does not look like a DeepSeek Harness checkout (no %s or %s)\nhint: point --repo at the right checkout",
		ZH: "%s 看起来不是 DeepSeek Harness 仓库(缺少 %s 或 %s)\n提示: 用 --repo 指向正确的 checkout",
	},
	MsgRepoNotGit: {
		EN: "%s is not a git repository",
		ZH: "%s 不是 git 仓库",
	},
	MsgRepoNoOrigin: {
		EN: "the checkout %s has no origin remote, so there is nothing to compare against\nhint: check that this is a clone and not a local directory",
		ZH: "仓库 %s 没有 origin 远程，无法比较版本\n提示: 确认这是一个 clone，而不是本地目录",
	},
	MsgRepoNoOriginLatest: {
		EN: "the checkout %s has no origin remote, so latest cannot be resolved\nhint: name a local version with dshctl update <tag|commit>",
		ZH: "仓库 %s 没有 origin 远程，无法解析 latest\n提示: 用 dshctl update <tag|commit> 指定本地已知的版本",
	},
	MsgRepoNotBuilt: {
		EN: "the checkout has not been built (no %s or node_modules)\nhint: run dshctl build first, then dshctl start",
		ZH: "仓库尚未构建(缺少 %s 或 node_modules)\n提示: 先运行 dshctl build, 再执行 dshctl start",
	},
	MsgRepoTrackedChanges: {
		EN: "the checkout %s has uncommitted changes to tracked files, so it cannot switch versions\nhint: run git -C %s status and handle them (untracked files are not affected)",
		ZH: "仓库 %s 有已跟踪文件的未提交修改，不能切换版本\n提示: 先运行 git -C %s status 查看并处理（未跟踪文件不受影响）",
	},
	MsgRepoUsedByServers: {
		EN: "the checkout %s is in use by services dshctl manages (ports %v, pids %v); %s would replace artifacts they are using\nhint: stop them first (dshctl stop --port <port>), then %s and start again",
		ZH: "仓库 %s 正被 dshctl 管理的服务使用 (端口 %v, pid %v),%s 会替换它正在使用的产物\nhint: 先停止这些服务(用对应的 --port 执行 dshctl stop),%s 完成后再启动",
	},
	MsgRepoUsedByOtherPorts: {
		EN: "the checkout %s is also used by the service on port %v (pid %v); %s would replace build artifacts it is using\nhint: stop that service first (dshctl stop --port <port>), then %s",
		ZH: "仓库 %s 还被端口 %v 上的服务使用 (pid %v),%s会替换它正在使用的构建产物\n提示: 先停止那些服务(可用对应的 --port 运行 dshctl stop),再执行%s",
	},
	MsgRepoDirMissing: {
		EN: "the checkout does not exist: %s",
		ZH: "仓库目录不存在: %s",
	},
	MsgNodeModulesMissing: {
		EN: "%s/node_modules does not exist; run pnpm install in the checkout first",
		ZH: "%s/node_modules 不存在，请先在仓库内执行 pnpm install",
	},
	MsgPnpmMissing: {
		EN: "pnpm was not found; install it and make sure it is on PATH",
		ZH: "找不到 pnpm，请先安装并确保它在 PATH 中",
	},
	MsgPnpmMissingRemedy: {
		EN: "pnpm was not found; install it and make sure it is on PATH",
		ZH: "找不到 pnpm，请先安装并确保它在 PATH 中",
	},
	MsgBuildRunning: {
		EN: "building the checkout %s ... (output is shown live and written to the log)",
		ZH: "正在构建仓库 %s ... (输出实时显示，同时写入日志)",
	},
	MsgBuildFailed: {
		EN: "the build failed: %w\nsee the log: %s",
		ZH: "构建失败: %w\n详见日志: %s",
	},
	MsgBuildSucceeded: {
		EN: "build succeeded",
		ZH: "构建完成",
	},
	MsgBuildDone: {
		EN: "build succeeded",
		ZH: "构建完成",
	},
	MsgBuildLogRotated: {
		EN: "the log was rotated: %s",
		ZH: "日志已轮转: %s",
	},
	MsgPruneDone: {
		EN: "prune finished: ",
		ZH: "prune 完成: ",
	},
	MsgRecordInvalidPID: {
		EN: "the record's pid is invalid: %d",
		ZH: "记录的 pid 无效: %d",
	},
	MsgLogFileMissing: {
		EN: "the log file does not exist: %s",
		ZH: "日志文件不存在: %s",
	},
	MsgLogNoBuildSection: {
		EN: "the log has no build/update/rollback record: %s",
		ZH: "日志中没有 build/update/rollback 记录: %s",
	},
	MsgLogTooLargeForBuild: {
		EN: "the log is too large to locate the last build/update/rollback record: %s\nhint: use dshctl logs -n <lines> to read the tail directly",
		ZH: "日志文件过大，未能定位最近一次 build/update/rollback 记录: %s\n提示: 用 dshctl logs -n <行数> 直接查看尾部",
	},
	MsgLogTooLargeForURL: {
		EN: "the log is already over %d MiB, so the address of port %d could not be located\nhint: dshctl logs -n 50 shows the tail",
		ZH: "日志已超过 %d MiB，未能在其中定位端口 %d 的访问地址\n提示: 可运行 dshctl logs -n 50 查看尾部输出",
	},
	MsgAddressStarting: {
		EN: "the service is starting and has not announced the address of port %d yet; retry shortly or check dshctl logs",
		ZH: "服务正在启动，尚未公布端口 %d 的访问地址;稍后重试或查看 dshctl logs",
	},
	MsgAddressNotInRecord: {
		EN: "the runtime record carries no address for port %d, and the log does not either: %s",
		ZH: "运行记录中没有端口 %d 的访问地址，日志中也找不到: %s",
	},
	MsgAddressSurvivor: {
		EN: "hint: this is a survivor of an interrupted start; dshctl start or dshctl stop manages it again",
		ZH: "提示: 这是上次启动被中断后仍存活的服务;运行 dshctl start 或 dshctl stop 可恢复管理",
	},
	MsgAddressSurvivorLog: {
		EN: "the service on port %d is left over from an earlier start, and the log has no address for it; run dshctl start to manage it again and retry",
		ZH: "端口 %d 上的服务是上次启动遗留的，日志中找不到它的访问地址;运行 dshctl start 恢复管理后再试",
	},
	MsgURLNotRunning: {
		EN: "DSH Web is not running (%s), so there is no address",
		ZH: "DSH Web 未在运行(%s)，没有可访问的地址",
	},
	MsgProbeToolsMissing: {
		EN: "no usable port probe (lsof/ss/netstat), so the state of port %d cannot be decided",
		ZH: "缺少可用的端口探测工具(lsof/ss/netstat)，无法判断端口 %d 的状态",
	},
	MsgProbeToolsRemedy: {
		EN: "hint: install one of them (iproute2 or net-tools, for example) and retry",
		ZH: "提示: 安装其中任意一个(例如 iproute2 或 net-tools)后重试",
	},
	MsgProbeOwnerUnknown: {
		EN: "something listens on port %d but the platform probe did not name its owner, so it cannot be confirmed as the service this start launched",
		ZH: "端口 %d 已有监听，但平台探测工具未报告其归属进程，无法确认它是本次启动的服务",
	},
	MsgProbeOwnerUnknownTip: {
		EN: "port %d has a listener the platform probe cannot attribute",
		ZH: "端口 %d 已有监听，但平台探测工具未报告其归属进程",
	},
	MsgWaitStartingExited: {
		EN: "the DSH Web process (pid=%d) exited and port %d never became ready",
		ZH: "DSH Web 进程 (pid=%d) 已退出，端口 %d 始终没有就绪",
	},
	MsgWaitStartingExitedLog: {
		EN: "the DSH Web process (pid=%d) exited and port %d never became ready (see the log)",
		ZH: "DSH Web 进程 (pid=%d) 已退出，端口 %d 始终没有就绪(详见日志)",
	},
	MsgPortTakenByOther: {
		EN: "port %d is held by another process (pid=%d: %s)",
		ZH: "端口 %d 被另一个进程占用 (pid=%d: %s)",
	},
	MsgWaitReadyTimeout: {
		EN: "timed out waiting for port %d to become ready (%s)",
		ZH: "等待端口 %d 就绪超时 (%s)",
	},
	MsgStopTimeoutPortBusy: {
		EN: "port %d is still held by pid %d; the stop timed out",
		ZH: "端口 %d 仍被 pid=%d 占用，停止超时",
	},
	MsgProcessUnknown: {
		EN: "unknown process",
		ZH: "未知进程",
	},
	MsgProcessSource: {
		EN: " (source: ",
		ZH: " (来源: ",
	},
	MsgObserveDebug: {
		EN: "observe port %d: state=%s record=%v survivor=%v",
		ZH: "观测端口 %d: state=%s record=%v survivor=%v",
	},
	MsgTextWarning: {
		EN: "warning: %s\n",
		ZH: "警告: %s\n",
	},
	MsgRowStateDir: {
		EN: "state directory",
		ZH: "状态目录",
	},
	MsgRowConfig: {
		EN: "settings document",
		ZH: "配置文件",
	},
	MsgRowRepo: {
		EN: "checkout",
		ZH: "仓库目录",
	},
	MsgRowRepoVersion: {
		EN: "checkout revision",
		ZH: "仓库版本",
	},
	MsgRowDeps: {
		EN: "dependencies",
		ZH: "依赖",
	},
	MsgRowArtifacts: {
		EN: "build artifacts",
		ZH: "构建产物",
	},
	MsgRowNode: {
		EN: "Node",
		ZH: "Node",
	},
	MsgRowPnpm: {
		EN: "pnpm",
		ZH: "pnpm",
	},
	MsgRowPort: {
		EN: "port",
		ZH: "端口",
	},
	MsgRowRecord: {
		EN: "runtime record",
		ZH: "运行记录",
	},
	MsgRowServiceRepo: {
		EN: "service checkout",
		ZH: "服务仓库",
	},
	MsgRowLock: {
		EN: "operation lock",
		ZH: "操作锁",
	},
	MsgRowLog: {
		EN: "log",
		ZH: "日志",
	},
	MsgRowDetach: {
		EN: "detach method",
		ZH: "进程分离方式",
	},
	MsgRowBuildInfo: {
		EN: "build info",
		ZH: "构建信息",
	},
	MsgDirNotCreated: {
		EN: " not created yet; the first run creates it",
		ZH: " 尚未创建，首次运行会自动创建",
	},
	MsgConfigNotCreated: {
		EN: " not created yet; the defaults will be written",
		ZH: " 尚未创建，将写入默认值",
	},
	MsgRepoMissingDetail: {
		EN: "%s does not exist; name it once with --repo or %s and a successful run writes it into %s",
		ZH: "%s 不存在；用 --repo 或环境变量 %s 指定一次，成功运行后会写入 %s",
	},
	MsgRepoNotGitDetail: {
		EN: "%s is not a git repository",
		ZH: "%s 不是 git 仓库",
	},
	MsgRepoNotCheckoutDetail: {
		EN: "%s has no %s or %s; it does not look like a DeepSeek Harness checkout",
		ZH: "%s 缺少 %s 或 %s，不像是 DeepSeek Harness checkout",
	},
	MsgRepoDirty: {
		EN: " (tracked changes present)",
		ZH: " (有未提交改动)",
	},
	MsgDepsInstalled: {
		EN: " installed",
		ZH: " 已安装",
	},
	MsgDepsMissing: {
		EN: " does not exist; run pnpm install first",
		ZH: " 不存在，请先执行 pnpm install",
	},
	MsgArtifactsMissing: {
		EN: "missing %s; run dshctl build",
		ZH: "缺少 %s，请运行 dshctl build",
	},
	MsgNodeViaShim: {
		EN: ", resolved through a shim",
		ZH: "，经转发条目解析",
	},
	MsgPnpmNotRunnable: {
		EN: "%s exists but cannot be executed",
		ZH: "%s 存在但无法执行",
	},
	MsgStatusUnobservableDoctor: {
		EN: "the service state cannot be observed; see the row above",
		ZH: "无法观察服务状态，见上一条",
	},
	MsgPortOwnedRunning: {
		EN: "%d is held by a service dshctl started (pid=%d)",
		ZH: "%d 由 dshctl 启动的服务占用 (pid=%d)",
	},
	MsgPortOwnedStarting: {
		EN: "%d is held by a service dshctl started (pid=%d), but the port is not ready yet",
		ZH: "%d 由 dshctl 的服务占用 (pid=%d)，端口尚未就绪",
	},
	MsgPortForeignDoctor: {
		EN: "%d is held by another process (pid=%d: %s)",
		ZH: "%d 被其他进程占用 (pid=%d: %s)",
	},
	MsgPortSurvivorDoctor: {
		EN: "%d is served by a survivor of an interrupted start (pid=%d); dshctl start or dshctl stop manages it again",
		ZH: "%d 上是上次启动被中断后仍存活的服务 (pid=%d);运行 dshctl start 或 dshctl stop 可恢复管理",
	},
	MsgPortUnclaimedDoctor: {
		EN: "%d is held by a process dshctl cannot claim (pid=%d: %s)",
		ZH: "%d 被一个 dshctl 无法确认归属的进程占用 (pid=%d: %s)",
	},
	MsgPortFree: {
		EN: "%d is free",
		ZH: "%d 空闲",
	},
	MsgRecordCorrupt: {
		EN: "%s cannot be parsed; the next start or stop rebuilds it",
		ZH: "%s 无法解析，下次 start/stop 会重建它",
	},
	MsgRecordStaleDoctor: {
		EN: "the record names pid=%d, which is gone or has been reused (a stale record; the next start or stop clears it)",
		ZH: "记录 pid=%d 已不存在或已被复用(陈旧记录，下次 start/stop 会清理)",
	},
	MsgRecordMissing: {
		EN: "none (the service has never been started)",
		ZH: "不存在(尚未启动过服务)",
	},
	MsgServiceOtherRepo: {
		EN: "the running service (pid=%d) comes from %s while the configuration says %s; stop it with its port and start again with --repo %s to switch",
		ZH: "运行中的服务 (pid=%d) 来自 %s，配置中是 %s；用对应端口执行 dshctl stop 后再用 --repo %s start 可切换",
	},
	MsgLockFree: {
		EN: "free",
		ZH: "空闲",
	},
	MsgLockHeldBy: {
		EN: "held by pid=%d; another dshctl operation is running",
		ZH: "被 pid=%d 持有，另一个 dshctl 操作正在进行",
	},
	MsgLockUnreadable: {
		EN: "held, but the lock file carries no readable pid",
		ZH: "已被持有，但锁文件里没有可读的 pid 记录",
	},
	MsgRecordUnreadable: {
		EN: "the runtime record %s could not be read, so whether it is using %s cannot be confirmed",
		ZH: "无法读取 %s 的运行记录，无法确认它是否在使用 %s",
	},
	MsgPortServiceUnconfirmable: {
		EN: "whether the service on port %d is using %s could not be confirmed",
		ZH: "无法确认端口 %d 上的服务是否在使用 %s",
	},
	MsgPruneRemoved: {
		EN: "removed %d residue directories",
		ZH: "已清理 %d 个残留目录",
	},
	MsgBuildRepoMissing: {
		EN: "the checkout does not exist: %s\nhint: name it with --repo or the %s environment variable",
		ZH: "仓库目录不存在: %s\n提示: 用 --repo 或环境变量 %s 指定仓库路径",
	},
	MsgBuildNotCheckout: {
		EN: "%s does not look like a DeepSeek Harness checkout (no %s or %s)\nhint: point --repo at the right checkout",
		ZH: "%s 看起来不是 DeepSeek Harness 仓库(缺少 %s 或 %s)\n提示: 用 --repo 指向正确的 checkout",
	},
	MsgBuildNodeModules: {
		EN: "%s/node_modules does not exist; run pnpm install in the checkout first",
		ZH: "%s/node_modules 不存在，请先在仓库内执行 pnpm install",
	},
	MsgBuildSection: {
		EN: "build",
		ZH: "构建",
	},
	MsgBuildRunningShort: {
		EN: "building",
		ZH: "构建",
	},
	MsgUpdateSection: {
		EN: "update",
		ZH: "更新",
	},
	MsgRollbackSection: {
		EN: "rollback",
		ZH: "回退",
	},
	MsgUpdateVerb: {
		EN: "update",
		ZH: "更新",
	},
	MsgRollbackVerb: {
		EN: "roll back",
		ZH: "回退",
	},
	MsgUpdateAdoptFailed: {
		EN: "a service left over from an interrupted start was found (pid=%d), but its runtime record could not be rebuilt; run dshctl stop first or handle it by hand",
		ZH: "检测到上次启动遗留的服务 (pid=%d)，但无法恢复运行记录;请先运行 dshctl stop 或手动处理",
	},
	MsgUpdateAdopted: {
		EN: "a survivor of an interrupted start was found and is managed again",
		ZH: "检测到上次启动被中断后仍存活的服务，已恢复管理",
	},
	MsgUpdateOccupant: {
		EN: "port %d is held by a process dshctl cannot claim (pid=%d): %s\nhint: confirm and stop it first, then %s",
		ZH: "端口 %d 被 dshctl 无法确认归属的进程占用 (pid=%d): %s\n提示: 先确认并停止它,再执行%s",
	},
	MsgUpdateRepoMissing: {
		EN: "the checkout does not exist: %s\nhint: name it with --repo or the %s environment variable",
		ZH: "仓库目录不存在: %s\n提示: 用 --repo 或环境变量 %s 指定仓库路径",
	},
	MsgUpdateNotGit: {
		EN: "%s is not a git repository",
		ZH: "%s 不是 git 仓库",
	},
	MsgUpdateNotCheckout: {
		EN: "%s does not look like a DeepSeek Harness checkout (no %s or %s)",
		ZH: "%s 看起来不是 DeepSeek Harness 仓库(缺少 %s 或 %s)",
	},
	MsgUpdateResolveTarget: {
		EN: "resolve target",
		ZH: "解析目标",
	},
	MsgUpdateNoOp: {
		EN: "already at %s; nothing to %s",
		ZH: "已在 %s，无需%s",
	},
	MsgUpdateUnverifiable: {
		EN: "the service in the runtime record (pid=%d) cannot be verified as this run's own (the platform cannot read its start time), so it cannot be ended safely",
		ZH: "运行记录中的服务 (pid=%d) 是否属于本次启动无法验证(平台读不到进程启动时间)，不能安全地结束它",
	},
	MsgUpdateUnverifiableTip: {
		EN: "hint: confirm the process may be stopped and end it by hand, or use another port with --port",
		ZH: "提示: 确认该进程可以停止后手动结束它，或用 --port 换一个端口",
	},
	MsgUpdateStopping: {
		EN: "DSH Web is running; stopping it first ...",
		ZH: "DSH Web 正在运行，先停止服务 ...",
	},
	MsgUpdateSwitchFailed: {
		EN: "error: %s failed",
		ZH: "错误: %s失败",
	},
	MsgUpdateRestore: {
		EN: "the old build is intact; starting the old version again ...",
		ZH: "仓库旧构建仍然完好，恢复启动旧版本 ...",
	},
	MsgUpdateRestoreFailed: {
		EN: "restoring the service failed",
		ZH: "恢复启动失败",
	},
	MsgUpdateFailed: {
		EN: "%s failed",
		ZH: "%s失败",
	},
	MsgInstallSection: {
		EN: "pnpm install",
		ZH: "pnpm install",
	},
	MsgInstallFailed: {
		EN: "pnpm install failed: %w\n%s",
		ZH: "pnpm install 失败: %w\n%s",
	},
	MsgInstallFailedNote: {
		EN: "pnpm install failed",
		ZH: "pnpm install 失败",
	},
	MsgInstallSucceeded: {
		EN: "pnpm install succeeded",
		ZH: "pnpm install 成功",
	},
	MsgBuildFailedNote: {
		EN: "pnpm run build failed",
		ZH: "pnpm run build 失败",
	},
	MsgBuildSucceededNote: {
		EN: "pnpm run build succeeded",
		ZH: "pnpm run build 成功",
	},
	MsgMoveDone: {
		EN: "%s finished",
		ZH: "%s完成",
	},
	MsgUpdateRestarting: {
		EN: "starting DSH Web again ...",
		ZH: "恢复启动 DSH Web ...",
	},
	MsgUpdateNoOriginLatest: {
		EN: "the checkout %s has no origin remote, so latest cannot be resolved\nhint: name a local version with dshctl update <tag|commit>",
		ZH: "仓库 %s 没有 origin 远程，无法解析 latest\n提示: 用 dshctl update <tag|commit> 指定本地已知的版本",
	},
	MsgUpdateFetchFailed: {
		EN: "the remote could not be fetched; resolving %q from what is known locally",
		ZH: "无法获取远程更新，按本地已知状态解析 %q",
	},
	MsgHistoryUnreadable: {
		EN: "the deployment history cannot be read: %v\nhint: delete %s and switch to a named version with dshctl update <version>",
		ZH: "更新历史无法读取: %v\n提示: 删除 %s 后可用 dshctl update <版本> 定点切换",
	},
	MsgHistoryUnreadableTip: {
		EN: "delete it to rebuild from the current version",
		ZH: "删除它将以当前版本重建",
	},
	MsgNoHistory: {
		EN: "nothing to roll back to: dshctl has not recorded a position for this checkout yet",
		ZH: "没有可回退的历史: dshctl 还没有记录过这个 checkout 的部署位置",
	},
	MsgNoHistoryTip: {
		EN: "hint: dshctl timeline shows the versions; dshctl update <version> moves to one",
		ZH: "提示: 用 dshctl timeline 查看版本，用 dshctl update <版本> 定点切换",
	},
	MsgNoHistorySteps: {
		EN: "nothing to roll back to: the history has at most %d steps left",
		ZH: "没有可回退的位置: 历史里最多还能退 %d 步",
	},
	MsgRecordedPosition: {
		EN: "recorded position",
		ZH: "记录中的位置",
	},
	MsgTargetOutsideOrigin: {
		EN: "target %s is not in the history of %s (it may come from an unmerged branch or a local commit)",
		ZH: "目标 %s 不在 %s 的历史上（可能来自未合并的分支或本地提交）",
	},
	MsgHistoryRebuild: {
		EN: "the deployment history cannot be read (%v); rebuilding from the current version",
		ZH: "更新历史无法读取(%v)，将以当前版本重建",
	},
	MsgHistoryWriteFailed: {
		EN: "the deployment history could not be written",
		ZH: "更新历史未写入",
	},
	MsgShutdownMessage: {
		EN: "the service stays stopped\nhint: fix the problem and run dshctl build && dshctl start",
		ZH: "服务保持停止状态\n提示: 修复问题后可运行 dshctl build && dshctl start",
	},
	MsgLogsBuildMissing: {
		EN: "the log has no build/update/rollback record: %s",
		ZH: "日志中没有 build/update/rollback 记录: %s",
	},
	MsgTimelineNotGit: {
		EN: "%s is not a git repository",
		ZH: "%s 不是 git 仓库",
	},
	MsgTimelineFetchFailed: {
		EN: "the remote could not be fetched; the gap below is against the last known state",
		ZH: "无法获取远程更新，以下差距基于本地已知状态",
	},
	MsgTimelineHistoryRead: {
		EN: "the deployment history %s could not be read",
		ZH: "无法读取更新历史 %s",
	},
	MsgPortsRestartOccupant: {
		EN: "port %d is held by a process dshctl cannot claim (pid=%d): %s\nhint: confirm and handle it first, then restart",
		ZH: "端口 %d 被 dshctl 无法确认归属的进程占用 (pid=%d): %s\n提示: 先确认并处理它,再执行重启",
	},
	MsgAdoptUnrecorded: {
		EN: "a service left over from an interrupted start was found (pid=%d), but its runtime record could not be rebuilt; end it by hand and retry",
		ZH: "检测到上次启动遗留的服务 (pid=%d)，但无法恢复运行记录;请手动结束它后重试",
	},
	MsgAdoptRecovered: {
		EN: "a survivor of an interrupted start was found and is managed again: %s (pid=%d)",
		ZH: "检测到上次启动被中断后仍存活的服务，已恢复管理: %s (pid=%d)",
	},
	MsgAlreadyRunning: {
		EN: "DSH Web is already running: %s (pid=%d)",
		ZH: "DSH Web 已在运行: %s (pid=%d)",
	},
	MsgStarting: {
		EN: "DSH Web is starting: %s (pid=%d)",
		ZH: "DSH Web 正在启动中: %s (pid=%d)",
	},
	MsgPortForeign: {
		EN: "port %d is held by another program (pid=%d: %s); stop it first, or use another port with --port",
		ZH: "端口 %d 被其他程序占用 (pid=%d: %s);请先停止它,或用 --port 换一个端口",
	},
	MsgPortUnclaimed: {
		EN: "the process on port %d (pid=%d) cannot be confirmed as one dshctl started: %s",
		ZH: "端口 %d 上的进程 (pid=%d) 无法确认是不是 dshctl 启动的服务: %s",
	},
	MsgPortUnclaimedTip: {
		EN: "hint: confirm it is safe to stop, end it by hand, and start again; dshctl never ends a process whose ownership it cannot establish",
		ZH: "提示: 确认它可以安全停止后手动结束它,再重新启动;dshctl 不会主动结束无法确认归属的进程",
	},
	MsgRecordLiveElsewhere: {
		EN: "the service in the runtime record (pid=%d) is still alive, but it is not listening on port %d",
		ZH: "运行记录中的服务 (pid=%d) 仍然存活，但它没有监听端口 %d",
	},
	MsgRecordLiveElsewhereTip: {
		EN: "hint: run dshctl stop first (it ends that process by the record), or confirm the process is safe to end and handle it by hand",
		ZH: "提示: 先运行 dshctl stop(会按记录结束它),或确认该进程可以安全结束后手动处理",
	},
	MsgAdoptFailed: {
		EN: "a service left over from an interrupted start was found (pid=%d), but its runtime record could not be rebuilt",
		ZH: "无法收养上次启动遗留的服务 (pid=%d)",
	},
	MsgLaunching: {
		EN: "starting DSH Web in the background ... (log: %s)",
		ZH: "正在后台启动 DSH Web ... (日志: %s)",
	},
	MsgStartFailed: {
		EN: "start failed",
		ZH: "start 失败",
	},
	MsgCloseLogFailed: {
		EN: "closing the log handle failed",
		ZH: "关闭日志句柄时出错",
	},
	MsgRecordWrapperFailed: {
		EN: "the started process could not be recorded (pid=%d)",
		ZH: "无法记录启动的进程 (pid=%d)",
	},
	MsgFingerprintUnreadable: {
		EN: "the start time of DSH Web (pid=%d) could not be read, so this run relies on port ownership alone",
		ZH: "无法读取 DSH Web (pid=%d) 的进程启动时间，本次运行将只依据端口归属判断",
	},
	MsgFingerprintUnreadableTip: {
		EN: "if the system reuses that pid, dshctl may refuse to end it (check whether a security policy forbids reading process information)",
		ZH: "若系统复用了该 pid，dshctl 可能拒绝结束它(检查是否有安全策略限制读取进程信息)",
	},
	MsgListenerGoneEarly: {
		EN: "the service process (pid=%d) exited before its start time could be recorded",
		ZH: "服务进程 (pid=%d) 在记录其启动时间之前退出了",
	},
	MsgRecordUpdateFailed: {
		EN: "the runtime record could not be updated",
		ZH: "无法更新运行记录",
	},
	MsgListenerGoneAfter: {
		EN: "the service process (pid=%d) was no longer running when the start finished",
		ZH: "服务进程 (pid=%d) 在启动完成后没有继续运行",
	},
	MsgStartSucceeded: {
		EN: "start succeeded: %s (pid=%d)",
		ZH: "启动成功: %s (pid=%d)",
	},
	MsgStartAnnounced: {
		EN: "address: %s",
		ZH: "访问地址: %s",
	},
	MsgConfigWriteFailed: {
		EN: "the settings document %s could not be written: %v (later runs resolve from the settings it already has)",
		ZH: "无法把本次运行的信息写入配置 %s: %v(以后仍会按既有设置重新解析)",
	},
	MsgConfigWroteRepo: {
		EN: "the checkout %s was written into the settings document: %s",
		ZH: "已将仓库目录 %s 写入配置: %s",
	},
	MsgConfigWroteNode: {
		EN: "Node %s was written into the settings document: %s",
		ZH: "已将 Node %s 写入配置: %s",
	},
	MsgRunningOtherCheckout: {
		EN: "the running service (pid=%d) comes from %s while the configured repoDir is %s; the two operate on different checkouts",
		ZH: "运行中的服务 (pid=%d) 来自 %s，配置中的 repoDir 是 %s；两者操作的不是同一份 checkout",
	},
	MsgUsingRepoOverride: {
		EN: "using the checkout %s (configured as %s; edit %s to make it permanent)",
		ZH: "本次使用仓库 %s(配置中为 %s；如需固定请修改 %s)",
	},
	MsgEnvRepoOverride: {
		EN: "the environment variable %s=%s overrides the configured repoDir=%s; this run uses %s",
		ZH: "环境变量 %s=%s 覆盖了配置里的 repoDir=%s，本次运行使用 %s",
	},
	MsgUsingNodeOverride: {
		EN: "using Node %s (configured as %s; edit %s to make it permanent)",
		ZH: "本次使用 Node %s(配置中为 %s；如需固定请修改 %s)",
	},
	MsgEnvNodeOverride: {
		EN: "the environment variable %s=%s overrides the configured nodeVersion=%s; this run uses %s",
		ZH: "环境变量 %s=%s 覆盖了配置里的 nodeVersion=%s，本次运行使用 %s",
	},
	MsgCleaningUp: {
		EN: "the start failed or timed out; cleaning up the processes this run started ...",
		ZH: "启动失败或超时，正在清理本次启动的进程 ...",
	},
	MsgCleanupDone: {
		EN: "cleaned up. The tail of the log:",
		ZH: "已清理。日志尾部:",
	},
	MsgCleanupPortBusy: {
		EN: "port %d is still occupied; not every process this start created has exited",
		ZH: "端口 %d 仍被占用，本次启动的进程没有全部退出",
	},
	MsgWithLog: {
		EN: " (log: %s)",
		ZH: "(日志: %s)",
	},
	MsgGroupSurvivedForce: {
		EN: "the process group this start created (leader %d) still exists after a forced end",
		ZH: "本次启动的进程树 (组长 %d) 在强制结束后仍然存在",
	},
	MsgStartLogTooLarge: {
		EN: "the log is too large to find the address this start announced; dshctl logs shows it, or wait for the service to write it",
		ZH: "日志过大，未能在其中找到本次启动公布的访问地址;可用 dshctl logs 查看或等待服务输出",
	},
	MsgStopAdoptFailed: {
		EN: "a service left over from an interrupted start was found (pid=%d), but its runtime record could not be rebuilt; end it by hand and retry",
		ZH: "检测到上次启动遗留的服务 (pid=%d)，但无法恢复运行记录;请手动结束它后重试",
	},
	MsgStopAdopted: {
		EN: "a survivor of an interrupted start was found and is managed again",
		ZH: "检测到上次启动被中断后仍存活的服务，已恢复管理",
	},
	MsgStopForeignSkipped: {
		EN: "note: port %d is held by a non-DSH process (pid=%d: %s); it was skipped, not killed by mistake",
		ZH: "注意: 端口 %d 被非 DSH 进程占用 (pid=%d: %s)，已跳过，不会误杀它",
	},
	MsgStopUnclaimedSkipped: {
		EN: "note: the process on port %d (pid=%d) cannot be confirmed as one dshctl started; it was skipped",
		ZH: "注意: 端口 %d 上的进程 (pid=%d) 无法确认是 dshctl 启动的服务，已跳过",
	},
	MsgStopUnclaimedTip: {
		EN: "hint: confirm it is safe to stop and end it by hand; dshctl never ends a process whose ownership it cannot establish",
		ZH: "提示: 确认它可以安全停止后手动结束它;dshctl 不会结束无法确认归属的进程",
	},
	MsgStopNotRunning: {
		EN: "DSH Web is not running",
		ZH: "DSH Web 未在运行",
	},
	MsgStopStopping: {
		EN: "stopping DSH Web on port %d (pid=%d) ...",
		ZH: "正在停止端口 %d 上的 DSH Web (pid=%d) ...",
	},
	MsgStopStrangerOnPort: {
		EN: "port %d is now held by another process (pid=%d); it is not a service dshctl started",
		ZH: "端口 %d 现由其他进程 (pid=%d) 占用;它不是 dshctl 启动的服务",
	},
	MsgStopStopped: {
		EN: "stopped",
		ZH: "已停止",
	},
	MsgStopRecycled: {
		EN: "pid %d has been reused by another process; no signal was sent",
		ZH: "pid %d 已被系统复用为其他进程，未发送信号",
	},
	MsgStopRecycledForce: {
		EN: "pid %d was reused by another process while waiting; no forced end was sent",
		ZH: "pid %d 在等待期间被系统复用为其他进程，未发送强制结束信号",
	},
	MsgStopSurvivedForce: {
		EN: "pid %d still exists after a forced end",
		ZH: "pid %d 在强制结束后仍然存在",
	},
	MsgStopRecordKept: {
		EN: "note: the service in the runtime record (pid=%d) is still alive; the record is kept",
		ZH: "注意: 运行记录中的服务 (pid=%d) 仍然存活，记录已保留",
	},
	MsgRestartOccupant: {
		EN: "port %d is held by a process dshctl cannot claim (pid=%d): %s\nhint: confirm and handle it first, then restart",
		ZH: "端口 %d 被 dshctl 无法确认归属的进程占用 (pid=%d): %s\n提示: 先确认并处理它,再执行重启",
	},
	MsgStateRunning: {
		EN: "running",
		ZH: "运行中",
	},
	MsgStateStarting: {
		EN: "starting",
		ZH: "启动中",
	},
	MsgStateOccupied: {
		EN: "port %d is held by another program (pid=%d)",
		ZH: "端口 %d 被其他程序占用 (pid=%d)",
	},
	MsgStateSurvivor: {
		EN: "port %d is served by a survivor of an interrupted start (pid=%d)",
		ZH: "端口 %d 上是上次启动被中断后仍存活的服务 (pid=%d)",
	},
	MsgStateUnmanaged: {
		EN: "port %d is held by a process dshctl cannot claim (pid=%d); the runtime record is missing or contradicts it",
		ZH: "端口 %d 被一个 dshctl 无法确认归属的进程占用 (pid=%d), 运行记录缺失或与之矛盾",
	},
	MsgStateUnobservable: {
		EN: "port %d cannot be probed: %s",
		ZH: "端口 %d 无法探测: %s",
	},
	MsgStateNotListening: {
		EN: "not listening on port %d (the recorded pid %d is still alive)",
		ZH: "未监听端口 %d(记录中的 pid %d 仍然存活)",
	},
	MsgStateStopped: {
		EN: "not running",
		ZH: "未运行",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

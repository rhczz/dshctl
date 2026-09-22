package cli

import "github.com/rhczz/dshctl/internal/i18n"

// This file is the shell's word list: the text a read-only command prints, and
// the messages only a command line can say. It lives here because the words
// belong to the front-end that speaks them; internal/i18n is the capability
// (resolution, catalogs, merge, audit) and knows none of them.

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
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

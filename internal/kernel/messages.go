package kernel

import "github.com/rhczz/dshctl/internal/i18n"

// This file is the kernel's word list: every sentence about an instance the
// engine can say, in every language this build speaks.
//
// It lives here rather than in internal/i18n on purpose. That package is the
// capability — resolution, the catalog type, merge, audit — and this is the
// resource: the words the lifecycle knows how to say. A front-end adds its own
// catalog the same way (see internal/cli), and the shell merges them at startup.

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
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
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

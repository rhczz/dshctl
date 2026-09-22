package host

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: why a probe could not answer and what a
// signal could not reach. internal/i18n is the capability; the words belong to
// the layer that owns the operating-system contract.

// Message ids. The host.* namespace belongs to this file.
const (
	MsgUnsupported       = "host.unsupported"
	MsgUnknownProbeTool  = "host.probe.unknown-tool"
	MsgPortIndeterminate = "host.probe.indeterminate"
	MsgNoProbeTool       = "host.probe.no-tool"
	MsgAnyProbeTool      = "host.probe.any-tool"
	MsgToolExitStatus    = "host.probe.tool-exit"
	MsgRefuseSignalGroup = "host.signal.refuse-group"
	MsgGroupEndFailed    = "host.signal.group-end-failed"
	MsgGroupSignalFailed = "host.signal.group-failed"
	MsgRefuseSignal      = "host.signal.refuse"
	MsgPIDNotFound       = "host.signal.pid-not-found"
	MsgSignalFailed      = "host.signal.failed"
	MsgEmptySnapshot     = "host.snapshot.empty"
	MsgTcpTableFailed    = "host.windows.tcp-table"
	MsgOpenProcessDenied = "host.windows.open-process-denied"
	MsgOpenProcessEmpty  = "host.windows.open-process-empty"
	MsgExecFailed        = "host.exec.failed"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgUnsupported: {
		EN: "this probe cannot be answered on this platform",
		ZH: "当前平台无法完成该探测",
	},
	MsgUnknownProbeTool: {
		EN: "unknown probe tool %q",
		ZH: "不认识的探测工具 %q",
	},
	MsgPortIndeterminate: {
		EN: "the state of port %d cannot be decided",
		ZH: "无法判断端口 %d 的占用情况",
	},
	MsgNoProbeTool: {
		EN: "no %s was found, so the state of port %d cannot be decided",
		ZH: "找不到 %s，无法判断端口 %d 的占用情况",
	},
	MsgAnyProbeTool: {
		EN: "any port probe tool",
		ZH: "任何端口探测工具",
	},
	MsgToolExitStatus: {
		EN: "%s exited with status %d",
		ZH: "%s 退出状态 %d",
	},
	MsgRefuseSignalGroup: {
		EN: "refusing to signal the process group of pid %d",
		ZH: "拒绝向 pid %d 的进程组发送信号",
	},
	MsgGroupEndFailed: {
		EN: "the process group %d could not be ended",
		ZH: "无法结束进程组 %d",
	},
	MsgGroupSignalFailed: {
		EN: "the process group %d could not be sent %v",
		ZH: "无法向进程组 %d 发送 %v",
	},
	MsgRefuseSignal: {
		EN: "refusing to signal pid %d",
		ZH: "拒绝向 pid %d 发送信号",
	},
	MsgPIDNotFound: {
		EN: "pid %d was not found",
		ZH: "找不到 pid %d",
	},
	MsgSignalFailed: {
		EN: "pid %d could not be sent %v",
		ZH: "无法向 pid %d 发送 %v",
	},
	MsgEmptySnapshot: {
		EN: "the process snapshot is empty",
		ZH: "进程快照为空",
	},
	MsgTcpTableFailed: {
		EN: "GetExtendedTcpTable failed: %d",
		ZH: "GetExtendedTcpTable 失败: %d",
	},
	MsgOpenProcessDenied: {
		EN: "OpenProcess(access denied)",
		ZH: "OpenProcess(拒绝访问)",
	},
	MsgOpenProcessEmpty: {
		EN: "OpenProcess returned an empty handle",
		ZH: "OpenProcess 返回空句柄",
	},
	MsgExecFailed: {
		EN: "%s could not be executed",
		ZH: "无法执行 %s",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

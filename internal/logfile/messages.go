package logfile

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: why a log file could not be read,
// rotated or written. internal/i18n is the capability; the words belong to the
// layer that owns the log's contract.

// Message ids. The logfile.* namespace belongs to this file.
const (
	MsgReadFailed      = "logfile.read.failed"
	MsgLocateFailed    = "logfile.locate.failed"
	MsgSizeFailed      = "logfile.size.failed"
	MsgNotRegularFile  = "logfile.not-regular"
	MsgRotateTruncate  = "logfile.rotate.truncate"
	MsgRotateRead      = "logfile.rotate.read"
	MsgRotateWrite     = "logfile.rotate.write"
	MsgRotateCopy      = "logfile.rotate.copy"
	MsgRotateSync      = "logfile.rotate.sync"
	MsgRotateClose     = "logfile.rotate.close"
	MsgRotateShort     = "logfile.rotate.short"
	MsgMkdirFailed     = "logfile.mkdir.failed"
	MsgOpenFailed      = "logfile.open.failed"
	MsgWriteFailed     = "logfile.write.failed"
	MsgTitleEmpty      = "logfile.title.empty"
	MsgTitleWhitespace = "logfile.title.whitespace"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgReadFailed: {
		EN: "%s could not be read",
		ZH: "无法读取 %s",
	},
	MsgLocateFailed: {
		EN: "%s could not be located",
		ZH: "无法定位 %s",
	},
	MsgSizeFailed: {
		EN: "the size of %s could not be read",
		ZH: "无法读取日志大小 %s",
	},
	MsgNotRegularFile: {
		EN: "the log path %s is not a regular file; check DSH_LOG_FILE",
		ZH: "日志路径 %s 不是普通文件，请检查 DSH_LOG_FILE 配置",
	},
	MsgRotateTruncate: {
		EN: "rotation failed: %s could not be truncated",
		ZH: "日志轮转失败，无法清空 %s",
	},
	MsgRotateRead: {
		EN: "rotation failed: %s could not be read",
		ZH: "日志轮转失败，无法读取 %s",
	},
	MsgRotateWrite: {
		EN: "rotation failed: %s could not be written",
		ZH: "日志轮转失败，无法写入 %s",
	},
	MsgRotateCopy: {
		EN: "rotation failed while copying to %s",
		ZH: "日志轮转失败，复制到 %s 时出错",
	},
	MsgRotateSync: {
		EN: "rotation failed while syncing %s",
		ZH: "日志轮转失败，同步 %s 时出错",
	},
	MsgRotateClose: {
		EN: "rotation failed while closing %s",
		ZH: "日志轮转失败，关闭 %s 时出错",
	},
	MsgRotateShort: {
		EN: "rotation failed: %s received only %d/%d bytes",
		ZH: "日志轮转失败，%s 只写入了 %d/%d 字节",
	},
	MsgMkdirFailed: {
		EN: "the log directory %s could not be created",
		ZH: "无法创建日志目录 %s",
	},
	MsgOpenFailed: {
		EN: "the log %s could not be opened",
		ZH: "无法打开日志 %s",
	},
	MsgWriteFailed: {
		EN: "the log %s could not be written",
		ZH: "无法写入日志 %s",
	},
	MsgTitleEmpty: {
		EN: "a section title cannot be empty",
		ZH: "日志段落名不能为空",
	},
	MsgTitleWhitespace: {
		EN: "a section title cannot contain whitespace: %q",
		ZH: "日志段落名不能包含空白字符: %q",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

package atomically

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: why a replacement could not be made.
// internal/i18n is the capability; the words belong to the layer that owns the
// crash-safe write contract.

// Message ids. The atomically.* namespace belongs to this file.
const (
	MsgMkdirFailed   = "atomically.mkdir.failed"
	MsgTempFailed    = "atomically.temp.failed"
	MsgChmodFailed   = "atomically.chmod.failed"
	MsgWriteFailed   = "atomically.write.failed"
	MsgSyncFailed    = "atomically.sync.failed"
	MsgCloseFailed   = "atomically.close.failed"
	MsgReplaceFailed = "atomically.replace.failed"
	MsgSyncDirFailed = "atomically.sync-dir.failed"
	MsgReadDirFailed = "atomically.read-dir.failed"
	MsgNotADirectory = "atomically.not-a-directory"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgMkdirFailed: {
		EN: "the directory %s could not be created",
		ZH: "无法创建目录 %s",
	},
	MsgTempFailed: {
		EN: "a temporary file could not be created in %s",
		ZH: "无法在 %s 创建临时文件",
	},
	MsgChmodFailed: {
		EN: "the permissions of %s could not be set",
		ZH: "无法设置 %s 的权限",
	},
	MsgWriteFailed: {
		EN: "%s could not be written",
		ZH: "无法写入 %s",
	},
	MsgSyncFailed: {
		EN: "%s could not be synced",
		ZH: "无法同步 %s",
	},
	MsgCloseFailed: {
		EN: "%s could not be closed",
		ZH: "无法关闭 %s",
	},
	MsgReplaceFailed: {
		EN: "%s could not be replaced",
		ZH: "无法替换 %s",
	},
	MsgSyncDirFailed: {
		EN: "the directory %s could not be synced",
		ZH: "无法同步目录 %s",
	},
	MsgReadDirFailed: {
		EN: "the directory %s could not be read",
		ZH: "无法读取目录 %s",
	},
	MsgNotADirectory: {
		EN: "%s cannot be swept: it is not a directory",
		ZH: "无法清理 %s: 不是目录",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

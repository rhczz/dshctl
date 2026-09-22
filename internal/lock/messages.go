package lock

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: who holds the operation lock and why a
// lock file could not be used. internal/i18n is the capability; the words belong
// to the layer that owns the lock's contract.

// Message ids. The lock.* namespace belongs to this file.
const (
	MsgTimeoutWithHolder = "lock.timeout.with-holder"
	MsgTimeout           = "lock.timeout"
	MsgCheckFailed       = "lock.check.failed"
	MsgNotRegularFile    = "lock.check.not-regular"
	MsgOpenFailed        = "lock.open.failed"
	MsgReplaced          = "lock.replaced"
	MsgMkdirFailed       = "lock.mkdir.failed"
	MsgFlockFailed       = "lock.flock.failed"
	MsgRecordWriteFailed = "lock.record.failed"
	MsgResidueFailed     = "lock.residue.failed"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgTimeoutWithHolder: {
		EN: "another dshctl operation is running (pid=%d); waited %s",
		ZH: "另一个 dshctl 操作正在进行 (pid=%d)，已等待 %s",
	},
	MsgTimeout: {
		EN: "another dshctl operation is running; waited %s",
		ZH: "另一个 dshctl 操作正在进行，已等待 %s",
	},
	MsgCheckFailed: {
		EN: "the lock file %s could not be checked",
		ZH: "无法检查锁文件 %s",
	},
	MsgNotRegularFile: {
		EN: "the lock path %s is not a regular file, so it cannot be checked",
		ZH: "锁路径 %s 不是普通文件，无法检查",
	},
	MsgOpenFailed: {
		EN: "the lock file %s could not be opened",
		ZH: "无法打开锁文件 %s",
	},
	MsgReplaced: {
		EN: "the lock file %s keeps being replaced; giving up",
		ZH: "锁文件 %s 反复被替换，放弃等待",
	},
	MsgMkdirFailed: {
		EN: "the lock's parent directory %s could not be created",
		ZH: "无法创建锁的父目录 %s",
	},
	MsgFlockFailed: {
		EN: "%s could not be locked",
		ZH: "无法锁定 %s",
	},
	MsgRecordWriteFailed: {
		EN: "the lock record %s could not be written",
		ZH: "无法写入锁记录 %s",
	},
	MsgResidueFailed: {
		EN: "the residue at the lock path %s could not be cleared",
		ZH: "无法清理锁路径上的残留 %s",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

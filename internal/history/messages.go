package history

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: why the deployment history could not be
// read or written. internal/i18n is the capability; the words belong to the
// layer that owns the position stack.

// Message ids. The history.* namespace belongs to this file.
const (
	MsgCorruptHistory = "history.corrupt"
	MsgReadFailed     = "history.read.failed"
	MsgNotRegularFile = "history.read.not-regular"
	MsgTooLarge       = "history.read.too-large"
	MsgEmpty          = "history.read.empty"
	MsgNotJSONObject  = "history.read.not-object"
	MsgGroupNoRepo    = "history.group.no-repo"
	MsgGroupRepeated  = "history.group.repeated"
	MsgGroupEmpty     = "history.group.empty"
	MsgRecordNoCommit = "history.record.no-commit"
	MsgRecordNoTime   = "history.record.no-time"
	MsgRecordRepeated = "history.record.repeated"
	MsgRefuseInvalid  = "history.write.invalid"
	MsgEncodeFailed   = "history.write.encode-failed"
	MsgWriteTooLarge  = "history.write.too-large"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgCorruptHistory: {
		EN: "the deployment history cannot be parsed",
		ZH: "更新历史无法解析",
	},
	MsgReadFailed: {
		EN: "the deployment history %s could not be read",
		ZH: "无法读取更新历史 %s",
	},
	MsgNotRegularFile: {
		EN: "%s is not a regular file",
		ZH: "%s 不是普通文件",
	},
	MsgTooLarge: {
		EN: "%s is too large (%d bytes)",
		ZH: "%s 过大 (%d 字节)",
	},
	MsgEmpty: {
		EN: "%s is empty",
		ZH: "%s 内容为空",
	},
	MsgNotJSONObject: {
		EN: "%s is not a JSON object",
		ZH: "%s 不是 JSON 对象",
	},
	MsgGroupNoRepo: {
		EN: "one group has no checkout path",
		ZH: "有一组没有仓库路径",
	},
	MsgGroupRepeated: {
		EN: "the checkout %s appears in two groups",
		ZH: "仓库 %s 出现了两组",
	},
	MsgGroupEmpty: {
		EN: "the group for %s has no positions",
		ZH: "仓库 %s 的组没有任何位置",
	},
	MsgRecordNoCommit: {
		EN: "the group for %s has a position without a commit",
		ZH: "仓库 %s 有一条没有 commit 的位置",
	},
	MsgRecordNoTime: {
		EN: "the position %s of %s has no timestamp",
		ZH: "仓库 %s 的位置 %s 没有时间戳",
	},
	MsgRecordRepeated: {
		EN: "the position %s of %s appears twice",
		ZH: "仓库 %s 的位置 %s 出现了两次",
	},
	MsgRefuseInvalid: {
		EN: "refusing to write an invalid deployment history",
		ZH: "拒绝写入更新历史",
	},
	MsgEncodeFailed: {
		EN: "the deployment history could not be encoded",
		ZH: "无法序列化更新历史",
	},
	MsgWriteTooLarge: {
		EN: "the deployment history is too large (%d bytes); refusing to write %s",
		ZH: "更新历史过大 (%d 字节)，拒绝写入 %s",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

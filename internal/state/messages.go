package state

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: why a stored document could not be read
// or written. internal/i18n is the capability; the words belong to the layer
// that owns the store's contract.

// Message ids. The state.* namespace belongs to this file.
const (
	MsgReadFailed      = "state.read.failed"
	MsgNotRegularFile  = "state.read.not-regular"
	MsgTooLarge        = "state.read.too-large"
	MsgEmpty           = "state.read.empty"
	MsgTrailingContent = "state.read.trailing"
	MsgRefuseInvalid   = "state.write.invalid"
	MsgEncodeFailed    = "state.write.encode-failed"
	MsgCheckFailed     = "state.remove.check-failed"
	MsgResidueFailed   = "state.remove.residue-failed"
	MsgDeleteFailed    = "state.remove.delete-failed"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgReadFailed: {
		EN: "the document %s could not be read",
		ZH: "无法读取文档 %s",
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
	MsgTrailingContent: {
		EN: "%s has content after the document",
		ZH: "%s 在文档之后还有内容",
	},
	MsgRefuseInvalid: {
		EN: "refusing to write an invalid document",
		ZH: "拒绝写入无效的文档",
	},
	MsgEncodeFailed: {
		EN: "the document could not be encoded",
		ZH: "无法序列化文档",
	},
	MsgCheckFailed: {
		EN: "the document %s could not be checked",
		ZH: "无法检查文档 %s",
	},
	MsgResidueFailed: {
		EN: "the residue at the document path %s could not be cleared",
		ZH: "无法清理文档路径上的残留 %s",
	},
	MsgDeleteFailed: {
		EN: "the document %s could not be deleted",
		ZH: "无法删除文档 %s",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

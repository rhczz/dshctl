package paths

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: why a path was refused. internal/i18n
// is the capability; the words belong to the layer that owns the path contract.

// Message ids. The paths.* namespace belongs to this file.
const (
	MsgNotAbsolute     = "paths.not-absolute"
	MsgHomeFailed      = "paths.home.failed"
	MsgNoHome          = "paths.home.missing"
	MsgHomeNotAbsolute = "paths.home.not-absolute"
	MsgExpandFailed    = "paths.expand.failed"
	MsgMkdirFailed     = "paths.mkdir.failed"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgNotAbsolute: {
		EN: "a path must be absolute or start with ~",
		ZH: "路径必须是绝对路径或 ~ 开头的路径",
	},
	MsgHomeFailed: {
		EN: "the home directory could not be determined",
		ZH: "无法确定用户主目录",
	},
	MsgNoHome: {
		EN: "the home directory cannot be determined: the environment does not provide one",
		ZH: "无法确定用户主目录: 运行环境未提供",
	},
	MsgHomeNotAbsolute: {
		EN: "the home directory is not an absolute path: %q",
		ZH: "主目录不是绝对路径: %q",
	},
	MsgExpandFailed: {
		EN: "%q could not be expanded",
		ZH: "无法展开 %q",
	},
	MsgMkdirFailed: {
		EN: "the directory %s could not be created",
		ZH: "无法创建目录 %s",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

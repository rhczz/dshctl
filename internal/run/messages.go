package run

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: why a command could not be captured.
// internal/i18n is the capability; the words belong to the layer that owns the
// execution contract.

// Message ids. The run.* namespace belongs to this file.
const (
	MsgNoExecutor = "run.no-executor"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgNoExecutor: {
		EN: "there is no command executor, so output cannot be collected",
		ZH: "没有可用的命令执行器,无法收集输出",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

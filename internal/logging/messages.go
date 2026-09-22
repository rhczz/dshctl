package logging

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: the level names and the step lines the
// log file carries. internal/i18n is the capability; the words belong to the
// layer that owns the record.

// Message ids. The logging.* namespace belongs to this file.
const (
	MsgUnknownLevel = "logging.level.unknown"
	MsgStepFailed   = "logging.step.failed"
	MsgStepDone     = "logging.step.done"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgUnknownLevel: {
		EN: "unknown log level %q: the values are debug, info, warn, error",
		ZH: "未知的日志级别 %q: 取值是 debug、info、warn、error",
	},
	MsgStepFailed: {
		EN: "step %s: failed (%v), took %s",
		ZH: "步骤 %s: 失败（%v），耗时 %s",
	},
	MsgStepDone: {
		EN: "step %s: took %s",
		ZH: "步骤 %s: 耗时 %s",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

package detach

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: why the background child could not be
// started. internal/i18n is the capability; the words belong to the layer that
// owns the detached-process contract.

// Message ids. The detach.* namespace belongs to this file.
const (
	MsgStartFailed = "detach.start.failed"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgStartFailed: {
		EN: "%s could not be started",
		ZH: "无法启动 %s",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

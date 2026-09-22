package nodejs

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: how a Node resolution failure reads to
// the operator, including the remedies. internal/i18n is the capability; the
// words belong to the layer that owns the runtime contract.

// Message ids. The node.* namespace belongs to this file.
const (
	MsgNoNodeOnPath        = "node.path.none"
	MsgProbeFailed         = "node.probe.failed"
	MsgProbeNoVersion      = "node.probe.no-version"
	MsgVersionUndetermined = "node.version.undetermined"
	MsgManagersNone        = "node.managers.none"
	MsgManagersNewest      = "node.managers.newest"
	MsgNotFoundRequested   = "node.not-found.requested"
	MsgPathNodeUnusable    = "node.path.unusable"
	MsgPathNodeMissing     = "node.path.missing"
	MsgBelowMinimum        = "node.gate.below-minimum"
	MsgUntestedMajor       = "node.gate.untested-major"
	MsgRemedyNotFound      = "node.remedy.not-found"
	MsgRemedyNoUsableNode  = "node.remedy.no-usable"
	MsgObservationLine     = "node.observation.line"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgNoNodeOnPath: {
		EN: "no node",
		ZH: "没有 node",
	},
	MsgProbeFailed: {
		EN: "could not run `%s -v`: %v",
		ZH: "无法执行 `%s -v`: %v",
	},
	MsgProbeNoVersion: {
		EN: "`%s -v` printed no version: %q",
		ZH: "`%s -v` 的输出里没有版本: %q",
	},
	MsgVersionUndetermined: {
		EN: "the version cannot be determined: ",
		ZH: "无法确定版本: ",
	},
	MsgManagersNone: {
		EN: "none installed",
		ZH: "没有安装",
	},
	MsgManagersNewest: {
		EN: "the newest is ",
		ZH: "最新的是 ",
	},
	MsgNotFoundRequested: {
		EN: "Node %s was not found",
		ZH: "找不到 Node %s",
	},
	MsgPathNodeUnusable: {
		EN: "the node on PATH cannot be used: %v",
		ZH: "PATH 上的 node 无法使用: %v",
	},
	MsgPathNodeMissing: {
		EN: "there is no node on PATH",
		ZH: "PATH 上没有 node",
	},
	MsgBelowMinimum: {
		EN: "Node %s is below the minimum %s (%s, from %s)",
		ZH: "Node %s 低于最低要求 %s(%s，来源 %s)",
	},
	MsgUntestedMajor: {
		EN: "Node %s is outside what dshctl has verified (verified %s; %s, from %s); if the Web side reports \"Failed to load plugins\", switch to Node %d.x",
		ZH: "Node %s 不在 dshctl 的验证范围内(已验证 %s；%s，来源 %s)；若 Web 端出现 \"Failed to load plugins\" 请改用 Node %d.x",
	},
	MsgRemedyNotFound: {
		EN: "Node %s was not found (looked in the nvm/fnm install roots and on PATH)",
		ZH: "找不到 Node %s(已查找 nvm/fnm 的安装目录与 PATH)",
	},
	MsgRemedyNoUsableNode: {
		EN: "no usable node",
		ZH: "找不到可用的 node",
	},
	MsgObservationLine: {
		EN: "  %s: %s is %s",
		ZH: "  %s: %s 是 %s",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

package repo

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: what a git operation could not do.
// internal/i18n is the capability; the words belong to the layer that owns the
// checkout contract.

// Message ids. The repo.* namespace belongs to this file.
const (
	MsgResolvePathFailed   = "repo.path.resolve-failed"
	MsgGlobFailed          = "repo.glob.failed"
	MsgLsFilesFailed       = "repo.lsfiles.failed"
	MsgPruneRemoving       = "repo.prune.removing"
	MsgPruneFailed         = "repo.prune.failed"
	MsgRemoteConfigFailed  = "repo.remote.config-failed"
	MsgFetchFailed         = "repo.fetch.failed"
	MsgHeadFailed          = "repo.head.failed"
	MsgHeadNoCommit        = "repo.head.no-commit"
	MsgBranchFailed        = "repo.branch.failed"
	MsgRemoteTipFailed     = "repo.remote-tip.failed"
	MsgRemoteTipNoCommit   = "repo.remote-tip.no-commit"
	MsgSelectorEmpty       = "repo.selector.empty"
	MsgSelectorFlagLike    = "repo.selector.flag-like"
	MsgSelectorLocalBranch = "repo.selector.local-branch"
	MsgGitNoCommit         = "repo.git.no-commit"
	MsgResolveFailed       = "repo.resolve.failed"
	MsgCountFailed         = "repo.count.failed"
	MsgCountOdd            = "repo.count.odd"
	MsgLogFailed           = "repo.log.failed"
	MsgLogParseFailed      = "repo.log.parse-failed"
	MsgCommitInfoFailed    = "repo.commit-info.failed"
	MsgCommitInfoParse     = "repo.commit-info.parse"
	MsgTagsFailed          = "repo.tags.failed"
	MsgTagsParse           = "repo.tags.parse"
	MsgAncestorFailed      = "repo.ancestor.failed"
	MsgSwitchFailed        = "repo.switch.failed"
	MsgSwitchMasterFailed  = "repo.switch-master.failed"
	MsgFastForwardFailed   = "repo.fast-forward.failed"
	MsgStatusFailed        = "repo.status.failed"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgResolvePathFailed: {
		EN: "the checkout path %s could not be resolved",
		ZH: "无法解析仓库路径 %s",
	},
	MsgGlobFailed: {
		EN: "%s could not be expanded",
		ZH: "无法展开 %s",
	},
	MsgLsFilesFailed: {
		EN: "git ls-files failed",
		ZH: "git ls-files 失败",
	},
	MsgPruneRemoving: {
		EN: "removing residue: %s (only %s)",
		ZH: "清理残留目录: %s (仅含 %s)",
	},
	MsgPruneFailed: {
		EN: "warning: %s could not be removed; the build continues, run pnpm run clean by hand later",
		ZH: "警告: 无法删除 %s，构建将继续;可稍后手动执行 pnpm run clean",
	},
	MsgRemoteConfigFailed: {
		EN: "the checkout's remotes could not be read",
		ZH: "无法读取仓库的远程配置",
	},
	MsgFetchFailed: {
		EN: "the remote could not be fetched",
		ZH: "无法获取远程更新",
	},
	MsgHeadFailed: {
		EN: "the checkout revision could not be read",
		ZH: "无法读取仓库版本",
	},
	MsgHeadNoCommit: {
		EN: "the checkout revision could not be read: git returned no commit",
		ZH: "无法读取仓库版本: git 没有返回 commit",
	},
	MsgBranchFailed: {
		EN: "the checkout branch could not be read",
		ZH: "无法读取仓库分支",
	},
	MsgRemoteTipFailed: {
		EN: "the position of %s could not be determined",
		ZH: "无法确定 %s 的位置",
	},
	MsgRemoteTipNoCommit: {
		EN: "the position of %s could not be determined: git returned no commit",
		ZH: "无法确定 %s 的位置: git 没有返回 commit",
	},
	MsgSelectorEmpty: {
		EN: "the version cannot be empty",
		ZH: "版本不能为空",
	},
	MsgSelectorFlagLike: {
		EN: "the version cannot start with -: %q",
		ZH: "版本不能以 - 开头: %q",
	},
	MsgSelectorLocalBranch: {
		EN: "the version cannot name a local branch %q: use %s for the remote tip, or a tag/commit",
		ZH: "版本不能指向本地分支 %q: 用 %s 表示远程最新，或改用 tag/commit",
	},
	MsgGitNoCommit: {
		EN: "git returned no commit",
		ZH: "git 没有返回 commit",
	},
	MsgResolveFailed: {
		EN: "the version %q could not be resolved",
		ZH: "无法解析版本 %q",
	},
	MsgCountFailed: {
		EN: "the commits in %s..%s could not be counted",
		ZH: "无法统计 %s..%s 的提交数",
	},
	MsgCountOdd: {
		EN: "the commits in %s..%s could not be counted: git answered %q",
		ZH: "无法统计 %s..%s 的提交数: git 返回了 %q",
	},
	MsgLogFailed: {
		EN: "the commit list of %s..%s could not be read",
		ZH: "无法读取 %s..%s 的提交列表",
	},
	MsgLogParseFailed: {
		EN: "git's commit list could not be parsed: %q",
		ZH: "无法解析 git 的提交列表: %q",
	},
	MsgCommitInfoFailed: {
		EN: "the information of %s could not be read",
		ZH: "无法读取 %s 的信息",
	},
	MsgCommitInfoParse: {
		EN: "the information of %s could not be parsed: %q",
		ZH: "无法解析 %s 的信息: %q",
	},
	MsgTagsFailed: {
		EN: "the checkout's tags could not be read",
		ZH: "无法读取仓库 tag",
	},
	MsgTagsParse: {
		EN: "git's tag list could not be parsed: %q",
		ZH: "无法解析 git 的 tag 列表: %q",
	},
	MsgAncestorFailed: {
		EN: "whether %s is an ancestor of %s could not be decided",
		ZH: "无法判断 %s 是否为 %s 的祖先",
	},
	MsgSwitchFailed: {
		EN: "could not switch to %s",
		ZH: "无法切换到 %s",
	},
	MsgSwitchMasterFailed: {
		EN: "could not switch to the %s branch",
		ZH: "无法切换到 %s 分支",
	},
	MsgFastForwardFailed: {
		EN: "could not fast-forward to %s",
		ZH: "无法快进到 %s",
	},
	MsgStatusFailed: {
		EN: "the checkout's state could not be read",
		ZH: "无法读取仓库状态",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

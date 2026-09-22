package config

import "github.com/rhczz/dshctl/internal/i18n"

// This file is this package's word list: the settings errors an operator reads
// when a document, an environment variable or a flag is wrong, and the -v
// rendering of the resolved settings. internal/i18n is the capability; the words
// belong to the layer that owns the settings contract.

// Message ids. The config.* namespace belongs to this file.
const (
	MsgConfigPathIsDir     = "config.path.is-dir"
	MsgNoHome              = "config.home.missing"
	MsgRepoDirEmpty        = "config.repo-dir.empty"
	MsgRepoDirNotAbsolute  = "config.repo-dir.not-absolute"
	MsgPortOutOfRange      = "config.port.out-of-range"
	MsgRotateNegative      = "config.rotate.negative"
	MsgRotateTooSmall      = "config.rotate.too-small"
	MsgStateDirNotAbsolute = "config.state-dir.not-absolute"
	MsgLogPathNotAbsolute  = "config.log.not-absolute"
	MsgStateGlobFailed     = "config.state.glob-failed"
	MsgStateDirUnreadable  = "config.state.unreadable"
	MsgNodeUndetermined    = "config.node.undetermined"
	MsgDescribeConfig      = "config.describe.config"
	MsgDescribeStateDir    = "config.describe.state-dir"
	MsgDescribeRepoDir     = "config.describe.repo-dir"
	MsgDescribePort        = "config.describe.port"
	MsgDescribeNode        = "config.describe.node"
	MsgDescribeLog         = "config.describe.log"
	MsgDescribeStartTO     = "config.describe.start-timeout"
	MsgDescribeStopTO      = "config.describe.stop-timeout"
	MsgDescribeLockTO      = "config.describe.lock-timeout"
	MsgDescribeRotate      = "config.describe.rotate"
	MsgDescribeLogLevel    = "config.describe.log-level"
	MsgEncodeFailed        = "config.encode.failed"
	MsgRuntimeEmpty        = "config.runtime.empty"
	MsgRuntimeRepoFailed   = "config.runtime.repo-failed"
	MsgReadFailed          = "config.read.failed"
	MsgNotRegularFile      = "config.read.not-regular"
	MsgTooLarge            = "config.read.too-large"
	MsgParseFailed         = "config.read.parse-failed"
	MsgTrailingContent     = "config.read.trailing"
	MsgFileRepoDir         = "config.file.repo-dir"
	MsgFileLogLevel        = "config.file.log-level"
	MsgEnvRepoDir          = "config.env.repo-dir"
	MsgEnvPortNotANumber   = "config.env.port-not-a-number"
	MsgEnvLogLevel         = "config.env.log-level"
	MsgFlagRepo            = "config.flag.repo"
	MsgFlagLogLevel        = "config.flag.log-level"
	MsgFlagConfig          = "config.flag.config"
	MsgEnvLogFile          = "config.env.log-file"
	MsgTimeoutTooLong      = "config.timeout.too-long"
	MsgTimeoutTooShort     = "config.timeout.too-short"
	MsgTimeoutTooShortText = "config.timeout.too-short-text"
	MsgTimeoutTooLongText  = "config.timeout.too-long-text"
	MsgSourceRepeatsGuess  = "config.source.repeats-guess"
)

// Messages is this layer's catalog.
var Messages = i18n.Catalog{
	MsgConfigPathIsDir: {
		EN: "the settings path %s is a directory, so no default document can be written",
		ZH: "配置路径 %s 是目录，无法写入默认配置",
	},
	MsgNoHome: {
		EN: "the home directory cannot be determined: the environment does not provide one",
		ZH: "无法确定用户主目录: 运行环境未提供",
	},
	MsgRepoDirEmpty: {
		EN: "repoDir cannot be empty",
		ZH: "repoDir 不能为空",
	},
	MsgRepoDirNotAbsolute: {
		EN: "repoDir must be an absolute path: %s",
		ZH: "repoDir 必须是绝对路径: %s",
	},
	MsgPortOutOfRange: {
		EN: "port must be between %d and %d: %d",
		ZH: "port 必须在 %d-%d 之间: %d",
	},
	MsgRotateNegative: {
		EN: "logRotateBytes cannot be negative: %d",
		ZH: "logRotateBytes 不能为负: %d",
	},
	MsgRotateTooSmall: {
		EN: "logRotateBytes cannot be smaller than %d (every write would rotate): %d",
		ZH: "logRotateBytes 不能小于 %d (否则每次写入都会轮转): %d",
	},
	MsgStateDirNotAbsolute: {
		EN: "the state directory must be an absolute path: %s",
		ZH: "状态目录必须是绝对路径: %s",
	},
	MsgLogPathNotAbsolute: {
		EN: "the log file must be an absolute path: %s",
		ZH: "日志文件必须是绝对路径: %s",
	},
	MsgStateGlobFailed: {
		EN: "the runtime records in %s could not be located: %w",
		ZH: "无法定位状态目录 %s 中的运行记录: %w",
	},
	MsgStateDirUnreadable: {
		EN: "the state directory %s could not be read: %w",
		ZH: "无法读取状态目录 %s: %w",
	},
	MsgNodeUndetermined: {
		EN: "(not determined; resolved from PATH at start)",
		ZH: "(未确定，启动时按 PATH 解析)",
	},
	MsgDescribeConfig: {
		EN: "settings document: %s (%s)",
		ZH: "配置文件: %s (%s)",
	},
	MsgDescribeStateDir: {
		EN: "state directory: %s (%s)",
		ZH: "状态目录: %s (%s)",
	},
	MsgDescribeRepoDir: {
		EN: "checkout: %s (%s)",
		ZH: "仓库目录: %s (%s)",
	},
	MsgDescribePort: {
		EN: "port: %s (%s)",
		ZH: "监听端口: %s (%s)",
	},
	MsgDescribeNode: {
		EN: "Node version: %s (%s)",
		ZH: "Node 版本: %s (%s)",
	},
	MsgDescribeLog: {
		EN: "log file: %s (%s)",
		ZH: "日志文件: %s (%s)",
	},
	MsgDescribeStartTO: {
		EN: "start timeout: %s (%s)",
		ZH: "启动超时: %s (%s)",
	},
	MsgDescribeStopTO: {
		EN: "stop timeout: %s (%s)",
		ZH: "停止超时: %s (%s)",
	},
	MsgDescribeLockTO: {
		EN: "lock timeout: %s (%s)",
		ZH: "锁超时:   %s (%s)",
	},
	MsgDescribeRotate: {
		EN: "log rotation: %s bytes (0 disables) (%s)",
		ZH: "日志轮转: %s 字节 (0 表示不轮转) (%s)",
	},
	MsgDescribeLogLevel: {
		EN: "log level: %s (%s)",
		ZH: "日志级别: %s (%s)",
	},
	MsgEncodeFailed: {
		EN: "the settings document could not be encoded: %w",
		ZH: "无法序列化配置: %w",
	},
	MsgRuntimeEmpty: {
		EN: "refusing to write an empty runtime record (no checkout and no Node version)",
		ZH: "拒绝写入空的运行信息(仓库目录与 Node 版本都为空)",
	},
	MsgRuntimeRepoFailed: {
		EN: "refusing to write the checkout into the document: %w",
		ZH: "拒绝把仓库路径写入配置: %w",
	},
	MsgReadFailed: {
		EN: "the settings document %s could not be read: %w",
		ZH: "无法读取配置文件 %s: %w",
	},
	MsgNotRegularFile: {
		EN: "the settings document %s is not a regular file, so it cannot be read as settings",
		ZH: "配置文件 %s 不是普通文件，无法作为配置读取",
	},
	MsgTooLarge: {
		EN: "the settings document %s is too large (%d bytes, limit %d)",
		ZH: "配置文件 %s 过大 (%d 字节，上限 %d)",
	},
	MsgParseFailed: {
		EN: "the settings document %s could not be parsed: %v",
		ZH: "配置文件 %s 解析失败: %v",
	},
	MsgTrailingContent: {
		EN: "the settings document %s has content after its first JSON value",
		ZH: "配置文件 %s 在第一个 JSON 值之后还有内容",
	},
	MsgFileRepoDir: {
		EN: "the settings document's repoDir: %w",
		ZH: "配置文件 repoDir: %w",
	},
	MsgFileLogLevel: {
		EN: "the settings document's logLevel",
		ZH: "配置文件 logLevel",
	},
	MsgEnvRepoDir: {
		EN: "the environment variable %s: %w",
		ZH: "环境变量 %s: %w",
	},
	MsgEnvPortNotANumber: {
		EN: "the environment variable %s is not a number: %q",
		ZH: "环境变量 %s 不是数字: %q",
	},
	MsgEnvLogLevel: {
		EN: "the environment variable %s",
		ZH: "环境变量 %s",
	},
	MsgFlagRepo: {
		EN: "the flag --repo: %w",
		ZH: "参数 --repo: %w",
	},
	MsgFlagLogLevel: {
		EN: "the flag --log-level",
		ZH: "参数 --log-level",
	},
	MsgFlagConfig: {
		EN: "the flag --config: %w",
		ZH: "参数 --config: %w",
	},
	MsgEnvLogFile: {
		EN: "the environment variable %s: %w",
		ZH: "环境变量 %s: %w",
	},
	MsgTimeoutTooLong: {
		EN: "%s cannot exceed %d seconds: %d",
		ZH: "%s 不能超过 %d 秒: %d",
	},
	MsgTimeoutTooShort: {
		EN: "%s must be at least 1 second: %d",
		ZH: "%s 必须至少为 1 秒: %d",
	},
	MsgTimeoutTooShortText: {
		EN: "%s must be at least 1 second: %s",
		ZH: "%s 必须至少为 1 秒: %s",
	},
	MsgTimeoutTooLongText: {
		EN: "%s cannot exceed %d seconds: %s",
		ZH: "%s 不能超过 %d 秒: %s",
	},
	MsgSourceRepeatsGuess: {
		EN: "default(repoDir in the document equals the default)",
		ZH: "default(配置文件中的 repoDir 与默认值相同)",
	},
}

// i18nLine renders one message of this layer's catalog.
func i18nLine(id string, args ...any) string { return i18n.T(id, args...) }

---
name: dshctl-architecture
description: dshctl 的垂直分层与内核契约：领域/内核/命令/接入四层各自拥有什么、Emmitter 端口怎么让第二个前端复用内核、i18n 消息目录怎么扩展、以及重写期间的金标与账本怎么保证强度。用于新增前端（HTTP/gRPC）、新增消息、判断代码该放哪一层、或改动 app/domain/i18n/层边界时。
---

# dshctl 的垂直分层与内核

## 概要

dshctl 是按**垂直分层**组织的：越靠内越纯、越靠内越少依赖。每层只回答自己的问题，
一条依赖边只能自上而下。门禁 `scripts/check-conventions.py` 的 `LAYERS` 是这张图的
可执行形式：**未登记的边就是一次设计决定**，先想清楚再登记。

```
cmd/dshctl            进程边界：信号、退出码
  internal/cli        接入层（前端）：argv 解析、分发、帮助渲染、退出码映射、事件渲染
    internal/kernel   内核：引擎与操作（start/stop/update/… + 锁范围、多实例选择、写回）
      internal/domain 领域层：模型与不变量（状态、记录、归属、指纹、版本位置）
      基础设施         host/run/detach/lock/logfile/state/repo/nodejs/config/paths/
                       exitcode/version/atomically/logging + i18n（能力）
```

**没有单独的"应用层"包**，这是刻意的：这一层的内容（命令名、flag、帮助文案、退出码、
`--json` 的形状）本来就是某个前端的词汇。CLI 的处理器就住在 `internal/cli`，未来 HTTP
前端的处理器住在 `internal/httpapi`；它们共享的是内核（同一个 `*kernel.Service`）与
消息目录的**能力**（`internal/i18n` 提供解析、目录类型、合并与审计，各层带自己的目录）。
把它再抽成一个共享包只会得到一层转发。

- **领域层 `internal/domain` 零内部依赖**：状态词表、`Status`、运行记录模型、
  指纹比对（`Matches`）、版本位置（`Target`/`Label`/`ShortCommit`）、占用判定
  （`State.Occupant`）。这里没有文件、没有进程、没有时钟，因此规则可以脱离机器测试。
  领域层里**不放文案**：它会调用 `fmt` 拼一行诊断，但不决定用什么语言说。
- **内核 `internal/kernel` 是前端唯一需要知道的东西**：`observe`/`admit`/`launch`/
  `shutdown`/`deployLocked`/`logs`/`Timeline`/`Doctor` 是引擎，`Start`/`StopAll`/
  `RestartAll`/`Statuses`/`RunUpdate` 及锁的范围、多实例选择、写回配置是操作策略。
  两者同在一个包里是因为它们共享同一台虚构机器（`fake_test.go`）与同一批不变量；
  拆开只会把测试夹具复制一份，不产生新的边界。
- **基础设施只做机制**：`host` 是唯一直接和操作系统对话的包，`state` 只负责记录的
  字节与文件规则，`repo`/`history` 只负责 git 与位置栈，`logfile` 只负责日志文件格式。

## 前端扩展契约（第二个壳怎么接）

内核**不打印、不写 writer**。它做两件事：返回结构化的结果值（`StartResult`、
`Status`、`TimelineReport`、`[]Check`、`URLReport`…），把过程中发生的事发成事件。
前端只实现一个端口：

```go
type Emitter interface {
	Emit(Event)            // 叙事、原样输出、警告、已由调用方加框的错误
	Stream() io.Writer     // 子进程 stdout、被跟随的日志等字节流
	Diagnostics() io.Writer // 子进程 stderr、失败后的日志尾部
}
```

- `kernel.TextEmitter{Out, Err}` 是参考实现，也是命令行的实现：它把事件渲染成
  `dshctl` 一贯的文字。**新增前端（HTTP/gRPC）不要改内核**，实现 `Emitter` 即可；
  若某类事件需要结构化字段，先给 `Event` 加字段，再在参考实现里渲染成文字。
- 事件与结果**只增不改**：`Event` 的 `Kind` 与 `Text` 是既有前端的渲染依据，
  `Status` 的 JSON 键是 `--json` 的对外契约，改它们要走 `dshctl-surface-change`。
- 内核发事件是为了**可观测与可测试**：测试可以直接断言事件序列，而不必解析 stdout。
  `Step`（`internal/logging`）负责把每个命名步骤的耗时写进日志文件。
- 并发模型：`*kernel.Service` 构造后即不可变（共享的只有注入的适配器与文件锁），
  多个前端可以并发调用；互斥由状态目录上的内核锁保证，只读命令不取锁。

## i18n：消息是一条目录项

- **能力与资源分开**：`internal/i18n` 只有能力（`Lang`、`Message`、语言解析、`Catalog`
  类型、`Merge`、`Audit`）；消息本身是各层自己的目录（`internal/kernel/messages.go`
  的 `MsgState*`，未来的 `internal/cli`/`internal/httpapi` 各自命名空间），由 shell 在
  启动时 `i18n.Merge` 合并——重复 id 会报错，而不是让一层的文案悄悄消失。
  调用点用**常量 id**（`i18n.T(MsgStateRunning)`）：拼错 id 是编译错误。
- **完整性是类型**：`Message{EN, ZH}` 一条消息一个字段语言；新增语言 = 加一个字段，
  编译器会把所有漏填的消息指出来。`TestCatalogIsComplete` 再钉住两种语言都非空。
- **默认英文**，机器语言是中文时用中文。语言解析顺序：
  `DSHCTL_LANG` > `LC_ALL` > `LC_MESSAGES` > `LANG`，认不出的语言按英文处理
  （不出现半翻译界面）。解析只在 `cli.Main` 一处发生，通过 `i18n.Use` 安装。
- 缺译文的单条消息回退英文；未知 id 渲染成 id 本身（在输出与金标里一眼可见）。
- **新增消息的清单**：加常量 + 在 `Messages` 里写两种语言 + 在调用点用常量 +
  在 README 需要时补一句 + 跑 `make conventions`（`TestNoOrphanMessages` 会拒绝
  没人渲染的目录项）。
- 语言是调用时的属性，不是函数的参数：把 `Translator` 穿进每个签名会把语言塞进
  与该问题无关的层。需要隔离的测试显式 `i18n.Use(i18n.New(i18n.EN, i18n.Messages))`。

## 重写期的强度机器（不要删，不要绕）

v0.3 分支用三件工具保证"功能不变、强度只增"：

1. **金标 conformance**（`internal/conformance`）：黑盒场景矩阵驱动真实二进制，
   与 v0.2.5 录下的金标逐字节比对。金标只能 `-update` 重录，且 CI 会从参照 tag
   重新生成并要求零 diff——**改金标等于改契约**，必须走加法/豁免清单。
   它显式钉住 `DSHCTL_LANG=zh`，因为金标录的是 v0.2.5 的中文输出。
2. **账本**（`internal/conformance/accounting/*.json`）：树上每个测试一行，
   `disposition` 取值 `todo/kept/new-test/conformance/differential/merged/obsolete`；
   `scripts/check-accounting.py --strict` 是合并闸门（0 个 `todo`）。
   删测试必须记账：**没有任何一行可以无声消失**。
3. **决策名册**（`accounting/decisions.json`）：`scripts/mutation-check.py` 里 63 条
   被钉决策的**名字**是身份；锚点可以随重写换位置换文本，名字不许少，且全部要被抓住。

新增被钉决策 = 补一条 `MUTATIONS` 字面替换 + 在名册里登记；证明会红用
`python3 scripts/mutation-check.py --only <名字>`。

## 相关文件

- `../../../AGENTS.md`：所有会话都生效的规则（包地图、接口政策、语言规则）。
- `../../../scripts/check-conventions.py`：`LAYERS` 与包图检查。
- `../../../internal/kernel/emit.go`：前端端口与事件定义。
- `../../../internal/i18n/i18n.go`、`../../../internal/i18n/catalog.go`：语言解析与目录。
- `../dshctl-decisions/SKILL.md`：新增包/新增边/新增接口的准入判定。
- `../dshctl-surface-change/SKILL.md`：改表面（帮助、退出码、`--json`、文件格式）的清单。

---
name: dshctl-architecture
description: dshctl 的垂直分层与内核契约：领域/内核/命令/接入四层各自拥有什么、Emitter 端口怎么让第二个前端复用内核、文案该写在哪里、以及重写期间的金标与账本怎么保证强度。用于新增前端（HTTP/gRPC）、新增消息、判断代码该放哪一层、或改动 app/domain/层边界时。
---

# dshctl 的垂直分层与内核

## 概要

dshctl 是按**垂直分层**组织的：越靠内越纯、越靠内越少依赖。每层只回答自己的问题，
一条依赖边只能自上而下。门禁 `scripts/check-conventions.py` 的 `LAYERS` 是这张图的
可执行形式：**未登记的边就是一次设计决定**，先想清楚再登记。

```
cmd/dshctl            进程边界：信号、退出码
  internal/cli        接入层（前端）：argv 解析、分发、帮助渲染、退出码映射、事件渲染
    internal/service   内核：引擎与操作（start/stop/update/… + 锁范围、多实例选择、写回）
      internal/domain 领域层：模型与不变量（状态、记录、归属、指纹、版本位置）
      基础设施         host/run/detach/lock/logfile/state/repo/nodejs/config/paths/
                       exitcode/version/atomically/logging
```

**没有单独的"应用层"包**，这是刻意的：这一层的内容（命令名、flag、帮助文案、退出码、
`--json` 的形状）本来就是某个前端的词汇。CLI 的处理器就住在 `internal/cli`，未来 HTTP
前端的处理器住在 `internal/httpapi`；它们共享的是内核（同一个 `*service.Service`）与
事件端口。把它再抽成一个共享包只会得到一层转发。

- **领域层 `internal/domain` 零内部依赖**：状态词表、`Status`、运行记录模型、
  指纹比对（`Matches`）、版本位置（`Target`/`Label`/`ShortCommit`）、占用判定
  （`State.Occupant`）。这里没有文件、没有进程、没有时钟，因此规则可以脱离机器测试。
  领域层里**不放文案**：它会调用 `fmt` 拼一行诊断，但不决定用什么语言说。
- **内核 `internal/service` 是前端唯一需要知道的东西**：`observe`/`admit`/`launch`/
  `shutdown`/`deployLocked`/`logs`/`Timeline`/`Doctor` 是引擎，`Start`/`StopAll`/
  `RestartAll`/`Statuses`/`RunUpdate` 及锁的范围、多实例选择、写回配置是操作策略。
  两者同在一个包里是因为它们共享同一台虚构机器（`fake_test.go`）与同一批不变量；
  拆开只会把测试夹具复制一份，不产生新的边界。
- **基础设施只做机制**：`host` 是唯一直接和操作系统对话的包，`state` 只负责记录的
  字节与文件规则，`repo`/`history` 只负责 git 与位置栈，`logfile` 只负责日志文件格式。

## 前端扩展契约（第二个壳怎么接）

内核在操作过程中**不打印、不写 writer**：过程走事件，结果走返回值。唯一的例外是
只读命令的表格渲染（`internal/service/print.go` 与 `timeline.go` 的 `Print*`）——
它们是给 CLI 的参考渲染器，仍在服务层里，第二个前端可以完全忽略它们；把它们搬进
`internal/cli` 是已知的下一步（见 `references/mechanism-and-resource.md`）。它做两件事：返回结构化的结果值（`StartResult`、
`Status`、`TimelineReport`、`[]Check`、`URLReport`…），把过程中发生的事发成事件。
前端只实现一个端口：

```go
type Emitter interface {
	Emit(Event)            // 叙事、原样输出、警告、已由调用方加框的错误
	Stream() io.Writer     // 子进程 stdout、被跟随的日志等字节流
	Diagnostics() io.Writer // 子进程 stderr、失败后的日志尾部
}
```

- `service.TextEmitter{Out, Err}` 是参考实现，也是命令行的实现：它把事件渲染成
  `dshctl` 一贯的文字。**新增前端（HTTP/gRPC）不要改内核**，实现 `Emitter` 即可；
  若某类事件需要结构化字段，先给 `Event` 加字段，再在参考实现里渲染成文字。
- 事件与结果**只增不改**：`Event` 的 `Kind` 与 `Text` 是既有前端的渲染依据，
  `Status` 的 JSON 键是 `--json` 的对外契约，改它们要走 `dshctl-surface-change`。
- 内核发事件是为了**可观测与可测试**：测试可以直接断言事件序列，而不必解析 stdout。
  `Step`（`internal/logging`）负责把每个命名步骤的耗时写进日志文件。
- 并发模型：`*service.Service` 构造后即不可变（共享的只有注入的适配器与文件锁），
  多个前端可以并发调用；互斥由状态目录上的内核锁保证，只读命令不取锁。

## 基础设施只提供机制，产品值定义在拥有契约的层

这是本仓库对"基础设施"的定义，也是评审时要问的问题：

判据：**把这个包拿去给另一个产品用，需要改它的源码吗？** 需要，就说明产品值写死在了机制里。
机制包不许认识文件名、记录 schema、产品名、上限、工具清单或 env 名；这些值由
`internal/service`/`internal/domain`（拥有契约的层）以常量、参数或类型传入。

机制/资源的完整清单与迁移工作清单见 `references/mechanism-and-resource.md`。
## 文案：一句话只有一个家

- **面向操作者的每一句话都是英文，并且写在产生它的调用点上**：错误用 `fmt.Errorf`
  包一层说清"哪里错了"，诊断用 `%s`/`%d` 直接拼，不经过任何目录或翻译层。
- 这里没有语言层，也没有消息目录：一个消息只有一个家，就是它被打印的地方。
  改文案不需要同时改目录，也不会出现"目录里有、代码里没人渲染"的孤儿。
- **领域层仍然不放文案**：`internal/domain` 只用 `fmt` 拼出标识符与状态词，
  说给操作者听的话由拥有该契约的层（`service`、`cli` 或基础设施包）说。
- 日志里的行与终端上的行用同一套措辞：出问题时读日志的人与看终端的人看到的是
  同一句话。
- 哨兵错误（`ErrCorrupt`、`ErrUnsupported`、`ErrNotAbsolute`）是普通的
  `errors.New` 值：文案固定，`errors.Is` 只比身份，不需要在读取时再渲染。

## 重写期的强度机器（不要删，不要绕）

v0.3 分支用三件工具保证"功能不变、强度只增"：金标 conformance（`internal/conformance`，
黑盒场景驱动真实二进制并与 v0.2.5 金标逐字节比对）、账本
（`internal/conformance/accounting/`，每个测试一行 disposition）、决策名册
（63+ 条被钉决策的名字不许少、每条都要被抓住）。细则与"删测试必须记账"见
`../dshctl-testing/references/accounting.md`。

## 相关文件

- `../../../AGENTS.md`：所有会话都生效的规则（包地图、接口政策、语言规则）。
- `../../../scripts/check-conventions.py`：`LAYERS` 与包图检查。
- `../../../internal/service/emit.go`：前端端口与事件定义。
- `../dshctl-decisions/SKILL.md`：新增包/新增边/新增接口的准入判定。
- `../dshctl-surface-change/SKILL.md`：改表面（帮助、退出码、`--json`、文件格式）的清单。

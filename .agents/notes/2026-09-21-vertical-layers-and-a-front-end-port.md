# 决策: 垂直分层，以及前端只能通过一个端口接触内核

状态: 已实施

> 取代：2026-09-22-remove-i18n.md —— 文中把 `internal/i18n` 当作叶子的部分已不成立。

## 问题

引擎与命令层混在一起（`internal/service` 一处 4,067 行），而且用例虽然返回结构化结果，
却也直接往 `io.Writer` 写人类文案 40 余处。后果是：换一个前端（HTTP/gRPC）无法复用内核——
它拿到的调用混着 stdout 文字，只能重写一遍生命周期。分层要求（基础/领域/内核/应用/接入）
来自操作者，但具体怎么切必须按项目实际。

## 决定

1. **领域层 `internal/domain` 是零依赖叶子**：状态词表、`Status`、运行记录模型、
   指纹比对 `Matches`、版本位置 `Target`、占用判定 `State.Occupant`、`Record.Describe`。
   没有 I/O、没有时钟、没有文案。
2. **内核不打印**：结果用值返回，过程发 `service.Event`，前端实现 `service.Emitter`
   （`Emit`/`Stream`/`Diagnostics`）。`service.TextEmitter` 是参考实现，也是命令行的实现。
3. **内核 `internal/service` 包含引擎与操作**：`observe`/`admit`/`launch`/`deployLocked`
   是引擎，`Start`/`StopAll`/`Statuses`/`RunUpdate` 与锁范围、多实例选择、写回配置是操作
   策略。**不设独立的"应用层"包**：命令名、flag、帮助、退出码、`--json` 形状是某个前端的
   词汇，属于那个前端；CLI 的处理器住在 `internal/cli`，未来 HTTP 前端的处理器住在
   `internal/httpapi`，它们共享内核。
4. **接入层 `internal/cli`** 只做 argv 解析、分发、帮助渲染与退出码映射；它构造前端端口。
5. 新增一条 import 边必须登记进 `check-conventions.py` 的 `LAYERS`；领域层与 i18n
   永远不登记（没有内部依赖）。

## 备选方案

- **把引擎与命令拆成两个 Go 包**：需要重做整套测试夹具（`fake_test.go` 是白盒虚构机器，
  1,621 行，226 个测试挂在上面），两包各自需要一份夹具或一个测试支持包；成本高于收益，
  边界改为文件级 + 文档级，等第二个前端真正需要时再拆。
- **按 DDD 铺开（实体/值对象/仓储/领域服务）**：本项目全是本机进程编排，
  仓储只有三个 JSON 文件，铺开会得到一层转发代码。
- **每个用例一个接口**（`Starter`/`Stopper`/…）：接口数量爆炸，且没有第二个实现者。
- **继续让内核写 `io.Writer`**：第二个前端只能解析文字，这正是要修的病。

## 后果

- `internal/app` 改名 `internal/service`；`internal/domain`、`internal/i18n` 成为新叶子；
  参考实现 `TextEmitter` 与事件类型同处内核（否则测试无法使用它，会绕开真实渲染）。
- 并发模型不变而且更强：`*service.Service` 构造后不可变，多个前端可并发调用，
  互斥由状态目录上的内核锁保证。
- 尚未完成：命令层与引擎仍未物理分包；`print.go` 的结果渲染仍在 `app`（只读命令的
  文本渲染），第二个前端可以完全忽略它们。

## 验证

- `python3 scripts/check-conventions.py` 的 `go-imports` 规则（边登记、叶子零依赖）。
- `internal/domain/*_test.go`：规则可脱离机器测试（无 fake host、无临时目录）。
- `internal/service/emit_text_test.go`：参考实现的映射（叙事/原样/警告/错误 + 两个流）。
- `internal/conformance`：金标证明事件化之后对外字节不变。

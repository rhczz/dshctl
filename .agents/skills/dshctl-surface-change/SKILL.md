---
name: dshctl-surface-change
description: 改 dshctl 面向使用者的表面：命令、flag、环境变量、config.json 字段、退出码、--json 输出或帮助文案。给出必须同步修改的完整清单与验证方式。用于任何会被 README、脚本或 documentation_test 观察到的变化。
---

# dshctl 使用者表面改动

## 概要

表面是操作者与脚本能观察到的一切：命令与帮助文案、全局与命令参数、环境变量、`config.json` 字段、`--json` 输出、退出码。一次表面改动同时落在代码、README 与两个强制测试上，漏掉任何一处都不会立刻报错，因此按 `references/surface-checklist.md` 逐项走完。

## 触发与边界

- 触发：新增或改名命令、增删 flag、新增环境变量或配置键、改 `--json` 字段、改退出码、改帮助或错误文案、改 README 任一表格。
- 边界：这个值该固定还是该可配属于 `dshctl-decisions`；文案与注释的书写风格属于 `dshctl-style`。本 skill 只回答"一处改动要同步哪些地方、怎么证明没漏"。

## 规则

1. **先定"固定 vs 配置"，再动表面。** 走 `dshctl-decisions` 的三问，答不出就写死。为什么：表面一旦发布就不能撤回，而仓库有意把两类值分开——`port`、三个超时、`logRotateBytes` 可配，Node 下限 `24.12.0` 是安全不变量，任何 flag/env/配置都不得覆盖（见 `internal/config/config.go` 的 `File` 与常量注释）。

2. **命令的唯一清单是 `internal/cli/commands.go`。** 一个命令是 `Commands()` 的一项：`Name`、`Summary`（命令列表一行）、`Help`（中文完整帮助，沿用既有结构：叙述段 + `环境变量:` / `退出码:` 两行）、`Run`。为什么：`Summary` 与 `Help` 是操作者唯一的入口文档，README 命令表逐行对照它们。

3. **全局参数只在 `internal/cli/cli.go` 的 `parseGlobals` 解析，命令自己的参数用 `newFlagSet`。** 后者的 `Usage` 会打印"全局参数(--repo/--port/--node/--config/-v)须写在命令名之前"。为什么：全局参数写到命令名之后只会得到 `flag provided but not defined`，这句提示把它变成显而易见（`newFlagSet` 的文档注释即此理由）；新增参数时解析、空值拒绝与 `-v` 来源回显要成对补齐。

4. **环境变量常量只有 `internal/paths/paths.go` 的 `Env*` 一份。** 新增变量必须同时出现在 `README.md` 的环境变量表。为什么：`internal/cli/documentation_test.go` 的 `TestTheReadmeDocumentsEverySetting` 逐项断言 README 含该变量名，把"代码里生效、却没人记录"的静默失败变成红灯。

5. **配置键就是 `internal/config/config.go` 的 `File` 字段。** 指针类型表达"没写"，json tag 就是键名，取值校验与未知键拒绝同处；新增键要同步 README 配置表。为什么：指针加 `null` 回落是一条统一规则，`DisallowUnknownFields` 防止拼错的键被当成生效配置，而 `documentation_test.go` 会逐个 json tag 断言 README 用反引号记录了它。

6. **体检与运行记录也是表面。** `doctor` 每一项是 `internal/service/doctor.go` 的 `Check{Name, Status, Detail}`（中文短标签，`Detail` 给观测值或补救动作）；`status --json` 与 `url` 读 `internal/state/state.go` 的 `Record`。为什么：排障时操作者只看这两处，新增字段却不进记录或体检，等于让 README 的运行记录一节说谎。

7. **退出码只能取 `internal/exitcode` 的常量**：`OK=0`、`Failure=1`、`Usage=2`、`NotRunning=3`、`Preflight=4`、`LockTimeout=5`、`Interrupted=130`；用 `exitcode.Wrap` 带上分类、`exitcode.Of` 还原，命令自己打印错误又要定退出码时用 `SilentExit(code)`。为什么：脚本按码分支而不是解析文案，`internal/cli/readonly_test.go` 连"该返回 3 却返回 1"都算回归。

8. **只读命令零写盘。** `status`/`url`/`logs`/`doctor`/`version` 的改动必须跑 `internal/cli/readonly_test.go`。为什么：`TestReportingCommandsLeaveNoTrace` 用真实二进制跑每个只读命令并断言磁盘零新增（含中间步骤顺手创建的锁文件）——只读承诺曾经就是这样被破坏的。

9. **输出流分工：`Env.Stdout` 放结果与帮助，`Env.Stderr` 放警告、错误与 `-v` 的配置回显。** 为什么：脚本把 stdout 当数据消费，把回显或警告混进 stdout 会污染管道输出。

10. **文案简洁、说明后果、给出下一步。** 例："仓库目录 … 不存在；用 --repo 或环境变量 DSH_REPO_DIR 指定一次，成功运行后会写入 …"。为什么：操作者与 agent 都照这句话决定下一步动作，只说"失败"会逼人去读源码（`internal/service/doctor.go` 的失败行即范例）。

11. **README 五张表是同一份契约的五个视图**（命令、全局参数、配置项、环境变量、退出码），沿用既有硬折行与列数，只增删必要的行。为什么：README 正文按固定宽度硬折行，整段重排会让 diff 失去可读性，评审看不出真正改了什么。

12. **`--json` 输出沿用 `printJSON`（两空格缩进），字段名与含义同样是契约。** 为什么：`--json` 给脚本消费，改名会让下游静默取到零值。

## 验证

- `go test ./internal/cli/ ./internal/config/ -count=1`（含 `documentation_test.go` 与 `readonly_test.go`）。
- `make fmt-check vet`。
- `python3 scripts/check-conventions.py`。
- 按 `references/surface-checklist.md` 逐项核对触点。

## 相关文件

- 完整触点清单：[references/surface-checklist.md](references/surface-checklist.md)
- 命令与参数：[../../../internal/cli/commands.go](../../../internal/cli/commands.go)、[../../../internal/cli/cli.go](../../../internal/cli/cli.go)
- 配置与路径：[../../../internal/config/config.go](../../../internal/config/config.go)、[../../../internal/paths/paths.go](../../../internal/paths/paths.go)
- 退出码：[../../../internal/exitcode/exitcode.go](../../../internal/exitcode/exitcode.go)
- 体检与记录：[../../../internal/service/doctor.go](../../../internal/service/doctor.go)、[../../../internal/state/state.go](../../../internal/state/state.go)
- 契约测试：[../../../internal/cli/documentation_test.go](../../../internal/cli/documentation_test.go)、[../../../internal/cli/readonly_test.go](../../../internal/cli/readonly_test.go)
- 对外文档：[../../../README.md](../../../README.md)

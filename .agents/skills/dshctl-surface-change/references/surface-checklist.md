# 使用者表面改动触点清单

每一类表面改动要同步的位置。代码列给出唯一真源，文档列给出 README 里对应的表，验证列给出能立刻发现遗漏的命令或测试。

| 改动 | 代码位置 | 文档 | 验证 |
|---|---|---|---|
| 新增/改名命令 | `internal/cli/commands.go` 的 `Commands()` 一项：`Name`/`Summary`/`Help`/`Run` | `README.md` 命令表 | `go test ./internal/cli/ -count=1` |
| 新增/修改只读命令 | 同上；`status`/`url`/`logs`/`doctor`/`version` 的属性由 `internal/cli/readonly_test.go` 的用例表断言 | `README.md` 状态目录一节的"不写盘"承诺 | `go test ./internal/cli/ -run TestReportingCommandsLeaveNoTrace -count=1` |
| 新增全局参数 | `internal/cli/cli.go` 的 `globals` 结构体与 `parseGlobals`，并接进 `loadSettings` 的 `config.Overrides` | `README.md` 全局参数表；有等价环境变量时同步环境变量表 | `go test ./internal/cli/ -count=1` |
| 新增命令参数 | 该命令的 `Run` 里用 `newFlagSet` 注册；用法提示与 `flag.ContinueOnError` 分类保持不变 | 该命令的 `Help` 正文与 `README.md` 命令表 | `go test ./internal/cli/ -count=1` |
| 新增环境变量 | `internal/paths/paths.go` 的 `Env*` 常量 + 读取处 | `README.md` 环境变量表 | `internal/cli/documentation_test.go` 的 `TestTheReadmeDocumentsEverySetting` |
| 新增/修改配置键 | `internal/config/config.go` 的 `File` 字段（指针类型 + json tag）+ 校验 + `Default`/`Load` | `README.md` 配置项表 | `go test ./internal/config/ -count=1`；`documentation_test.go` 逐个 json tag 断言 |
| 修改配置校验 | `internal/config/config.go` 的校验分支 | 若取值语义变化，更新配置项表说明 | `go test ./internal/config/ -count=1`（含 `config_audit_test.go`） |
| 修改 `--json` 输出 | 该命令输出的结构体与 `printJSON`（两空格缩进） | `README.md` 命令表里该命令的 `--json` 说明 | 该命令相关测试 + `readonly_test.go`（退出码） |
| 修改退出码 | `internal/exitcode/exitcode.go` 常量 + 调用点（`SilentExit` 用于自打印错误的命令） | `README.md` 退出码表 + 该命令 `Help` 的"退出码:"行 | `go test ./internal/exitcode/ ./internal/cli/ -count=1` |
| 新增/修改 `doctor` 检查项 | `internal/app/doctor.go` 的 `Check{Name, Status, Detail}` 列表 | 涉及新配置或新文件时同步 README 对应表 | `go test ./internal/app/ -run 'TestDoctor' -count=1` |
| 新增/修改运行记录字段 | `internal/state/state.go` 的 `Record`（json tag） | `README.md` 状态目录一节的记录说明 | `go test ./internal/state/ ./internal/app/ -count=1` |
| 修改帮助或错误文案 | `internal/cli/commands.go` 的 `Help`、`Summary`，或该命令的输出 | 语义变化时同步 README 对应行 | `go test ./internal/cli/ -count=1` |
| 修改输出流归属 | 该命令用的 `Env.Stdout` / `Env.Stderr` | — | `go test ./internal/cli/ -count=1` |

## 提交前总门禁

- `go test ./internal/cli/ ./internal/config/ -count=1`
- `make fmt-check vet`
- `python3 scripts/check-conventions.py`

## 容易漏掉的三处

- `Help` 里的"环境变量:"与"退出码:"两行不是装饰：它们是操作者在终端里唯一能看到的契约说明。
- `documentation_test.go` 只覆盖 `paths.Env*` 与 `File` 的 json tag；README 里的命令表、全局参数表、退出码表靠人和评审对照，没有测试兜底。
- `--json` 字段与记录字段一旦发布就不能改名，只能新增；改名属于破坏性变更，需要按 `.agents/notes/` 的决策记录流程走。

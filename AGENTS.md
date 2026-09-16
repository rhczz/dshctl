# AGENTS.md

dshctl 管理本机运行的 DeepSeek Harness Web 服务：后台启动、停止、重启、构建、更新与体检。单二进制、零第三方依赖，支持 darwin/linux/windows × amd64/arm64。

本文件只写每个会话都必须生效的规则；操作细节在对应的 skill 里（见文末路由表）。

## 命令

- `make check` = gofmt -s 检查 + 约定检查 + `go vet` + 测试；`make ci` 在前面再加 workflow 形状检查、覆盖率与 race 全量。
- `make test` 本机约 2 分钟（包间并行；单包最慢是 `internal/cli` 与 `internal/service`，各约 110s），`make ci` 约 4 分钟：迭代中跑受影响包，提交前跑全量。
- 按改动追加：`make mutation`（改 Node 或配置决策）、`make hermetic`、`make coverage`、`make cross`、`make workflow-check`。
- 需要 Go 1.24+（CI 锁 1.25.x）与 python3（`scripts/` 下的检查）。

## 包地图与依赖方向

`cmd/dshctl → internal/cli → internal/service → 叶子`，方向不可逆、不得成环、不得引入第三方 import。叶子：`atomically`（崩溃安全的写入）、`buildinfo`、`exitcode`（退出码与类型化错误）、`config`（四层设置解析）、`detach`（后台子进程）、`host`（唯一直接和操作系统对话的包）、`lock`、`logfile`、`nodejs`（Node 解析与版本门槛）、`paths`、`repo`（checkout 检查与清理）、`run`（外部命令）、`state`（运行记录）、`version`。

## 固定 vs 配置

新增任何可调值前先问三句，答不出就写死：

1. 它随部署变化吗？否 → 常量。
2. 今天就有消费者要设它吗？否 → 常量，或要求调用方显式传值。
3. 它是协议常量、外部规范或安全不变量吗？是 → 写死，任何 flag/env/配置都不得覆盖。

`DEFAULT_*` 常量与测试钩子不是可配置性。配置错误必须在最早可判定点响亮失败：未知键拒绝，缺 pnpm/git/构建产物拒绝启动，绝不降级。

写死的例子（真源在测试里）：退出码、状态目录文件名、`dsh web:` 地址行格式、构建标记路径、Node 下限 `24.12.0`、进程归属 = 运行记录 + 启动时间指纹、轮转截断同一 inode。可配置的例子：`repoDir`、`port`、`nodeVersion`、三个超时、`logRotateBytes`。

## 其他不变量

- 只读命令（`status`/`url`/`logs`/`doctor`/`version`/`help`）零写盘；`internal/cli/readonly_test.go` 跑真实二进制断言。
- 设置优先级 `flag > env > 文件 > 默认`，`-v` 打印每项来源；解析只在 `config.Load` 一处完成，操作函数内部不得再有隐藏默认。
- "探测不了"绝不当作"没有"；绝不结束不是自己启动的进程（`internal/service` 包文档三条不变量）。
- 校验只在四处边界：CLI 参数、配置文件、状态与日志文件、外部命令输出。
- 状态文件 0600、状态目录 0700、写入原子替换。
- README 是唯一对外契约，`internal/cli/documentation_test.go` 强制环境变量与配置键都被记录。
- 人读的文案中文，标识符与注释英文。

## TDD

- 先写会失败的测试再写实现；bug 先写复现测试，并证明它在修复前是红的。
- 守卫只有在回归能让它变红时才是守卫：引入回归 → 看红 → 还原；`make mutation` 是这条规则的可执行形式。
- 禁止先实现后补测试、禁止放宽或删除断言、禁止新增 skip（CI 的 skip 白名单要同步）。
- 测试描述行为而不是"正确性"；行为过时就连测试一起改，并在提交里说明。

## 风格

- 格式真源只有 `gofmt -s`；本仓库没有 linter，其余规范靠 `dshctl-style` 与 `scripts/check-conventions.py`。
- 注释英文、讲契约与失败模式，非测试文件 ≤ 88 列；导出标识符必须有文档注释。
- 错误中文 + `: %w`，句尾不加句号；命令与参数加反引号或 `%q`。
- 禁止 `panic` 与 `func init()`；TODO/FIXME/XXX 按紧急度分级并写明触发条件（当前树中为 0）。
- 一个事实一个家：README = 操作者契约，包文档 = 模型与不变量，测试 = 被钉住的行为，`.agents/notes` = 为什么与放弃了什么，skill = 流程，git = 历史；别处只链接。

## 架构

- 跨层调用只经 `service`；平台差异只出现在 build tag 文件里，上层不得有 `if windows`。
- 接口只为可测性存在（当前只有 `run.Executor`/`Capturer`/`Outputer` 与 `service.OsHost`），文档要写明它买到了什么。
- 新不变量同时写进包文档与一个测试；新包需"独立不变量 + 可独立测试 + 不引入反向依赖"三条同时成立。
- 契约性决定连同被否决的方案与后果写进 `.agents/notes/`，与代码同一提交。

## 工作流

1. 定位真源：包文档、pinning 测试、README、`.agents/notes`。
2. 定夺该固定还是该可配、代码属于哪一层。
3. 写失败测试，再实现。
4. 按改动选门禁，提交前跑全量。
5. 提交信息 `<type>: <小写英文句子描述行为变化>`，type 用 feat/fix/test/docs/ci；PR 面向 main 且 CI 三平台必绿；发布只打 `v*` tag，版本由 ldflags 注入。

## 完成定义

- [ ] `make fmt-check vet` 与受影响包测试通过，提交前 `make test` 全绿。
- [ ] 按改动补跑 `mutation` / `workflow-check` / `cross` / `hermetic` / `coverage`。
- [ ] 新增或改变的行为有会失败的测试，新不变量有反向用例。
- [ ] README 与包文档同步；契约性决定已写进 `.agents/notes/`。
- [ ] 没有新增依赖、没有削弱断言、没有新增 skip、没有触碰 `bin/` 与 `dist/`。

## Skill 路由

| 任务 | skill |
|---|---|
| 该固定还是该可配、代码放哪层、要不要抽象、要不要写决策记录 | `dshctl-decisions` |
| 开始实现、修 bug、决定先写哪种测试 | `dshctl-tdd` |
| 写或改测试文件、修 flake、补覆盖率 | `dshctl-testing` |
| 生命周期、并发、子进程、超时、清理 | `dshctl-defensive` |
| 写新文件、评审风格、`fmt-check` 报错 | `dshctl-style` |
| 命令、flag、环境变量、配置项、退出码、帮助文案 | `dshctl-surface-change` |
| 配置/状态/锁/日志的磁盘与恢复语义 | `dshctl-state-safety` |
| 平台相关代码与三平台失败 | `dshctl-portability` |
| 提交前门禁、CI 定位、提交与发布 | `dshctl-verify` |
| 评审改动、核实"已完成"的声明 | `dshctl-review` |
| 本机真实服务的排障与操作 | `dshctl-live-debug` |

通用 Go 风格、通用 TDD、代码评审方法与安全基线走全局 skill；本仓库的 skill 只写本仓库特有的形态。

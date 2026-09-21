# 评审清单

每一项给出三样东西：**去哪找真源**、**违规长什么样**、**怎么取证**。清单只列门禁证明不了的东西；`make check` 能证明的一律不提。

| 核查项 | 真源 | 违规长相 | 取证方式 |
|---|---|---|---|
| 固定 vs 配置越界 | [AGENTS.md](../../../../AGENTS.md) 的"固定 vs 配置"三问与列举 | 把 Node 下限、进程归属规则、状态目录文件名、构建标记路径、`dsh web:` 地址行格式做成 flag/env/配置；或把该随部署变化的写死 | 看 `internal/config` 的 `File`/`Overrides` 与 `internal/paths` 的 `Env*` 是否出现新面孔；问"改这个值的消费者今天存在吗" |
| 边界校验位置 | `AGENTS.md` 的"校验只在四处边界" | 在操作函数内部补了一层隐藏默认，或在解析层之外重新校验/放行 | 逐处找：CLI 参数、配置文件、状态与日志文件、外部命令输出；确认 `config.Load` 是唯一解析点 |
| 生命周期到达静止 | `internal/service/stop.go`、`internal/host` 包文档 | `Signal` 之后直接返回；等待循环缺失或不被等待；进程树未清理 | 走一遍 `shutdown` 的分支：`terminate` → `waitForStopped` → `Record.Remove()` → `endGroup` → `observe`；确认"强杀后仍在"有返回值 |
| 结果正交上报 | `internal/run` 的 `Result`/`ExitError`；`StartResult`/`StopResult` | 超时被折进成功分支；`Unverifiable` 只在一处被读；错误被 `err != nil` 一刀切 | 列出该结果类型的每个字段与每个返回点，确认每种组合都可达且有测试 |
| 资源在 teardown 归还 | `internal/logfile/follow.go`（`defer ticker.Stop()`、句柄关闭）、`internal/service/start.go`（`handle.Close()`）、`internal/lock` | 临时目录/句柄/跟随循环/子进程在返回路径上泄漏；锁在错误分支未释放 | 看每个提前 return 的错误分支是否也走 teardown；并发测试用 `-race` |
| README 同步 | `internal/cli/documentation_test.go` 钉住的环境变量与配置键；README 的命令表与退出码表 | 新 flag/env/设置/退出码只出现在代码里；README 与实现描述不一致 | 跑 `go test ./internal/cli/ -run 'TestTheReadmeDocumentsEverySetting' -count=1`，再人工核对命令表、退出码表这类测试没覆盖的部分 |
| 平台拆分被绕过 | `AGENTS.md` 与 `dshctl-portability` | 上层出现 `if windows` 平台分支；平台分支只在本机跑过；为新平台在测试里加 skip | 搜 `if windows` 与 `runtime.GOOS` 平台分支是否出现在平台文件之外；命中的先对照既有例外（`internal/config` 的 `quoteGlob`、`internal/buildinfo` 的报表用途），新增同类分支要有理由。看新增 `_windows.go` 是否有对应 `_windows_test.go` |
| 断言强度 | `dshctl-tdd` 与 [scripts/mutation-check.py](../../../../scripts/mutation-check.py) | 新测试只断言"没报错"；断言被放宽以适配实现；用例被删 | 要求给出"引入回归 → 变红 → 还原"的证据，或 `make mutation` 的结果 |
| 新增不变量缺反向用例 | 包文档里新写的不变量 | 包文档新增一条规则，测试只覆盖顺向路径 | 对每条新规则构造一个会让它变红的输入，确认有测试覆盖 |
| 契约外的输入被静默接受 | README 的命令表与命令帮助里的选择器/参数形态 | 文档没承诺的名字被解析成某个本地引用（`update master` 部署本地分支指针），用户以为在跟远程 | 对每个选择器形态跑真实 git 或真实二进制，核对帮助与 README 是否写明；没写就必须拒绝或补文档 |
| 外部命令做了超出最小动作的事 | 包文档对边界的声明（如"只写 `.git` 的远程跟踪引用"） | fetch 顺手 `--prune`、checkout 顺手 `clean`、构建顺手删除 | 读 argv，并确认有"不做 X"的反向用例（`TestFetchDoesNotPruneRemoteTrackingRefs`） |
| 用户可见文案重复或误导 | README 与命令帮助 | `目标: abc1234（abc1234）`；把 no-op 说成更新、把未知说成最新 | 真实二进制输出逐行读一遍，尤其失败与边界路径（no-op、未确认、空历史） |
| PR 体量是否可评审 | AGENTS.md 工作流 | 一个 PR 跨多个命令/子系统、上千行 | `git diff --stat main...HEAD`；按命令/子系统拆成堆叠 PR |
| 依赖与边界纪律 | [scripts/check-conventions.py](../../../../scripts/check-conventions.py) 的 `go-imports` 规则 | 引入第三方模块；叶子反向依赖 `service`/`cli`；新增 skip；触碰 `bin/`、`dist/` | 跑 `python3 scripts/check-conventions.py`；看 `git status` 与 `git diff --stat` |

## 报告格式

```
阻塞项
1. <缺陷一句话>
   位置: <文件 + 符号>
   影响: <哪个失败模式，谁会受影响>
   证据: <命令与决定性输出，或测试/包文档段落>

建议
1. <同样四要素，但说明为什么不是阻塞项>

覆盖范围
看了: <文件/包>
跑了: <命令与结果>
没看: <明确列出>
```

没有阻塞项时直接写"没有阻塞项"，并保留"覆盖范围"一节；凑条目比漏报更伤——它会让真正的阻塞项淹在噪声里。

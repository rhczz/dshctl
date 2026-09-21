# 生命周期落点表

每条性质的三处落点：**声明地**（谁规定了它）、**执行地**（哪个测试会让它变红）、**违反后的失败模式**。改代码前对着这张表找证据，改完对着它给证据。

| 性质 | 声明地 | 执行地 | 违反后的失败模式 |
|---|---|---|---|
| 正交结果独立上报 | `internal/run` 的 `Result`/`ExitError`；`internal/kernel` 的 `StartResult`/`StopResult` | `internal/run/collector_test.go`、`internal/kernel/start_test.go`、`internal/kernel/stop_test.go` | 超时被读成成功；`Unverifiable` 这类独立事实被折叠进成功分支，脚本据此误判服务状态 |
| 停机必须到达静止 | `internal/kernel/stop.go` 的 `shutdown`/`terminate` 注释 | `internal/kernel/stop_test.go`、`internal/kernel/lifecycle_test.go`（`fake_test.go` 里 `fakeProcess` 的 `survivesGraceful`/`survivesForce`） | 只发信号就返回，进程仍占端口，下一次 `start` 报"端口被占用"却停不下来 |
| 只在提交点发布状态 | `internal/kernel/start.go`（两处 `Record.Save` 的注释）、`internal/kernel/stop.go`（`Record.Remove`） | `internal/kernel/start_test.go`、`internal/state/state_test.go` | 早写把"启动中"当"运行中"；晚写让崩溃留下的服务无人认领 |
| "探测不了"不等于"没有" | `internal/host` 包文档两条契约、`internal/kernel` 包文档三条不变量 | `internal/host/probe_test.go`、`internal/kernel/fingerprint_test.go`、`internal/kernel/unified_test.go`、`internal/kernel/multi_test.go` | 探测失败被当成"端口空闲"，于是启动第二个实例或误判服务已停 |
| 一个异步操作一个控制者 | `internal/logfile/follow.go` 的 `wait`、`internal/kernel/observe.go` 的调度常量 | `internal/logfile/tail_test.go`、`internal/logfile/logfile_test.go`、`internal/kernel/lifecycle_test.go` | 等待循环在操作返回后继续跑，泄漏 goroutine 或在下一次操作里改状态 |
| 子进程环境显式拼装 | `internal/run/run.go` 的 `WithPathPrefix` 文档 | `internal/nodejs/nodejs_test.go`、`internal/kernel/nodeversion_test.go` | `node` 与 `pnpm` 来自不同安装，构建或启动用了非预期的 Node 版本 |
| 状态与临时文件安全 | `internal/state` 包文档、`internal/atomically/atomically.go`、`internal/config` 包文档 | `internal/atomically/atomically_test.go`、`internal/state/state_test.go`、`internal/config/config_test.go` | 跟随 symlink 把写入引到记录之外；半写记录让之后每次读取都以同样方式失败 |
| 日志与诊断不泄漏凭据 | `internal/kernel/start.go` 的 `webURLPattern`/`announcedURL` | `internal/kernel/start_test.go`、`internal/logfile/match.go` 相关测试 | 共享日志里认领了别的实例的地址，把别人的 token 汇报给操作者 |

## 这张表怎么用

1. 动手前：找到你改的那条性质，读它的声明地全文（包文档往往还写了为什么不用更直观的写法）。
2. 动手时：执行地是必须继续通过的测试；如果现有测试无法区分"做对了"和"看起来做对了"，先按 `dshctl-tdd` 补一个会红的测试。
3. 完成后：贴出验证命令与结果，而不是"逻辑上没问题"。改动触及等待循环、信号或记录发布时，另外给出一条"引入回归 → 变红 → 还原"的证据。

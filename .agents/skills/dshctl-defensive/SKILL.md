---
name: dshctl-defensive
description: 改 dshctl 的生命周期、并发、子进程、超时或清理代码前必读：正交结果独立上报、停机必须到达静止、只在提交点发布状态、子进程环境与临时文件安全、链接路径删除、失败封闭。用于 start/stop/restart、端口与进程探测、日志跟随、锁与等待循环的改动。
---

# dshctl 防御性改动

## 概要

`internal/service`、`internal/host`、`internal/state`、`internal/lock` 的包文档声明了本仓库最贵的几条性质。它们不是风格偏好，而是三类事故的防线：误杀别人的进程、把"探测失败"当成结论、崩溃后留下无人认领的服务。改这些包之前逐条对照本 skill，改完逐条给证据。

## 规则

1. **正交结果独立上报**：一个操作可以同时"超时"与"退出码 0"，两者必须各自可达，不得嵌在对方的分支里。
   - 落点：`internal/run` 的 `Result{Stdout,Stderr,Err,Truncated}` 与 `ExitError` 把"跑了但失败"和"根本没跑起来"分开；`internal/service` 的 `StartResult`/`StopResult` 把结果状态与 `Unverifiable` 这类独立事实分开。
   - 为什么：把结论塞进彼此的分支后，调用方必须先否定另一个事实才能到达某个分支，于是"超时"被读成"没超时所以成功"。

2. **停机必须到达静止**：发出终止请求不算停完，要等到进程树真的消失；强杀后仍存在必须有明确的上报路径。
   - 落点：`internal/service/stop.go` 的 `shutdown`：`terminate`（每次发信号前重新校验记录与启动时间，内部含 `waitForExit`）→ `waitForStopped` 等端口清空 → `Record.Remove()` → `endGroup` 结束整棵树 → 重新 `observe` → 才打印"已停止"；强杀后仍存活时返回 `pid %d 在强制结束后仍然存在`。
   - 为什么：只发请求就返回，会把仍在监听端口的进程留给下一次 `start`。`internal/service/fake_test.go` 用 `survivesGraceful`/`survivesForce` 专门建模"不听话的进程"，说明这两条路径都必须可达。

3. **只在提交点发布状态**：状态在身份确定的时刻落盘，中间态不对外宣告。
   - 落点：`internal/service/start.go` 在 wrapper 存在时先写一条记录（等待期间崩溃也留下可管理的服务），端口就绪后再改写为 listener，并把指纹换成"跑完所有 exec 之后"读到的启动时间；`internal/service/stop.go` 在进程消失后才 `Record.Remove()`。
   - 为什么：运行记录是后续所有归属判断的唯一依据；早写会把"启动中"当"运行中"，晚写会让崩溃留下的服务无人认领。

4. **"探测不了"绝不降级成结论（失败封闭）**：探测失败必须拒绝行动，不得当成"端口空闲"或"进程已死"。
   - 落点：`internal/host` 包文档的两条契约（系统拒绝回答时每个探测返回错误；本包不判定归属）与 `internal/service` 包文档的三条不变量（归属只来自记录、不把"无法查看"当事实、不结束不是自己启动的进程）。
   - 为什么：把"无法观察"当成"观察到了没有"，正是误杀与误启动的根源。

5. **一个异步操作一个控制者**：一个 context + 一把锁 + 一个等待循环，等待循环不得逸出操作生命周期；轮询间隔与宽限期是内部调度常量，不做成开关。
   - 落点：`internal/logfile/follow.go` 的 `wait(ctx, ticker)` 在 `ctx.Done()` 上退出并有 `defer ticker.Stop()`；`internal/service/stop.go` 的 `waitForExit` 只在 ctx 或 deadline 上结束；`internal/service/observe.go` 的 `pollInterval = 400ms`、`terminateGrace = 2s`、`fingerprintTolerance = 5s` 都是常量。
   - 为什么：多一个控制者就多一种"谁先返回"的不确定；把调度常量做成配置项，会让测试与生产跑在不同的时序假设上（判据见 [AGENTS.md](../../../AGENTS.md) 的"固定 vs 配置"）。

6. **子进程环境显式拼装**：用 `run.WithPathPrefix(installation.BinDir)` 把解析出的 Node bin 目录前置到 `PATH`（环境里没有 `PATH` 时结果就是该目录本身），让 `node` 与 `pnpm` 指向同一份安装；不修改 dshctl 自己的 `PATH`。
   - 落点：`internal/service/start.go` 的 `spawn`、`internal/service/build.go`、`internal/run/run.go` 的 `WithPathPrefix`。
   - 为什么：版本管理器下 `PATH` 上的 `node` 与 `pnpm` 可能来自不同安装；隐式继承环境会让"这次用的哪个 Node"变成不可复现的事实。

7. **状态与临时文件的安全写入**：状态目录 0700、状态文件 0600；写入经 `internal/atomically` 在同目录建临时文件（独占创建、0600）后原子替换；非普通文件（symlink/目录/设备）是残留，清除而不跟随。
   - 落点：`internal/state/state.go`（`Remove` 用 `Lstat` 判定，目录走 `RemoveAll`，其余 `Remove`；`Save` 用 `atomically.WriteFile(..., 0o600)`）、`internal/atomically/atomically.go`（`MkdirAll(dir, 0o700)`、`os.CreateTemp`）、`internal/config/config.go`（配置文件属于操作者，允许 symlink，写入靠重命名替换而不是跟随）。
   - 为什么：跟随链接会把写入引到记录之外；半写的记录会让下一次操作读到自相矛盾的状态。

8. **日志与诊断里的 token 地址是凭据，不许扩大它的传播面**：`dshctl url` 专门打印带 token 的地址，但 `status` 的"访问:"行与 `--json` 的 `urlWithToken` 字段也带，`logs` 转发的服务日志本身含 `dsh web:` 地址行——打印、上报、写入这些输出的路径都要按凭据对待。从共享日志里认领地址时要求整行匹配 `dsh web:` 的格式并且端口相同。
   - 落点：`internal/service/start.go` 的 `webURLPattern` 与 `announcedURL` 的注释——报告别的端口的地址，等于把另一台服务的 token 交给操作者。
   - 为什么：共享日志里每个实例都写自己的地址行；宽松匹配会把别人的 token 当成自己的汇报出去。

## 触发与边界

- 适用：`start`/`stop`/`restart`、端口与进程探测、日志跟随、锁与等待循环、子进程与清理路径、超时与宽限期。
- 同时加载：先写失败测试用 `dshctl-tdd`，测试形态用 `dshctl-testing`，磁盘语义用 `dshctl-state-safety`，平台实现用 `dshctl-portability`。
- 本 skill 不重复"该固定还是该可配"的判据（在 `dshctl-decisions`），也不描述平台 API 细节。

## 验证

```sh
go test ./internal/service/ ./internal/host/ ./internal/run/ -count=1
make test-race   # CI：本地不跑，race 只在 CI 的三平台矩阵上有意义
```

改到等待循环或信号路径时，另外证明守卫会红：引入回归 → 看测试变红 → 还原。`make mutation` 是这条规则在 Node 与配置决策上的可执行形式（见 [scripts/mutation-check.py](../../../scripts/mutation-check.py)）：整套在 CI 上跑，本地只按需用 `--only <name>` 证明某一条会被抓住。

## 相关文件

- [internal/service/stop.go](../../../internal/service/stop.go) — 停机到静止、强制结束与整棵树清理
- [internal/service/start.go](../../../internal/service/start.go) — 记录发布点、地址认领、环境拼装
- [internal/service/fake_test.go](../../../internal/service/fake_test.go) — 虚构进程表与两种"不听话"
- [internal/host/host.go](../../../internal/host/host.go) — 唯一与操作系统对话的包及其两条契约
- [internal/atomically/atomically.go](../../../internal/atomically/atomically.go) — 原子写入的实现与临时文件规则
- [生命周期落点表](references/lifecycle-paths.md) — 每条性质对应的声明地、执行地与违反后的失败模式

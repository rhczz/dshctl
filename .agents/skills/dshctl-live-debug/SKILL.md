---
name: dshctl-live-debug
description: 在本机排查或操作真实运行的 DSH Web 服务：先只读取证再决定是否 stop/start/restart；只结束运行记录里 pid 与启动时间都吻合的进程；不动别的端口上的服务（含 GUI 正在用的 3081）。
---

# 本机 DSH Web 服务的排障与操作

## 概要

dshctl 管的是长跑在后台的真实服务：它有独立的状态目录、按端口分开的运行记录和一把操作锁。排障的第一原则是**先取证再动手**——只读命令（`status`/`url`/`logs`/`doctor`/`version`）保证零写盘，`internal/cli/readonly_test.go` 跑真实二进制断言过这一点，所以它们不会改变被诊断对象的状态。判断清楚之后再决定是否需要 stop/start/restart，而且只动 dshctl 自己启动过的进程。

## 触发与边界

- 触发：本机服务起不来、端口行为异常、要确认某个端口上跑的是谁、要重启或停止服务、要端到端验证 dshctl 自身的行为。
- 边界：停止或重启 3081 必须先向操作者确认（那是他正在用的 GUI）；结束任何没有运行记录证据的进程、改操作者的状态目录或配置，都不属于本 skill 授权范围。状态文件与恢复语义的细节看 `dshctl-state-safety`，磁盘格式不是这里的主场。

## 规则

### 1. 只读取证，按固定顺序

1. `dshctl status --json`：看 `state`（`running`/`starting`/`stopped`/`port-foreign`/`port-unmanaged`，定义见 `../../../internal/app/observe.go`）、`ready`、`listenerPid` 与 `recordedPid` 是否一致、`recordLive`/`recordStale`/`survivorService`、以及 `recordedRepoDir`/`recordedNodeVersion`——"运行中的服务"与"配置里的 checkout"可能不是同一个，前者以记录为准。
2. `dshctl doctor --json`：每项是 `{name, status, detail}`，`status` 取 `ok`/`warn`/`fail`；先看 `fail`，`detail` 里通常直接写了补救动作。
3. `dshctl url`：打印带 token 的访问地址；未运行时退出码 3，不是错误。
4. `dshctl logs --build`：最近一次 build/update 记录。服务起不来最常见的原因是上一次构建失败，这一条比翻整个日志快。
5. 状态目录与日志文件：默认 `~/.dsh/dshctl/`，含 `config.json`、`dsh-web-<端口>.state.json`（`StateFileNamePattern`）、`dshctl.lock`、`dsh-web.log`（`DefaultLogFileName`）。只读地看它们来对照 `status --json` 的说法。

为什么先只读：`status` 混合了两个独立判据（端口上有没有东西、运行记录说的是不是我们），先把两者分开看清，才能避免把"端口被占用"误判成"服务挂了"。

### 2. 配置来源先看 `-v`

`dshctl -v <命令>` 把生效值与每一项的来源打到标准错误，来源标签取 `flag` / `env` / `file` / `default`，覆盖配置文件、状态目录、仓库目录、监听端口、Node 版本、日志文件、三个超时与日志轮转。排查"为什么用了这个仓库/端口/Node"先看它，不要从环境变量和配置文件两头猜。

### 3. 多端口并存时先认端口

同一个状态目录按端口分运行记录，`--port` 决定这次看/动的是哪一个：3080 与 3081 是两个互不相干的服务。任何时候先确认自己面对的是哪个端口，再谈状态；`dshctl --port 3081 status` 与 `dshctl status` 可能是两个不同的答案。

### 4. 只结束自己启动过的进程

- dshctl 只在运行记录里的 pid **与该进程的启动时间仍然吻合**时才结束它，因为 pid 会被操作系统回收，启动时间指纹是唯一的归属证据（`../../../internal/app/service.go` 的三条不变量、`../../../internal/state/state.go` 的记录模型）。
- 禁止裸 `kill`/`killall`/`pkill`：agent 没有比运行记录更强的归属证据，绕过 dshctl 就等于绕过唯一的判据。
- 端口被别的程序占用时 dshctl 只报告（`port-foreign`），`stop` 的帮助文案写的是"端口被其他程序占用时只提示，绝不误杀"；agent 也必须照此办理，不得代为清场。
- `port-unmanaged` 表示端口上有个 dshctl 无法分类的持有者：同样只报告。变更命令只有在进程组证据证明那是自己被中断的启动留下的残留时才会接管它。

### 5. 不要碰操作者正在用的服务

3081 上跑的是操作者正在使用的 GUI，停止或重启会直接打断他正在进行的会话；3080 是本机配置里的默认端口。要操作就操作你自己启动的那个端口，并且操作前先确认它不属于操作者。

### 6. 端到端验证用临时环境

- 用 `make build` 产出的 `bin/dshctl` 验证真实二进制行为，而不是 `go run`。
- 用临时目录与端口隔离：`DSHCTL_STATE_DIR`、`DSH_HOME`、`DSH_PORT` 指向你自己的一次性位置，绝不碰操作者的状态目录与运行记录。
- 退出码帮助判断：3 = 服务未运行（`status`/`url`）或端口被其他进程占用；4 = 前置检查失败（缺 node/pnpm、不是 checkout、未构建、端口被占用或无法探测）；5 = 锁超时（另一个 dshctl 操作正在进行）。完整定义见 `../../../internal/exitcode/exitcode.go`。

## 验证

- 只读取证：`dshctl status --json`、`dshctl doctor --json`、`dshctl logs --build`（全部零写盘）。
- 端到端：在临时 `DSHCTL_STATE_DIR`/`DSH_HOME`/`DSH_PORT` 下依次跑 `bin/dshctl start` → `bin/dshctl status --json` → `bin/dshctl stop`，确认退出码符合 0/3/4/5 的语义，并确认操作者的状态目录未被写入。

## 相关文件

- `../../../README.md`：命令、配置项、环境变量、状态目录与退出码的操作者契约。
- `../../../internal/app/observe.go`：状态取值与 `status --json` 的字段语义。
- `../../../internal/app/service.go`：进程归属、"探测不了不等于没有"、不误杀他人服务三条不变量。
- `../../../internal/state/state.go`：运行记录与启动时间指纹。
- `../../../internal/config/config.go`：`-v` 输出的设置来源、状态目录内文件名。
- `../../../internal/exitcode/exitcode.go`：退出码定义。
- `../../../internal/cli/commands.go`：各命令的帮助文案（含 `stop` 的"绝不误杀"）。

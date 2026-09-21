---
name: dshctl-state-safety
description: 改动 dshctl 自己的磁盘状态与并发安全：config.json、运行记录、操作锁、日志轮转、原子写入与其失败恢复。用于任何写盘/读盘路径、权限、损坏记录处理变化时。
---

# dshctl 磁盘状态与并发安全

## 概要

dshctl 的状态目录（默认 `~/.dsh/dshctl`）里只有四类文件：配置文件、按端口分的运行记录、操作锁、日志。它们被并发的 `status`/`stop`/`build` 同时读写，也会遇到崩溃、半写、被外部删除。本 skill 给出每条读写路径的既有契约与失败模式；文件清单见 `references/state-files.md`。

## 触发与边界

- 触发：新增或修改写盘/读盘路径、权限、原子性、锁语义、记录格式、损坏与半写记录的处理、日志轮转。
- 边界：某个值该固定还是该可配、校验该放在哪一层属于 `dshctl-decisions`；面向操作者的字段命名与 README 表格属于 `dshctl-surface-change`。

## 规则

1. **只写必须写的键：`repoDir` 只在 `start`/`build`/`update` 成功后写，`nodeVersion` 只在 `start` 成功后写；操作者写过的值永不覆盖。** 为什么：内置 `repoDir` 默认值只是"按主目录猜的路径"，把猜测写进文件等于永久固定猜错的结果；写明过 `repoDir` 时只打印一行说明（`internal/config/config.go` 关于猜值与写入时机的注释，README"仓库目录"一节）。

2. **配置文件用指针字段表达"没写"**：`File` 的每个字段是 `*string`/`*int`/`*int64`，缺省或 `null` 一律回落默认值；未知键由 `decoder.DisallowUnknownFields()` 拒绝。为什么：指针让"没写"与"写成零值"可区分，未知键拒绝防止拼错的键被当成生效配置——两者都是 README 配置项一节的承诺。

3. **读取有界：配置文件与运行记录都以 `64 << 10` 为上限。** 为什么：一个"配置/记录"不可能是无限大的文档，不设限会让只读命令被一个畸形文件拖到无界分配（`internal/config/config.go` 的 `maxConfigBytes`、`internal/state/state.go` 的 `maxRecordBytes`）。

4. **权限固定：状态文件 `0600`、状态目录 `0700`。** 为什么：状态目录里可能有带 token 的访问地址；`internal/atomically` 先以 `0o700` 建目录，再用调用者给的权限创建临时文件并 `Chmod`，所以文件不会有一瞬间是 world-readable。

5. **写入一律走 `internal/atomically`：同目录临时文件 → rename 覆盖。** 为什么：读者永远看不到半写文档，崩溃也不会留下截断的文件；Unix 上还会 `syncDir` flush 目录项，让 rename 挺过崩溃（`internal/atomically/syncdir_unix.go`），而不支持目录 sync 的文件系统会被报告而不是被当作写失败。

6. **运行记录的归属判据是 pid + 启动时间指纹，不是 pid 本身。** 为什么：操作系统会回收 pid，任何"先检查再发信号"的序列都是竞态；`Record.PID` 配 `Record.StartedAt`，启动时间不符即视为陈旧（`internal/state/state.go` 包文档）。记录里的 `SpawnedPID` 只用于结束整棵进程组，不用于判定归属。

7. **运行记录只由 dshctl 读写，所以非普通文件按残留清理，绝不跟随。** 为什么：记录路径上的 symlink、目录、设备都不是记录，跟随它可能读到状态目录之外；配置文件相反——它属于操作者，可以合法地放在 symlink 后面（`internal/state/state.go` 与 `internal/config/config.go` 的两套规则）。

8. **损坏记录是"这里没有可用记录"，不是"服务已停止"。** 空文件、尾随内容、`pid <= 0`、超限都是 `ErrCorrupt`；未知字段必须接受。为什么：记录由一个构建写、另一个构建读（升级、降级、两个安装共享 home），拒收未知字段曾让 `stop` 删掉唯一指针并报成功，而服务还在跑（`internal/state/state.go` 的 `Load`）。

9. **锁在 inode 上，加锁后必须校验路径仍指向同一 inode。** 为什么：内核持有建议锁，进程猝死即释放，没有陈旧锁要维护；但锁跟着 inode 走，`rm -rf` 状态目录后新文件是另一把锁，所以 `Acquire` 校验不一致就重来（`maxTakeovers`）。锁文件里的 pid 只是 `dshctl status` 的诊断信息（`internal/lock/lock.go` 包文档与 `fileChanged`）。

10. **日志轮转复制到 `<path>.old` 后截断同一 inode，绝不 rename。** 为什么：分离出去的服务进程终身持有该 inode 的 append 句柄，rename 会把之后每一行送进备份文件，下一次轮转连它们一起删掉（`internal/logfile/logfile.go` 包文档与 `RotateIfNeeded`）。

11. **记录按端口分文件，跨端口守卫用转义过的 glob。** 为什么：`build`/`update` 会 glob 出其他端口的记录以免误停别人的服务，而状态目录路径可能含 `[`、`*`、`?`——未转义的 glob 匹配不到任何东西，守卫就瞎了（`internal/config/config.go` 的 `StateFileGlob`/`quoteGlob`，`internal/service/contracts_test.go` 把它钉住）。

12. **只读命令零写盘：`status`/`url`/`logs`/`doctor`/`version` 不建锁、不写配置、不清理。** 为什么：这是 README 承诺的行为，`internal/cli/readonly_test.go` 跑真实二进制断言磁盘零新增——中间步骤顺手创建的锁文件也算违约。

## 验证

- `go test ./internal/config/ ./internal/state/ ./internal/lock/ ./internal/atomically/ ./internal/logfile/ -count=1`。
- `make hermetic`（断言测试不在自己的临时目录之外留下任何状态）。
- 涉及只读命令时追加 `go test ./internal/cli/ -run TestReportingCommandsLeaveNoTrace -count=1`。

## 相关文件

- 文件清单与失败模式：[references/state-files.md](references/state-files.md)
- 配置与记录命名：[../../../internal/config/config.go](../../../internal/config/config.go)
- 运行记录：[../../../internal/state/state.go](../../../internal/state/state.go)
- 锁：[../../../internal/lock/lock.go](../../../internal/lock/lock.go)
- 原子写：[../../../internal/atomically/atomically.go](../../../internal/atomically/atomically.go)、[../../../internal/atomically/syncdir_unix.go](../../../internal/atomically/syncdir_unix.go)
- 日志：[../../../internal/logfile/logfile.go](../../../internal/logfile/logfile.go)
- 跨端口守卫：[../../../internal/service/contracts_test.go](../../../internal/service/contracts_test.go)
- 只读承诺：[../../../internal/cli/readonly_test.go](../../../internal/cli/readonly_test.go)

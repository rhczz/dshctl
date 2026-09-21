---
name: dshctl-portability
description: 跨平台地改 dshctl：命名后缀与 build tag 约定、host/run/detach/logfile 的平台边界、Windows 特有事实。用于本机通过但 CI 三平台失败、新增平台相关代码或平台测试时。
---

# dshctl 跨平台改动

## 概要

dshctl 支持 darwin/linux/windows × amd64/arm64，平台差异只允许出现在少数带 build tag 的文件里。本 skill 给出文件命名与 tag 约定、平台边界、Windows 的具体事实，以及为什么"本机能过"不构成证据。

## 触发与边界

- 触发：改 `internal/host`/`run`/`detach`/`lock`/`logfile`/`atomically`/`state` 的平台实现，新增平台相关文件或测试，或出现"本机通过、CI 三平台中某一个失败"。
- 边界：与平台无关的测试写法属于 `dshctl-testing`；权限、记录与恢复语义属于 `dshctl-state-safety`。

## 规则

1. **后缀与 build tag 同时给出，且必须一致。** 后缀是 `_unix.go`/`_windows.go`/`_darwin.go`/`_linux.go`/`_other.go`，首行是 `//go:build`；组合 tag 在本仓库已经存在（`//go:build unix && !linux`、`//go:build !unix && !windows`、`//go:build !windows`）。为什么：后缀与 tag 各给出一层约束，二者不一致时文件会在错误的平台上被编译或整个漏编，而漏编只在另一个平台暴露。

2. **测试同样按平台拆。** 例：`internal/host/netstat_darwin_test.go`、`internal/host/netstat_linux_test.go`、`internal/host/ports_windows_layout_test.go`、`internal/run/signal_windows_test.go`、`internal/config/config_permissions_unix_test.go`。为什么：平台代码只有在自己的平台上被执行才算被测过，合并成一个文件就必然有一半分支永远不跑。

3. **`internal/host` 是唯一直接和操作系统对话的包，平台差异只用 build tag 表达，上层不出现 `if windows`。** 为什么：`internal/host/host.go` 的包文档承诺"lifecycle code above this package never contains an `if windows`"；一条平台分支漏到上层，就会在另一个平台以最难复现的方式失败。两处已声明的例外是 `runtime.GOOS` 直接出现在非 build tag 文件里，判据是"差异只是一个值，不是一段逻辑"，拆文件只会把同一段代码抄两遍：`internal/config` 的 `quoteGlob`（Windows 的 `filepath.Match` 关掉反斜杠转义，通配符要写成字符类）与 `internal/buildinfo`（只回报 `runtime.GOOS`/`GOARCH`，供版本输出与平台测试替换）。新增例外要同时改这一条与 `dshctl-review` 的清单。

4. **上层通过窄接口获得平台能力**：`service.OsHost`（`Listening`/`Inspect`/`Alive`/`Signal`/`DescendsFrom`/`GroupExists`/`SignalGroup`/`KillGroup`）与 `run` 的 `Executor`/`Capturer`/`Outputer`。为什么：这些接口存在的唯一理由是让整个生命周期跑在虚构机器上（`internal/service/host.go` 的接口文档与 `internal/service/fake_test.go`）；把平台判断挪到上层会同时废掉这套测试能力。

5. **Windows 事实，改动前先读对应文件**：
   - 主目录：`os.UserHomeDir` 在 Unix 读 `$HOME`、在 Windows 读 `%USERPROFILE%`（`internal/config/config_sources_test.go` 与 `internal/nodejs/nodejs_test.go` 用 `t.Setenv("USERPROFILE", …)` 表达这一点）。
   - 没有可寻址的进程组信号，用父 pid 链回答"这个进程是不是我启动的"（`internal/host/group_windows.go` 的 `DescendsFrom`）。
   - 子进程被放进新进程组，避免控制台 Ctrl-C 波及它（`internal/run/process_windows.go` 的 `createNewProcessGroup = 0x00000200`）；取消时用 `taskkill` 走整棵树（同文件的 `killTree`）。
   - 端口探测走 IP Helper API `GetExtendedTcpTable`，不是 netstat 文本（`internal/host/ports_windows.go`）。
   - IPv6 行是 56 字节且字段位置与 IPv4 不同；用 IPv4 布局解析 56 字节行会把端口读成地址中间的值，从而把每个 IPv6 监听者报成空闲端口（`internal/host/ports_windows.go` 的 `mibTCP6Row` 注释）。
   - 文件名不能含 `*`/`?`，所以需要构造这类目录的测试分支只能在非 Windows 平台建立（`internal/service/contracts_test.go`）。
   - 没有 Unix 权限位语义，`0600`/`0700` 的实现与断言拆在 `_unix` 文件（`internal/atomically/syncdir_unix.go`、`internal/config/config_permissions_unix_test.go`）。
   - Unix 侧还有自己的差异：netstat/lsof/ss 的文本布局与 `ps` 事实读取分别实现（`internal/host/netstat_linux.go`、`internal/host/netstat_darwin.go`、`internal/host/facts_linux.go`、`internal/host/facts_other_unix.go`），同一个问题在不同 Unix 上答案不同。

6. **"本机能过"不是证据，禁止为了让本机通过而跳过或用 `runtime.GOOS` 分支绕开其他平台路径。** 为什么：`.github/workflows/ci.yml` 的注释写明了三平台矩阵的理由——平台代码不在自己的平台上执行就等于没测；用分支绕开等于把未测代码留在树里。

7. **skip 只允许"向真实可选工具提问"的测试在工具缺失时发生**，白名单是 `.github/workflows/ci.yml` 里 "Fail on unexpected skips" 步骤的列表；新增 skip 必须同步它。为什么：静默跳过等于未测，hermetic job 会因此变红。

8. **六个交叉目标必须全过**（CI 的 `build` job；本地可用 `make cross` 复现。`Makefile` 的 `PLATFORMS` 与 ci.yml 的矩阵由 `check-workflow.py` 校验一致：darwin/linux/windows × amd64/arm64）。交叉编译时 Go 在没配置交叉 C 工具链的 runner 上默认关闭 cgo，CI 的 build job 与 release 又显式 `CGO_ENABLED: "0"`——本仓库没有 cgo 依赖，关掉它什么也不会失去。

## 验证

- 六个交叉目标由 CI 的 `build` job 以等价的 `go build` 矩阵覆盖（`make cross` 只是同一批目标的本地别名，本地不跑）；本地能验的是类型检查与受影响包。
- `GOOS=windows go vet ./...`（在本机对 Windows 做类型检查）。
- 受影响包 `go test -count=1`；平台专属分支中只有本平台那部分在本机真正执行，其余以 CI 三平台矩阵为准。

## 相关文件

- 平台边界与接口：[../../../internal/host/host.go](../../../internal/host/host.go)、[../../../internal/service/host.go](../../../internal/service/host.go)
- Windows 实现：[../../../internal/host/ports_windows.go](../../../internal/host/ports_windows.go)、[../../../internal/host/group_windows.go](../../../internal/host/group_windows.go)、[../../../internal/run/process_windows.go](../../../internal/run/process_windows.go)
- Unix 实现：[../../../internal/host/netstat_linux.go](../../../internal/host/netstat_linux.go)、[../../../internal/host/netstat_darwin.go](../../../internal/host/netstat_darwin.go)
- 三平台矩阵与 skip 白名单：[../../../.github/workflows/ci.yml](../../../.github/workflows/ci.yml)
- 交叉编译目标：[../../../Makefile](../../../Makefile)
- 平台差异对照表：[references/platform-matrix.md](references/platform-matrix.md)

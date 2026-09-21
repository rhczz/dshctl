# 平台差异对照表

同一个问题在三个平台上的答案。改任何一行之前，先读第二列的实现文件；只在本机通过不算验证。

| 问题 | Unix | Windows | 实现文件 | 测试 |
|---|---|---|---|---|
| 主目录在哪 | `$HOME` | `%USERPROFILE%`（`os.UserHomeDir` 各平台取不同的环境变量） | `internal/config`、`internal/nodejs`、`internal/paths` | `internal/config/config_sources_test.go`、`internal/nodejs/nodejs_test.go`（`t.Setenv("USERPROFILE", …)`） |
| 这个进程是不是我启动的 | 进程组成员关系 | 父 pid 链 | `internal/host/group_unix.go` / `group_windows.go` 的 `DescendsFrom` | 同目录 `_unix_test.go` / `_windows_test.go` |
| 结束整棵进程树 | 向进程组发信号 | `taskkill` 走树；子进程先放进新进程组以免控制台 Ctrl-C 波及它 | `internal/run/process_unix.go` / `process_windows.go` | `internal/run` 的平台测试 |
| 谁占着端口 | netstat/ss/lsof 的文本输出 | IP Helper API `GetExtendedTcpTable` | `internal/host/netstat_linux.go`、`netstat_darwin.go`、`ports_unix.go` / `ports_windows.go` | `internal/host/netstat_*_test.go` / `ports_windows_test.go` |
| IPv6 监听者 | 各工具自己的文本布局 | 56 字节行，字段位置与 IPv4 不同 | 同上一行 | `internal/host/ports_windows_layout_test.go` |
| 进程事实（启动时间、命令行） | `ps` 文本解析 | Windows API | `internal/host/facts_linux.go`、`facts_other_unix.go`、`ports_unix.go` 的 `Inspect` / `ports_windows.go` 的 `Inspect` | `internal/host/facts_*_test.go` |
| 文件权限 | `0600`/`0700` 有语义 | 没有 Unix 权限位语义 | `internal/atomically`、`internal/config` | `internal/config/config_permissions_unix_test.go` |
| 原子替换与目录 flush | rename 后 `syncDir` | 各自实现，不支持目录 sync 的文件系统要被报告 | `internal/atomically/replace_unix.go`、`replace_windows.go`、`replace_other.go`、`syncdir_unix.go`、`syncdir_other.go` | `internal/atomically` 的平台测试 |
| 读一个正在被替换的文件 | 直接读 | 可能报"文件正被另一进程使用"，250ms 窗口内重试 | `internal/state/state.go` 的 `readRecordFile` | `internal/state/recordread_unix_test.go` / `recordread_windows_test.go` |
| 后台分离与就绪探测 | `detach_unix.go` | `detach_windows.go` | `internal/detach` | `internal/detach/detach_windows_test.go`、`probe_*_test.go` |
| 文件锁 | `flock_unix.go` | `flock_windows.go` | `internal/lock` | `internal/lock/lock_*_test.go` |
| Node 可执行文件解析 | `executable_unix.go` | `executable_other.go` | `internal/nodejs` | `internal/nodejs/nodejs_test.go` |
| 跟随日志的打开方式 | 普通 open 就够，句柄不阻止 rename/unlink | 必须显式共享 delete，否则跟随会把日志钉住、轮转与删除都会失败 | `internal/logfile/open_other.go` / `open_windows.go` 的 `openForFollow` | `internal/logfile` 测试 |
| 信号处理 | `cmd/dshctl/signal_unix.go` | `signal_windows.go` | `cmd/dshctl` | `cmd/dshctl` 测试 |
| 文件名限制 | 可以含 `*`/`?` | 不可以 | 只影响测试夹具 | `internal/app/contracts_test.go` 的分支 |

## 后缀与 build tag 的既有形态

- 单一约束：`_unix.go`、`_windows.go`、`_darwin.go`、`_linux.go`、`_other.go`，首行写 `//go:build`，两者必须一致。
- 组合约束：`//go:build unix && !linux`、`//go:build !windows`（Unix 与其它平台共用一份实现）、`//go:build !unix && !windows`。
- 测试同规则：`*_unix_test.go`、`*_windows_test.go`、`*_darwin_test.go`、`*_linux_test.go`。
- 只在一个平台上建得出来的测试夹具（例如文件名含 `*` 的目录）必须放平台测试文件，或用分支跳过非 Windows 路径。

## 交叉编译与 CI

- `make cross` 的 `PLATFORMS`：`darwin/amd64`、`darwin/arm64`、`linux/amd64`、`linux/arm64`、`windows/amd64`、`windows/arm64`；交叉编译单独关 cgo（本仓库没有 cgo 依赖）。
- `.github/workflows/ci.yml` 的矩阵是 `ubuntu-latest`、`macos-latest`、`windows-latest`，另有一个 hermetic job 跑 `make hermetic`。
- 该 workflow 里有一个 "Fail on unexpected skips" 步骤，白名单只允许"向真实可选工具提问"的测试在工具缺失时跳过；新增 skip 必须同步它。

# 状态目录文件清单

默认状态目录是 `$DSHCTL_STATE_DIR`，否则 `$DSH_HOME/dshctl`，再退回 `~/.dsh/dshctl`（`internal/paths/paths.go`）。目录里的每个文件都有一个写入者与一个读者，任何改动都要同时回答"崩溃时会留下什么"和"两个 dshctl 并发时会看到什么"。

| 文件 | 内容 | 写者与时机 | 读者 | 关键约束 |
|---|---|---|---|---|
| `config.json` | `internal/config/config.go` 的 `File`（指针字段，未知键拒绝） | 可变命令首次运行时按键的默认值生成；`start`/`build`/`update` 成功后写 `repoDir`，`start` 成功后写 `nodeVersion`，且只在文件还没写明它们时写 | `config.Load`（所有命令） | 属于操作者：可以放在 symlink 后面；操作者写过的值永不覆盖；上限 `64 << 10`，权限 `0600` |
| `dsh-web-<端口>.state.json` | `internal/state/state.go` 的 `Record`：pid、启动时间、端口、URL、spawnedPid、Node 版本与路径、仓库目录 | 持有操作锁的 `start`/`stop`/观察路径，通过 `Store.Save` | `status`、`url`、`stop`、`doctor` | 只由 dshctl 读写：非普通文件（symlink、目录、设备）按残留清理；上限 `64 << 10`；权限 `0600`；写入原子替换 |
| `dshctl.lock` | 持锁进程的 pid（诊断信息，不是锁本身） | `lock.Acquire` | `status` 显示 holder，等待者读取 | 锁在 inode 上：加锁后校验路径仍指向同一 inode，否则重来（`maxTakeovers`） |
| `dsh-web.log` | 服务输出、build 输出、update 输出共用一份日志 | 分离出去的服务进程（持有 append 句柄）、build/update 的收集器 | `logs`、`logs --build`、启动时读取地址行 | 轮转到 `dsh-web.log.old` 时复制后截断同一 inode，绝不 rename |
| `dsh-web.log.old` | 上一次轮转的备份 | 轮转 | `logs`（跨轮转跟随） | 由轮转写入；轮转失败时保留 live 文件并报告 |

## 失败模式与既有处理

- **半写/损坏记录**：空文件、只有空白、尾随内容、`pid <= 0`、超过 `64 << 10` 都判为 `state.ErrCorrupt`；调用者按"这里没有可用记录"处理，绝不是"服务已停止"。未知字段必须接受，因为记录可能由另一个构建写入（升级、降级、两个安装共享 home）。
- **pid 回收**：`Record.StartedAt` 是归属指纹，pid 相同但启动时间不同即视为陈旧；`SpawnedPID` 只用于结束整棵进程组。
- **文件正在被替换**：Windows 上读取可能得到"文件正被另一进程使用"，`readRecordFile` 在 250ms 窗口内重试，其他错误（不存在、损坏、权限）第一次就报告。
- **跨端口守卫**：`config.StateFileGlob` 用 `quoteGlob` 转义路径里的 `[`、`*`、`?`；未转义时 glob 匹配不到任何记录，`build` 就可能停掉别的端口上的服务。
- **目录被删**：状态目录可随时删除；锁跟着 inode 走，所以删除后必须重新校验；`stop` 仍然可用，因为它只看运行记录。
- **只读命令**：`status`/`url`/`logs`/`doctor`/`version` 不创建锁、不写配置、不做清理。

## 权限与原子性

- 目录：`0700`（`internal/atomically` 在写入前 `MkdirAll(dir, 0o700)`）。
- 配置与运行记录：`0600`（`atomically.WriteFile(path, data, 0o600)`）；临时文件以同样的权限创建并 `Chmod`，不会有一瞬间 world-readable。
- 写入：同目录临时文件 → rename 覆盖；Unix 上再 `syncDir` flush 目录项，让 rename 挺过崩溃，不支持目录 sync 的文件系统会被报告而不是当作写失败。

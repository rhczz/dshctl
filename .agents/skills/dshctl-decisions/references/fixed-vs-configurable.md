# 固定 vs 配置：分类与误判

三问程序只有三句话，真正难的是把仓库里已有的值放对类别，以及识别"看起来该可配、其实不该"的请求。这份清单是判例。

## 分类表

| 值 | 类别 | 真源与理由 |
|---|---|---|
| `repoDir`、`port`、`nodeVersion` | 可配置 | 每台机器的 checkout 与 Node 安装都不同，且今天就有消费者（操作者、多端口并存） |
| `startTimeoutSeconds`、`stopTimeoutSeconds`、`lockTimeoutSeconds`、`logRotateBytes` | 可配置 | 机器速度与日志量随部署变化；`logRotateBytes` 已写明 `0` 表示不轮转、非 0 时下限 65536 |
| 退出码 `0/1/2/3/4/5/130` | 固定：外部契约 | 脚本按退出码分支，改动等于破坏消费者；定义在 `internal/exitcode`，语义分类由该包文档固定 |
| `--json` 的字段名 | 固定：外部契约 | 同上，脚本消费的是字段而不是文案 |
| 状态目录文件名 `config.json`、`dshctl.lock`、`dsh-web-<端口>.state.json` | 固定：协议常量 | 三个包按名字协作；文件名可配只会让"哪个文件是权威"变成运行时问题 |
| `dsh web: <url>` 地址行格式 | 固定：外部规范 | 这是被管理服务打印的格式，不是 dshctl 的选择；解析正则在 `internal/kernel/start.go` |
| 构建标记 `.dsh-build/client-build-environment.json` | 固定：跨进程契约 | 构建写、`start` 与 `doctor` 读，三个包必须一致，由 `internal/kernel/contracts_test.go` 钉住 |
| checkout 标记 `package.json` 与 `pnpm-workspace.yaml` | 固定：外部规范 | 它们属于被管理的仓库 |
| Node 下限 `24.12.0` | 固定：安全不变量 | README 明确写了"任何配置项、参数或环境变量都改不动它"；下调等于放行未验证的运行时 |
| 进程归属 = 运行记录 + 启动时间指纹 | 固定：安全不变量 | 只凭 pid 会在 pid 回收后杀错进程；`internal/state` 包文档是理由的家 |
| "探测不了"不当作"没有" | 固定：安全不变量 | 把探测失败当成"端口空闲"会启动第二个服务或误杀；`internal/host` 与 `internal/kernel` 各有一条 |
| 状态文件 0600、状态目录 0700、写入原子替换 | 固定：安全不变量 | 权限与替换方式不是部署偏好 |
| 日志轮转"复制到 `.old` 后截断同一 inode" | 固定：正确性不变量 | 子进程持有 append 句柄，rename 会把后续输出写进备份文件；`internal/logfile` 包文档 |
| 轮询间隔、等待循环节奏、锁重试节奏、日志尾部匹配窗口 | 固定：内部调度 | 没有部署会想调它；把它做成开关只会把内部实现暴露成契约 |
| 测试用的注入点（虚构进程表、helper 标记） | 固定：测试基础设施 | 测试钩子不是可配置性；暴露成配置等于让生产路径也能伪造归属证据 |

## 常见误判

- **把内部调度做成开关。** "轮询间隔太慢，加个 `--poll-interval`"：没有消费者，且一旦可配就再也不能自由改实现。要么写死，要么按实际瓶颈改那个常量。
- **把安全不变量做成开关。** "加个 `--allow-old-node` 方便本地调试"：这类请求的正确回答是拒绝并说明代价，而不是加一个默认关闭的逃生门。逃生门会被打开。
- **用 `DEFAULT_*` 冒充可配置。** 常量有默认值不等于有人能改它；判据是入口在哪、谁在用。
- **给没有消费者的值加配置项。** 上游的表述是"配置字段留给随部署变化、且当前有消费者的选择"。今天没人要设的值，明天可以再加。
- **把测试钩子暴露成公开配置。** 测试需要注入的形状不应该出现在操作者能碰到的表面上。
- **把"可配置"当成不需要默认值的借口。** 可配置的项仍然要有一个经过验证的默认值，否则等于把选择推给操作者。

## 判例

**"日志显示多少行"**——`logs -n` 是命令参数，不是配置项：它随一次调用变化，不随部署变化。所以它是参数，有默认值 200。

**"构建超时"**——构建路径本身没有 deadline：只有 Node 探测受 `probeTimeout` 限制（`internal/nodejs`、`internal/host`），`startTimeoutSeconds` 只管"等端口就绪"（`start.go` 的 `waitForListening`）。今天没有消费者要求区分构建超时，所以不新增键；等真的有人需要，再加，并同时写决策记录。

**"Node 版本"**——四个来源（`--node` > `DSH_NODE_VERSION` > 文件 > PATH），最后一层不是默认值而是"按 PATH 解析"，解析结果在首次成功启动后写回文件。这是可配置性的正例：每个值都有来源标签，`-v` 会打印出来。

## 上游依据

- "No hardcoded tunables… **Protocol constants, external specs, and security invariants stay fixed.**"（deepseek-harness `AGENTS.md`）
- "Repo policy reserves `Config` fields for deployment-varying choices **with a current consumer**."（同一仓库的决策记录）
- "A `DEFAULT_*` constant or test hook is not configurability."（同上）
- "Misconfiguration fails loud… never silently skip a missing referent."（同上）

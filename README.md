# dshctl

管理本机运行的 DeepSeek Harness Web 服务：后台启动、停止、重启、构建、更新与体检。

macOS、Linux、Windows（amd64 / arm64）都支持。除 `pnpm`、`git` 与 Node 本身外，不需要安装其他工具。

Node 用 PATH 上的那个（nvm、fnm、Homebrew、n、Volta、asdf、mise、官方安装包都一样），
首次成功启动后会把它写进配置文件，之后固定使用该版本；低于 24.12.0 一律拒绝启动。

## 编译

```sh
make build          # → bin/dshctl
make install        # → ~/.local/bin/dshctl
make cross          # 交叉编译 6 个平台 → dist/
```

没有 make 时：

```sh
go build -o bin/dshctl ./cmd/dshctl      # 需要 Go 1.24+
```

## 快速上手

```sh
# 第一次：指定仓库路径，构建，然后启动
# （路径会写进配置文件，之后执行命令不用再带 --repo）
dshctl --repo ~/projects/deepseek-harness build
dshctl --repo ~/projects/deepseek-harness start

# 以后
dshctl status                 # 运行状态
dshctl url                    # 打印带 token 的访问地址
dshctl logs -f                # 跟随日志（Ctrl-C 退出）
dshctl restart                # 重启
dshctl stop                   # 停止
```

`start` 只检查构建产物，不会自动 install/build；缺依赖或没构建过时会提示先执行 `dshctl build`。

## 命令

| 命令 | 说明 |
| --- | --- |
| `start` | 后台启动并等待端口就绪（不加命令名时的默认命令）；已在运行时不会启动第二个 |
| `stop` | 停止服务并结束它所在的整棵进程树；不加 `--port` 时停止本状态目录管理的每一个服务 |
| `restart` | 在同一把锁内先停后启；不加 `--port` 时重启本状态目录中正在运行的每一个服务 |
| `status` | 运行状态；不加 `--port` 时报告本状态目录管理的每一个服务；`--json` 输出结构化结果 |
| `url` | 打印带 token 的访问地址；不加 `--port` 时每个运行中的实例一行；一个地址都没有时退出码 3 |
| `logs` | 日志；`-n <行数>`（默认 200 行，见 `internal/app.DefaultLogLines`）、`-f/--follow` 跟随、`--build` 只看最近一次 build/update/rollback 记录 |
| `build` | 清理已删除包的残留目录后执行 `pnpm run build` |
| `timeline` | 查看当前版本与 `origin/master` 的差距：落后/领先的提交数、差距内的 tag、最近的提交与部署历史；`--json` 输出结构化结果 |
| `update` | 更新到指定版本（`latest`/tag/commit，默认 `latest`）：停服 → `git fetch` → 切换 → 清理 → `pnpm install` → 构建 → 恢复启动 |
| `rollback` | 回退到之前部署过的位置：不带参数退 1 步、`-n <步数>` 退多步、`<tag>/<commit>` 定点回退；不联网 |
| `doctor` | 只读体检；`--json` 输出结构化结果 |
| `version` | 版本、提交、构建时间与目标平台；`--json` 输出结构化结果 |

`dshctl -h` 在一屏里列出每个命令的用法、参数与示例；`dshctl help <命令>`（或
`dshctl <命令> -h`）打印单个命令的完整说明：参数、退出码与注意事项。

全局参数写在命令名之前：

| 参数 | 等价环境变量 | 说明 |
| --- | --- | --- |
| `--repo <路径>` | `DSH_REPO_DIR` | 仓库位置，默认 `~/deepseek-harness`；配置里没写明时成功运行后写入（见「仓库目录」） |
| `--port <端口>` | `DSH_PORT` | 监听端口，默认 `3080`；同时是「只操作这一个实例」的选择器（见「多个实例」） |
| `--node <版本>` | `DSH_NODE_VERSION` | 指定 Node 版本；配置里没写明时成功启动后写入（见「Node 版本」） |
| `--config <文件>` | `DSHCTL_CONFIG` | 配置文件路径 |
| `-v` | — | 打印生效配置及每一项的来源 |
| `-h` / `-V` | — | 帮助 / 版本 |

## 配置项（config.json）

配置文件默认是 `<状态目录>/config.json`（即 `~/.dsh/dshctl/config.json`），首次执行可变命令时按默认值生成。

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `repoDir` | 字符串 | `~/deepseek-harness` | 被管理的 checkout，必须是绝对路径或 `~/` 开头；缺省时按默认值走，首次成功运行后写入（见「仓库目录」） |
| `port` | 整数 | `3080` | 监听端口，取值 1–65535 |
| `nodeVersion` | 字符串 | 无（未确定） | 这个安装使用的 Node 版本，例如 `"24.20.0"`；缺省时按 PATH 解析，首次成功启动后写入（见「Node 版本」） |
| `startTimeoutSeconds` | 整数 | `90` | 等待端口就绪的上限，1–86400 秒 |
| `stopTimeoutSeconds` | 整数 | `15` | 等待服务停止的上限，1–86400 秒 |
| `lockTimeoutSeconds` | 整数 | `10` | 等待另一把操作锁的上限，1–86400 秒 |
| `logRotateBytes` | 整数 | `4194304`（4 MiB） | 日志轮转阈值；`0` 表示不轮转；非 0 时不得小于 `65536` |

```json
{
  "repoDir": "/Users/you/projects/deepseek-harness",
  "port": 3080,
  "nodeVersion": "24.20.0",
  "startTimeoutSeconds": 90,
  "stopTimeoutSeconds": 15,
  "lockTimeoutSeconds": 10,
  "logRotateBytes": 4194304
}
```

规则：

- 每个字段都可以省略、删除或写成 `null`，都会退回到该字段的默认值；未知字段会被拒绝（防止把一个拼错的键当成生效配置）。
- 配置文件必须是普通文件、不超过 64 KiB，内容是单个 JSON 对象；解析失败会明确指出是哪个文件、哪个字段。
- 只有可变命令（`start`/`stop`/`restart`/`build`/`update`/`rollback`）会创建和写入它；`status`、`url`、`logs`、`doctor`、`version` 不写盘。
- dshctl 只在自己确有必要时改这个文件：写入它实际用过的 `repoDir`（`start`/`build`/`update`/`rollback`）与 `nodeVersion`（`start`），而且只写这两个键、只在这个文件还没有写明它们的时候写；你在文件里写过的值永远不会被覆盖，其他字段逐字保留。
- 首次执行可变命令时生成的配置里**不含** `repoDir`：默认值只是「按这台机器的主目录猜的路径」，把猜测写进配置就等于把猜错的结果永久固定下来。
- `dshctl -v <命令>` 会把生效值和每一项的来源（`flag` / `env` / `file` / `default`）打印出来，排查配置时先看它。

## 环境变量

| 变量 | 作用 | 缺省值 |
| --- | --- | --- |
| `DSHCTL_STATE_DIR` | 状态目录（dshctl 自己的文件都放这里） | `$DSH_HOME/dshctl`，再退回 `~/.dsh/dshctl` |
| `DSH_HOME` | DSH 主目录，状态目录的父目录 | `~/.dsh` |
| `DSHCTL_CONFIG` | 配置文件路径 | `<状态目录>/config.json` |
| `DSH_LOG_FILE` | 日志文件路径 | `<状态目录>/dsh-web.log` |
| `DSH_REPO_DIR` | 仓库目录，等价 `--repo` | `~/deepseek-harness` |
| `DSH_PORT` | 监听端口，等价 `--port` | `3080` |
| `DSH_NODE_VERSION` | Node 版本，等价 `--node`；覆盖配置文件里的 `nodeVersion`（仅本次运行） | 按 PATH 解析 |

规则：

- 路径类变量必须是绝对路径或以 `~` 开头（不支持 `~user`）；相对路径会被拒绝，因为它会让状态目录和操作锁跟着当前目录漂移。
- 只含空白的变量视为未设置。
- 优先级：命令行参数 > 环境变量 > 配置文件 > 默认值。所有配置项都按这个顺序，没有例外。两处补充：Node 版本的最后一层不是默认值，而是「没人指定就按 PATH 解析」（见「Node 版本」）；`repoDir` 在配置文件里的值与内置默认值完全相同时按默认值处理，不算你做过选择（见「仓库目录」）。
- dshctl 另外读取操作系统自身的 `PATH`（解析 `node`、`pnpm`、`git`，以及 Unix 上的 `lsof`/`ss`/`netstat`/`ps`）和 `HOME`（Windows 上是 `USERPROFILE`）来确定主目录与默认路径；这两个不是 dshctl 的配置项，但会决定上面这些默认值。
- 不可配置：Node 最低版本 `24.12.0` 是代码里的常量，任何配置项、参数或环境变量都改不动它；状态目录内的文件名（`dshctl.lock`、`dsh-web-<端口>.state.json`）也是固定的。

## Node 版本

Node 版本可以来自四个地方，优先级是 `--node` > `DSH_NODE_VERSION` > 配置文件 `nodeVersion` > PATH：

- 配置里没有 `nodeVersion` 时，dshctl 用 PATH 上的那个 node（nvm、fnm、Homebrew、n、Volta、asdf、mise、官方安装包都一样），并在首次成功启动后把它写进配置，此后固定使用该版本。
- `--node` 与 `DSH_NODE_VERSION` 都只影响本次运行，不写配置；只有当配置里还没有版本时，成功启动才会把用到的版本写进去。用环境变量覆盖配置里的版本时会打印一行说明，因为导出的变量在命令里看不见。
- 任何来源只要低于 `24.12.0` 都会被拒绝启动（退出码 4），并打印各安装方式的安装命令；高于已验证大版本（24.x）会警告但继续。
- 若 PATH 上是版本管理器的转发条目（shim / 符号链接），dshctl 会解析出真正的解释器并把它的目录前置给服务进程。
- `doctor` 显示当前会使用哪个 node，`status` 的运行记录里带着正在跑的服务所用的版本与仓库目录。

## 仓库目录

`repoDir` 可以来自四个地方，优先级是 `--repo` > `DSH_REPO_DIR` > 配置文件 `repoDir` > 默认 `~/deepseek-harness`：

- 默认值只是猜测：dshctl 从不把它写进配置，配置里写着同一个路径时也按默认值处理，`-v` 会显示成 `default(配置文件中的 repoDir 与默认值相同)`。所以某个旧版本写下的猜测不会把你的机器永久钉在一个不存在的目录上。
- 配置里没有写明 `repoDir` 时，一次成功运行会把它实际用过的 checkout 写进配置：`start`（服务起来了）、`build`、`update`。这就是 `dshctl --repo /path/to/checkout start` 之后 `doctor`、`update`、`start` 都自动用同一个 checkout 的原因。
- 配置里写明了另一个 checkout 时，dshctl 不会改它：本次运行用命令行/环境变量指定的那份，并打印一行「本次使用仓库 …（配置中为 …）」；用 `DSH_REPO_DIR` 覆盖时打在标准错误上（导出的变量在命令里看不见）。
- 运行记录里带着「这份服务是从哪个 checkout 起来的」，所以 `status` 显示的是**正在运行的服务**用的目录，`doctor` 在它与配置不一致时会多报一行 `服务仓库`。
- 配置指向一个不存在的目录时，`stop` 仍然可用（结束进程只看运行记录，不看配置）；`doctor` 的失败行会告诉你怎么指定一次路径、之后就自动记住。

## 状态目录

dshctl 自己的文件都在一个目录里，默认 `~/.dsh/dshctl`：

```
config.json                 配置（首次执行可变命令时按键的默认值生成；成功运行后写入 repoDir、成功启动后写入 nodeVersion）
dsh-web-<端口>.state.json   运行记录：监听进程 pid、启动时间、端口、访问地址、所用 Node 版本与仓库目录
updates.json                部署位置历史：每个 checkout 一组「曾经部署到的提交」（新→旧，最多 50 条）
dshctl.lock                 操作互斥锁
dsh-web.log                 服务与 build/update/rollback 输出（超过 4 MiB 轮转为 .old）
```

这个目录可以随时删掉，不影响 DSH 的会话、附件、设置与凭据（它们由 DSH 自己放在 `~/.dsh` 下）。
`status`、`url`、`logs`、`doctor`、`version` 不写盘；`timeline` 是唯一例外：它会
`git fetch`（只写 `.git` 的远程跟踪引用），不写状态目录、不改工作区。

## 多个实例

同一个状态目录可以管理多个服务：运行记录按端口分开，每个端口一个
`dsh-web-<端口>.state.json`。配置项里的 `port`（或 `DSH_PORT`）决定
`start` 在哪个端口上启动，也是**不点名时排在第一位**的那个实例。

`--port`（或 `DSH_PORT`）同时是选择器：

- **指定端口**时，`status`/`stop`/`restart`/`url` 只作用于该端口上的服务。
- **不指定端口**时，它们作用于本状态目录管理的每一个服务——`status` 报告全部，
  `stop` 停止全部，`restart` 重启所有正在运行的实例，`url` 打印所有运行中的地址
  （打印到地址就退出 0，一个都没有才退出 3）。
  `status` 的第一个（标准输出上的那个）是配置里的端口；其余实例接在后面，
  并打在标准错误上（含各自的 `访问:` 地址）。

```sh
dshctl --port 3080 start
dshctl --port 3081 start      # 两个服务并存，两条运行记录
dshctl status                 # 报告 3080 与 3081
dshctl --port 3081 stop       # 只停 3081，3080 继续服务
dshctl stop                   # 停止本状态目录管理的全部服务
```

不指定端口的命令只作用于**本状态目录**里记着运行记录的服务：dshctl 不会去扫端口，
也不会结束任何不是它启动的进程。端口被别的程序占用时只提示并跳过，`stop` 与
`restart` 都会保留它的运行记录：有记录、但记录的进程无法确认，或记录与端口上的进程
对不上时，不点名的 `stop` 以退出码 4 结束，说明有一个本应停掉的实例还留在那里；
端口上只是别人的程序、本状态目录从没在那里启动过服务时不算漏，因为那不是这次操作
要管的东西。点名一个端口时同样不算漏：保留那个占用者正是这次操作要的结果。

`build`/`update`/`rollback` 仍然只看配置里那个 checkout：它们会在替换产物之前检查所有端口，
任何一个正在用这份 checkout 的服务都会让它拒绝执行，并提示先停掉对应端口。

## 版本、更新与回退

`update` 不再盲目跟到 master 尖端：它接受一个目标版本；`rollback` 回到 dshctl
记录过的位置。先看差距再决定更新，是这个功能存在的理由。

```sh
dshctl timeline                    # 当前版本 vs origin/master：差距、tag、提交、部署历史
dshctl update                      # 更新到 origin/master 最新（等价 dshctl update latest）
dshctl update dsh-v0.1.6-alpha.2   # 切到该 tag 所在的提交
dshctl update 1a2b3c4              # 切到该 commit（支持完整或缩写 hash）
dshctl rollback                    # 回到上一次 update/rollback 之前所在的位置
dshctl rollback -n 3               # 回退 3 步
dshctl rollback dsh-v0.1.5-rc.2    # 定点回退到某个版本（不联网）
```

`timeline` 的输出：头部是当前版本、远程版本与差距（落后/领先/分叉），中间是
「最新 10 个提交 ∪ 差距内全部 tag ∪ 当前 ∪ 远程尖端」的列表（当前用 `●` 标注、
远程尖端用 `○`，中间省略的提交数会写明），末尾是 `updates.json` 里的部署历史。
工作区有未提交修改时会多一行提示。`--json` 输出结构化结果。

规则：

- `latest` 固定指 `origin` 的 `master`，不跟随当前分支的 upstream；仓库没有
  `origin` 时 `timeline` 与 `update latest` 拒绝执行（指定 tag/commit 仍可用）。
- 本地分支名不是版本：`dshctl update master` 会被拒绝，因为本地 master 可能落后于
  `origin/master`，而它会读起来像"最新的 master"；要远程最新用 `latest`，要具体
  提交用 tag 或 hash。
- 指定 tag/commit 时用 detached HEAD 检出，不移动 master 分支指针；之后不带参数
  的 `update` 会回到 master 并快进到 `origin/master`。
- 所有检查（版本能否解析、工作区是否干净）都在停止服务之前完成；目标就是当前
  版本时不会停服、不构建、不重启。
- 工作区有已跟踪文件的未提交修改时拒绝更新/回退；未跟踪文件（自己的插件目录等）
  原样保留。
- `rollback` 不联网：回到已知位置是救火路径，断网也必须可用。`update <tag|commit>`
  在 fetch 失败但目标本地已知时降级为本地解析并打印警告；`update latest` 无法降级。
- 更新历史存在 `<状态目录>/updates.json`：每个 checkout 一组位置（新→旧，最多
  50 条），git 的 HEAD 始终是「当前在哪」的唯一真源。文件损坏时 `timeline` 警告、
  `rollback` 拒绝、`update` 以当前位置重建。
- `timeline` 是唯一会写 `.git` 的报告命令（它必须 `git fetch` 才能知道远程最新）；
  它不写状态目录、不改工作区。fetch 失败时仍打印本地已知状态、明确标注「远程未
  确认」、绝不出现「已是最新」，并以退出码 4 结束。

## 退出码

| 码 | 含义 |
| --- | --- |
| 0 | 成功；`status` 表示运行中或启动中 |
| 1 | 失败 |
| 2 | 用法或配置错误 |
| 3 | 服务未运行（`status` 以配置端口为准；`url` 是一个地址都没有），或端口被其他进程占用 |
| 4 | 前置检查失败（缺 node/pnpm、不是 checkout、未构建、端口被占用或无法探测、版本无法解析、工作区有已跟踪修改、`timeline` 无法获取远程更新），或 `stop` 漏掉了一个它无法确认归属的实例 |
| 5 | 锁超时（另一个 dshctl 操作正在进行） |
| 130 | 命令被 Ctrl-C 取消 |

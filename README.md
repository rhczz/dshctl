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
| `start` | 后台启动并等待端口就绪（不加命令名时的默认命令） |
| `stop` | 停止服务，并结束它所在的整棵进程树 |
| `restart` | 在同一把锁内先停后启 |
| `status` | 运行状态；`--json` 输出结构化结果 |
| `url` | 打印带 token 的访问地址；未运行时退出码 3 |
| `logs` | 日志；`-n <行数>`、`-f/--follow` 跟随、`--build` 只看最近一次构建记录 |
| `build` | 清理已删除包的残留目录后执行 `pnpm run build` |
| `update` | 停服 → `git pull --ff-only` → 清理 → `pnpm install` → 构建 → 恢复启动 |
| `doctor` | 只读体检；`--json` 输出结构化结果 |
| `version` | 版本、提交、构建时间与目标平台；`--json` 输出结构化结果 |

全局参数写在命令名之前：

| 参数 | 等价环境变量 | 说明 |
| --- | --- | --- |
| `--repo <路径>` | `DSH_REPO_DIR` | 仓库位置，默认 `~/deepseek-harness` |
| `--port <端口>` | `DSH_PORT` | 监听端口，默认 `3080` |
| `--node <版本>` | `DSH_NODE_VERSION` | 指定 Node 版本，仅本次生效（不写配置） |
| `--config <文件>` | `DSHCTL_CONFIG` | 配置文件路径 |
| `-v` | — | 打印生效配置及每一项的来源 |
| `-h` / `-V` | — | 帮助 / 版本 |

## 配置项（config.json）

配置文件默认是 `<状态目录>/config.json`（即 `~/.dsh/dshctl/config.json`），首次执行可变命令时按默认值生成。

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `repoDir` | 字符串 | `~/deepseek-harness` | 被管理的 checkout，必须是绝对路径或 `~/` 开头 |
| `port` | 整数 | `3080` | 监听端口，取值 1–65535 |
| `nodeVersion` | 字符串 | 无（未确定） | 这个安装使用的 Node 版本，例如 `"24.20.0"`；缺省时按 PATH 解析，首次成功启动后写入（见下节） |
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
- 只有可变命令（`start`/`stop`/`restart`/`build`/`update`）会创建和写入它；`status`、`url`、`logs`、`doctor`、`version` 不写盘。
- dshctl 只在自己确有必要时改这个文件：首次成功启动后写入 `nodeVersion`，且只改这一个键，其他字段逐字保留。
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
| `DSH_NODE_VERSION` | Node 版本，等价 `--node`；**只在配置里没有 `nodeVersion` 时生效** | 按 PATH 解析 |

规则：

- 路径类变量必须是绝对路径或以 `~` 开头（不支持 `~user`）；相对路径会被拒绝，因为它会让状态目录和操作锁跟着当前目录漂移。
- 只含空白的变量视为未设置。
- 优先级：命令行参数 > 环境变量 > 配置文件 > 默认值。**唯一的例外是 `DSH_NODE_VERSION`**：`--node` > 配置文件 `nodeVersion` > `DSH_NODE_VERSION` > PATH，因为写进配置的那个版本是这台机器上验证过能跑的。
- dshctl 另外读取操作系统自身的 `PATH`（解析 `node`、`pnpm`、`git`，以及 Unix 上的 `lsof`/`ss`/`netstat`/`ps`）和 `HOME`（Windows 上是 `USERPROFILE`）来确定主目录与默认路径；这两个不是 dshctl 的配置项，但会决定上面这些默认值。
- 不可配置：Node 最低版本 `24.12.0` 是代码里的常量，任何配置项、参数或环境变量都改不动它；状态目录内的文件名（`dshctl.lock`、`dsh-web-<端口>.state.json`）也是固定的。

## Node 版本

Node 版本可以来自四个地方，优先级是 `--node` > 配置文件 `nodeVersion` > `DSH_NODE_VERSION` > PATH：

- 配置里没有 `nodeVersion` 时，dshctl 用 PATH 上的那个 node（nvm、fnm、Homebrew、n、Volta、asdf、mise、官方安装包都一样），并在首次成功启动后把它写进配置，此后固定使用该版本。
- `--node` 只影响本次运行，不写配置；只有当配置里还没有版本时才会写进去。
- 任何来源只要低于 `24.12.0` 都会被拒绝启动（退出码 4），并打印各安装方式的安装命令；高于已验证大版本（24.x）会警告但继续。
- 若 PATH 上是版本管理器的转发条目（shim / 符号链接），dshctl 会解析出真正的解释器并把它的目录前置给服务进程。
- `doctor` 显示当前会使用哪个 node，`status` 的运行记录里带着正在跑的服务所用的版本。

## 状态目录

dshctl 自己的文件都在一个目录里，默认 `~/.dsh/dshctl`：

```
config.json                 配置（首次执行可变命令时按默认值生成；首次成功启动后写入 nodeVersion）
dsh-web-<端口>.state.json   运行记录：监听进程 pid、启动时间、端口、访问地址、所用 Node 版本
dshctl.lock                 操作互斥锁
dsh-web.log                 服务与 build/update 输出（超过 4 MiB 轮转为 .old）
```

这个目录可以随时删掉，不影响 DSH 的会话、附件、设置与凭据（它们由 DSH 自己放在 `~/.dsh` 下）。
`status`、`url`、`logs`、`doctor`、`version` 不写盘。

同一个状态目录可以管理多个端口，运行记录按端口分开：

```sh
dshctl --port 3080 start
dshctl --port 3081 start      # 两个服务并存
dshctl --port 3081 stop       # 只停 3081
```

## 退出码

| 码 | 含义 |
| --- | --- |
| 0 | 成功；`status` 表示运行中或启动中 |
| 1 | 失败 |
| 2 | 用法或配置错误 |
| 3 | 服务未运行（`status`/`url`），或端口被其他进程占用 |
| 4 | 前置检查失败（缺 node/pnpm、不是 checkout、未构建、端口被占用或无法探测） |
| 5 | 锁超时（另一个 dshctl 操作正在进行） |
| 130 | 命令被 Ctrl-C 取消 |

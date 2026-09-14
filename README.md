# dshctl

管理本机运行的 DeepSeek Harness Web 服务：后台启动、停止、重启、构建、更新与体检。

macOS、Linux、Windows（amd64 / arm64）都支持。除 `pnpm`、`git` 与 Node 本身外，不需要安装其他工具。

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
| `--node <版本>` | `DSH_NODE_VERSION` | Node 版本，可写 `latest` |
| `--config <文件>` | `DSHCTL_CONFIG` | 配置文件路径 |
| `-v` | — | 打印生效配置及每一项的来源 |
| `-h` / `-V` | — | 帮助 / 版本 |

其余环境变量：`DSHCTL_STATE_DIR`（状态目录）、`DSH_HOME`（DSH 主目录，默认 `~/.dsh`）、`DSH_LOG_FILE`（日志路径）。

## 状态目录

dshctl 自己的文件都在一个目录里，默认 `~/.dsh/dshctl`：

```
config.json                 配置（首次执行可变命令时按默认值生成）
dsh-web-<端口>.state.json   运行记录：监听进程 pid、启动时间、端口、访问地址
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

## 开发

```sh
make check          # gofmt -s 校验 + go vet + 测试
make test-race      # 竞态检测
make hermetic       # 验证测试不在临时目录之外写任何东西
make ci             # 与 CI 一致：工作流校验 + 格式 + vet + 密闭性 + 竞态测试
```

CI 在 ubuntu / macos / windows 三个平台编译并测试，全部通过后才交叉编译 6 个平台。
打 `v*` 标签会触发 Release 工作流，把各平台产物发布到对应的 release。

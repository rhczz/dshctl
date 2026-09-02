# dshctl

管理 DeepSeek Harness(deepseek-harness)源码仓库里 `dsh web` 后台服务的命令行工具。

仓库更新到最新版后,用 `dshctl update` 一条命令就能完成:停服 → 拉代码 → 装依赖 → 构建 → 重启。

## 特性

- 单文件 bash,无依赖安装,放到 PATH 里就能用
- 端口 + PID 文件双重判定进程,不会误杀其他程序
- 启动失败自动清理进程和残留文件
- update / build 输出实时显示,同时落到同一份日志
- 构建前自动清理已被上游删除的包目录残留,避免陈旧构建产物导致构建失败

## 依赖

- bash、node(≥ 24.12,建议 24.20.0)、pnpm
- 脚本会优先使用 nvm 里 `DSH_NODE_VERSION` 指定的版本,没有则用 PATH 里的 node
- macOS / Linux

## 安装

```sh
curl -L -o ~/.local/bin/dshctl \
  https://raw.githubusercontent.com/rhczz/dshctl/main/dshctl
chmod +x ~/.local/bin/dshctl
```

## 配置

默认只改一个变量:把 `DSH_REPO_DIR` 指向你的 deepseek-harness 克隆路径,例如写入 `~/.zshrc`:

```sh
export DSH_REPO_DIR="$HOME/deepseek-harness"
```

可选变量:

| 变量 | 默认值 | 说明 |
|---|---|---|
| `DSH_REPO_DIR` | `~/deepseek-harness` | 仓库路径 |
| `DSH_PORT` | `3080` | 监听端口 |
| `DSH_LOG_FILE` | `~/.local/state/dsh/dsh-web.log` | 日志文件 |
| `DSH_PID_FILE` | `~/.local/state/dsh/dsh-web.pid` | PID 文件 |
| `DSH_NODE_VERSION` | `24.20.0` | 优先使用的 Node 版本 |

## 用法

```sh
dshctl             # 启动(等价于 dshctl start)
dshctl start       # 后台启动并等待端口就绪
dshctl stop        # 停止
dshctl restart     # 重启
dshctl status      # 查看运行状态
dshctl logs        # 跟随日志
dshctl build       # 执行 pnpm run build
dshctl update      # git pull --ff-only + pnpm install + build,服务运行中会自动停服再重启
```

## 说明

- 第一次使用前先跑一次 `dshctl build`(或 `dshctl update`),`start` 不自动安装依赖和构建。
- 所有子命令输出带互斥锁,并发执行会等待或报错。

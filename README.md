# dshctl

管理 DeepSeek Harness(deepseek-harness)源码仓库里 `dsh web` 后台服务的命令行工具。

`dsh web` 的界面在浏览器里,但 npx / pnpm 只能以前台进程方式启动,命令长且占用终端,因此把它收进后台,一条命令完成启停、日志与更新。

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

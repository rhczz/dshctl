# 机制与资源：谁拥有什么

判据：**把这个包拿去给另一个产品用，需要改它的源码吗？** 需要，就说明产品值写死在了机制里。

## 机制（可复用、与产品无关）

| 包 | 机制 |
|---|---|
| `atomically` | 崩溃安全的原子替换（临时文件 + rename + 目录同步 + 残留清扫） |
| `lock` | 内核拥有的咨询锁（inode 校验、超时、删除后重新获取） |
| `logfile` | 单文件的段落语法引擎：标记写入、按大小轮转（复制后截断同一 inode）、尾读、跟随 |
| `state` | 严格单文档 JSON 存储：大小上限、普通文件规则、原子替换、替换期间重试、残留清理 |
| `run` | 外部命令执行：进程树结束、输出上限、退出码分类、PATH 前缀 |
| `detach` | 脱离终端的子进程与收割（僵尸进程不算存活） |
| `host` | 操作系统事实与端口归属：探测、解析、发信号 |
| `paths` | 路径校验与展开（绝对路径、`~`、`~user` 拒绝） |

## 资源与策略（定义在拥有契约的层）

- 文件名（`dsh-web-<port>.state.json`、`updates.json`、`dshctl.lock`、`dsh-web.log`）、
  日志标记里的产品名与时间格式、记录 schema（字段与 JSON 键）、大小与条数上限
  （64 KiB 记录、50 条位置、4 MiB 轮转）、Node 下限、探针工具清单与候选顺序、
  dshctl 的环境变量名、`dsh-web:` 地址行格式、`latest` = `origin/master`。
- 这些值要么是 `internal/service` 与 `internal/domain` 的常量/类型，要么以参数或显式
  类型传进机制包（例如日志格式、记录类型、工具清单、上限）。
- **去向**：需要给机制换"另一套值"时只改资源层，不改机制包。机制的测试只验证机制
  （用测试自有的格式/类型），资源值的测试在资源层，并配 `MUTATIONS` 锚点。

## 迁移工作清单（已知写死在机制里的产品值）

- `state`：**已提取**——`state.Store[T]` 只认识"有界、严格、原子替换的 JSON 文档"，
  由调用方给出 `MaxBytes`、`Validate`、`Stamp`；运行时记录的 schema 在领域层、
  文件名模式在 `config`、上限与校验在服务层的 `records.go`。测试用测试自有的
  `testDoc` 验证机制。
- `logfile`：**已提取**——标记的形状是 `logfile.Format`（前缀、产品名、时间布局），
  由服务层的 `logFormat` 提供；窗口大小（`64 KiB/8 MiB/32 MiB`）仍是机制的安全上限，
  并在包文档里这么标注。
- `host`：**已提取**——工具清单与顺序是 `host.Tools`（`Port` 顺序 + `Process` 名），
  由服务层的 `hostTools` 给出；解析各工具方言仍是机制，配置里不认识的工具名会
  作为"探测失败"上报而不是被静默跳过。
- `paths`：**判定为资源包**（不拆分）——它拥有 dshctl 的路径契约：环境变量名、
  默认目录、状态目录与配置文件的派生。校验与展开（绝对路径、`~`、拒绝 `~user`）
  是机制部分，写在同包并由包文档标明；对外名字必须留在能被 README 与
  `readme-env` 检查看见的地方。
- `config`：四层优先级的合并机制与产品 schema 同处一包；目标是机制可被另一份 schema 复用。

## 未提取（已知，逐项独立提交）

- `internal/service/print.go` 与 `timeline.go` 的 `Print*`：只读命令的表格渲染。
  它们是 CLI 的参考渲染器，与 `ServeExitCode` 一起应当搬进 `internal/cli`；
  搬动会触及 8-10 个 Go 文件与账本约 46 行，因此单独提交。
- `internal/repo`：**已提取**——remote/branch（`Repo.Remote`/`Repo.Branch`）与 prune
  布局（`Repo.Residue`/`Repo.Areas`）由服务层的 `checkoutLayout` 给出；
  `history.Store.MaxRecords` 同样由调用方声明。

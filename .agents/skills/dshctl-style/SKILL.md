---
name: dshctl-style
description: 按 dshctl 的书写规范改代码与注释：gofmt -s、注释讲契约不讲过程、语言分工与 i18n 目录、错误包装、命名与文件布局、TODO 分级、折行与结尾换行。用于写新文件、评审风格、把外部代码改成本仓库写法，或 fmt-check/vet 报错时。
---

# dshctl 书写规范

## 概要

本仓库没有 linter：格式真源只有 `gofmt -s`，其余规范由本 skill 与 `scripts/check-conventions.py` 承担。规则的目标是让注释与命名承载**契约**（行为、失败、时序、归属、后果），而不是承载推理过程——推理属于提交信息与决策记录。

## 规则

1. **格式只跑 `make fmt` / `make fmt-check`**（即 `gofmt -s`）。不要在编辑器里另配一套格式化，也不要手工对齐：`-s` 做的是简化重写，手工结果与它冲突时 `make fmt-check` 在 CI 上变红。落点：`Makefile` 的 `fmt`/`fmt-check`，`check` 与 `ci` 都包含 `fmt-check`。

2. **注释用英文、讲契约不讲过程**：保留行为、失败、时序、归属、后果与非显而易见的取向；删除实现叙述、测试走查、评审历史与代码复述（DSH 的原话是 "Comments and docs state complete contracts and context, not reasoning transcripts"）。为什么：下一个改这段代码的人靠注释判断"能不能这样改"，而复述代码的注释会与代码一起腐烂。

3. **非测试 `.go` 文件的注释行 ≤ 88 列，按 ~80 列自然折行**。实测：2666 行注释中最宽 83 列（`internal/repo/prune.go:257`）、p95 80、p99 81、中位数 70；`scripts/check-conventions.py` 的 `go-comments` 规则以 88 为上限。为什么：一条注释是一条契约，超过一行宽度时它已经在写段落。

4. **包文档写模型与不变量**，不写实现目录。范例：`internal/kernel`（三条生命周期不变量）、`internal/host`（"探测不了"不是结论）、`internal/state`（记录的严格规则，以及为什么不跟随 symlink）、`internal/lock`（锁在 inode 上）、`internal/logfile`（轮转为什么截断同一 inode）、`internal/detach`（为什么必须 reap）。为什么：这些正是判断"这段代码能不能这样改"的依据；删掉它，理由就只剩 git 历史。

5. **每个导出标识符都要有文档注释，字段用 `// X is …` 句式**。范例：`internal/run` 的 `Result`/`ExitError`、`internal/exitcode` 的 `Error`、`internal/kernel` 的 `StartResult`/`StopResult`、`internal/state` 的 `Record`。

6. **语言分工：面向操作者的句子进目录，开发者读的用英文**。错误文案与命令 `Help` 必须走 `internal/i18n` 的消息目录（`Messages` 里一条消息两种语言，英文默认、机器语言为中文时中文，见 `dshctl-architecture`）；测试失败信息、标识符、注释、包文档、README 的开发者段落用英文。判据是读者：终端前的操作者读目录里的那句话，改代码的人读英文。**新代码里不再写死中文文案**；迁移尚未覆盖的旧文件，改动时顺手迁进目录。

7. **错误包装**：`fmt.Errorf("描述: %w", err)`；命令、参数、路径加反引号或 `%q`/`%s`；句尾不加句号。范例：`internal/detach` 的 `无法启动 %s: %w`、`internal/nodejs` 的 `` 无法执行 `%s -v`: %w ``。退出码与分类只用 `internal/exitcode` 的 `New`/`Wrap`，不自己造码。

8. **命名**：测试辅助用 `mustX`/`newX`/`writeX`（`internal/repo/prune_audit_test.go` 的 `mustCheckout`、`internal/kernel/fake_test.go` 的 `writeFile`）；虚构对象用 `fakeX`（`internal/kernel/fake_test.go` 的 `fakeProcess`）；正则与魔法数抽成有语义的包级 `var`/`const`（`internal/kernel/start.go` 的 `webURLPattern`）。

9. **单文件一概念**：新概念开新文件，而不是把无关逻辑堆进已有的大文件；平台实现只放 `_unix`/`_windows`/`_darwin`/`_linux`/`_other` 后缀的文件（细节见 `dshctl-portability`）。

10. **禁止 `panic` 与 `func init()`**：生产代码当前各 0 处，`scripts/check-conventions.py` 的 `go-forbidden` 规则会让新增的失败。为什么：panic 会结束操作者交给 dshctl 管理的服务；init 把初始化顺序藏到调用图之外。

11. **TODO/FIXME/XXX 分级并写触发条件**：FIXME = 应阻止发布，TODO = 资源允许时尽快，XXX = 最低优先级。当前树中为 0；新增必须在同一处写明"什么情况下该被处理"。不写"以后再说"式注解——会腐烂的状态注解属于 slop（见 [slop 清单](references/slop.md)）。

12. **文件恰好以一个换行结尾，行尾不留空格**。`scripts/check-conventions.py` 的 `trailing-newline` 规则覆盖 `.go`/`.md`/`.py`/`.sh`/`.yml`/`.yaml`/`.json`/`.txt` 与 `Makefile`、`.gitignore`。为什么：多一个或少一个结尾换行会进入之后的每一次 diff。

13. **一个事实一个家**：README = 操作者契约，包文档 = 模型与不变量，测试 = 被钉住的行为，`.agents/notes` = 为什么与放弃了什么，skill = 流程，git = 历史；别处只链接、不复制。判据见 [AGENTS.md](../../../AGENTS.md)。

## 触发与边界

- 适用：写新文件、改既有函数、把外部代码搬进本仓库、`make fmt-check` 或 `go vet` 报错。
- 不适用：通用 Go 风格争论走全局 skill；"该不该新增这个包/函数、这个值该固定还是该可配"走 `dshctl-decisions`；测试的写法走 `dshctl-testing`。
- 本 skill 只约束书写形态，不改行为；与机械门禁冲突时以 `scripts/check-conventions.py` 的判定为准。

## 验证

```sh
make fmt-check vet
python3 scripts/check-conventions.py      # --list 列出每条规则
```

## 相关文件

- [AGENTS.md](../../../AGENTS.md) — 不变量、固定 vs 配置、工作流
- [Makefile](../../../Makefile) — `fmt`/`fmt-check`/`check`/`ci` 目标
- [scripts/check-conventions.py](../../../scripts/check-conventions.py) — 被机械强制的规则与阈值
- [internal/run/run.go](../../../internal/run/run.go) — 错误包装与结果类型的范例
- [internal/kernel/start.go](../../../internal/kernel/start.go) — 包级正则与中文错误的范例
- [slop 清单](references/slop.md) — 见到就该删的七种写法与注释保留/删除对照

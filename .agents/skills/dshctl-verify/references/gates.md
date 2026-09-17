# dshctl 门禁表与失败定位

## 怎么用

先按改动类型在表里找到必跑的目标，跑完再看"常见失败信息"一节定位。表中的"实际执行"抄自 `../../../../Makefile`，改了 Makefile 就要回来同步这一页。

**分工**：标着"本地快检"的行在本地跑；标着 **CI** 的行由 GitHub Actions 跑，**本地不执行**（`AGENTS.md` 的「命令」一节）。本地跑全量只是把 CI 的时间花两遍，结论也不比 CI 全。

## 门禁表

| 目标 | 何时必跑 | 实际执行 | 覆盖什么 | 失败怎么读 |
|---|---|---|---|---|
| `make fmt-check` | 本地快检：任何 Go 改动 | `gofmt -s -l .`，有输出即失败 | 格式真源（`gofmt -s`，本仓库没有 linter） | 打印 `these files need gofmt -s -w:` 加文件列表 |
| `make fmt` | 修格式 | `gofmt -s -w .`，会重写文件 | 同上 | 不是门禁；跑完重跑 `fmt-check` |
| `make vet` | 本地快检：任何 Go 改动 | `go vet ./...` | 每个包**连同测试文件**的类型检查 | 编译错误，含文件与行号 |
| `make test` | **CI**（本地不跑全量） | `go test -timeout 600s ./...` | 全套行为与契约 | `--- FAIL: TestX`，先看断言的首句期望 |
| `make test-race` | **CI** | `go test -race -timeout 600s ./...` | 同上加数据竞争检测 | 竞争报告含两段 goroutine 栈 |
| `make hermetic` | **CI**：改测试隔离、新增 skip、碰 HOME/环境变量 | `../../../../scripts/hermetic-check.sh` | 一次性 HOME 下跑全套，断言临时目录之外零残留 | `tests created files in a real home directory:` 加路径 |
| `make coverage` | **CI**：改 `internal/nodejs` 或配置层决策 | hermetic 跑一次带 `-coverprofile`，再交 `../../../../scripts/check-coverage.py` | `internal/nodejs` 100% 硬门禁；`internal/config`、`internal/service` 只报告 | `FAIL internal/nodejs: xx.x% (要求 100%，a/b 条语句)` 加 `未覆盖:` 行 |
| `make mutation` | **CI**：改 `internal/nodejs` 或配置层决策 | `../../../../scripts/mutation-check.py`，逐条破坏决策并要求测试失败 | "测试真的会注意到破坏吗" | `ALIVE`/`INVALID`/`BLOCKED` 任一行 + `mutation(s) survived` |
| `make workflow-check` | **CI**：改 `.github/` | `../../../../scripts/check-workflow.py` | workflow 结构属性（触发、平台矩阵、构建门禁、产物、表达式引号） | `workflow check failed: …` |
| `make cross` | **CI**：平台代码改动；发布前 | 6 个 `GOOS/GOARCH` 交叉编译到 `dist/` | darwin/linux/windows × amd64/arm64 都能编译 | 某个目标的编译错误 |
| `make conventions` | 本地快检：改注释、依赖、skill 或 `AGENTS.md` | `../../../../scripts/check-conventions.py` | AGENTS.md 与 skill 的完整性、注释宽度、`panic`/`init`、零依赖与分层、结尾换行 | `检查失败: <rule>: …`，逐条见下 |
| `make check` | **CI**（本地只跑它的前三项） | `fmt-check` + `conventions` + `vet` + `test` | 上面四项 | 见各行 |
| `make ci` | **CI**（本地不执行） | `workflow-check` + `fmt-check` + `conventions` + `vet` + `coverage` + `go test -race -count=1 -timeout 600s ./...` | 全套门禁 | 见各行 |
| `make build` | 需要真实二进制做端到端验证 | `go build -ldflags … -o bin/dshctl ./cmd/dshctl` | 产物本身 | 编译错误 |
| `make help` | 忘了目标名 | 列出所有带 `## ` 说明的目标 | — | — |

辅助命令：`python3 scripts/mutation-check.py --list` 列出全部变异（当前 36 条），`--only <name>` 只跑一条（本地证明"这条变异会被抓住"就用它），`-v` 打印失败输出；`python3 scripts/check-conventions.py --list` 列出约定检查的规则；`python3 scripts/check-coverage.py <profile> --report all` 看每个包的覆盖率。

## 耗时基线

留给 CI 预算与读日志用；不是本地该跑的时长（见上表"分工"）。本机实测（热构建缓存，包并行运行，所以总时长约等于最慢的两个包而不是各包之和）：

- `make check` 整体 `real 1m56s`（含 `fmt-check`、`conventions`、`vet` 与测试）。
- `make ci` 整体 `real 3m57s`：在 `check` 之上再跑一次带覆盖率的 hermetic 套件与 `-race` 套件。
- 单包（约数，随机器波动）：`internal/cli` 约 110s、`internal/service` 约 106s、`internal/host` 约 21s、`internal/repo` 约 7s、`internal/lock` 约 6s、`internal/detach` 约 6s、`internal/logfile` 约 6s、`internal/nodejs` 约 5s、`internal/paths` 约 4s、`internal/exitcode` 约 4s、`internal/buildinfo` 约 3s、`internal/version` 约 3s、`internal/run` 约 3s、`internal/state` 约 3s、`internal/atomically` 约 2s、`internal/config` 约 2s、`cmd/dshctl` 约 1s。
- 冷构建缓存会额外付出编译时间；`make mutation` 比 `make ci` 更重（36 条变异，每条跑一次指定包），预算按十分钟量级准备。

`make test` 不带 `-count=1`：Go 会复用上一次通过的结果（输出 `(cached)`）。要确认某次修复真的重新执行过，用 `go test ./internal/<pkg>/ -count=1`。

## 常见失败信息 → 原因 → 下一步

1. `these files need gofmt -s -w:` + 文件列表 → 文件没按 `gofmt -s` 格式化 → `make fmt`，再看 diff 是否只动了格式。
2. `--- FAIL: TestXxx` → 行为断言失败 → `go test ./internal/<pkg>/ -run TestXxx -count=1 -v` 看首句期望；改实现前先确认是行为变了还是测试过时（`dshctl-tdd`）。
3. `tests created files in a real home directory:` + 路径 → 测试写了真实 HOME / 状态目录 / 全局配置 → 改用 `t.TempDir()` 与 `t.Setenv()`，环境变量的清理走 helper（`dshctl-testing`）。
4. `unexpected skips:` + 名单 → 出现白名单之外的 `--- SKIP`（子测试的 skip 也会被抓到）→ 要么去掉 skip（缺工具就让测试 Fatal），要么在 `../../../../.github/workflows/ci.yml` 的白名单里加上并说明为什么这个 skip 合理。
5. `FAIL internal/nodejs: xx.x% (要求 100%，a/b 条语句)` + `未覆盖:` 行 → 该包有语句没被执行 → 给新分支补测试；这是硬门禁，不接受报告了事。
6. `<profile> 里没有本模块的覆盖率数据` → profile 不是本模块的（或没带 `-coverprofile`）→ 用 `make coverage` 生成的那份。
7. `ALIVE   <name> — the suite did not notice` → 变异没被任何测试发现，说明这条决策没被钉住 → 加一个会因它失败的测试。
8. `INVALID <name> — the mutation did not compile, so it proves nothing` → 变异锚点在源码里不再唯一匹配或改坏了编译 → 更新 `scripts/mutation-check.py` 里的原文字面量。
9. `BLOCKED <name> — the toolchain could not use its build cache; set GOCACHE` → 运行环境用不了 Go 构建缓存（沙箱/权限）→ 换一个可写的 `GOCACHE` 再跑。
10. `mutation(s) survived or were invalid: the suite does not pin them` → 上面两类任一条出现后的汇总 → 逐条处理，不要只看总数。
11. `workflow check failed: …` → workflow 的结构属性被破坏（少了一个平台、构建丢了 `needs`、表达式里的裸词没加引号）→ 按提示改 `../../../../.github/workflows/` 下的文件。
12. `检查失败: go-comments: <file>:<line>: 注释 N 列，超过 88` → 注释超宽 → 折行（`dshctl-style`）。
13. `检查失败: go-imports: <file>: 引入第三方模块 …` → 破坏了零第三方依赖 → 用标准库实现。
14. `检查失败: go-imports: <file>: <包> 依赖 <包>，不在允许的层方向内` → 依赖方向反了或新增了跨层边 → 把逻辑放回上层，或经 `internal/service` 中转（`dshctl-decisions`）。
15. `检查失败: skills: … 相对链接指向不存在的 …` → skill 里的相对链接指向了被删/改名的文件 → 修链接或补文件（相对路径以 SKILL.md 所在目录为基准，仓库根是 `../../../`）。
16. `检查失败: trailing-newline: <file>` → 文件结尾不是恰好一个换行 → 补或删末尾换行。
17. `错误: …` 且退出码 2 → 命令行用法或配置错误（本地手跑命令时常见），不是门禁失败；看 `dshctl -v <命令>` 的来源输出。

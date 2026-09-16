---
name: dshctl-verify
description: 决定 dshctl 一次改动在提交前必须跑哪些门禁、按什么顺序、失败了怎么定位，以及提交/PR/发布的机械约束与报告纪律。用于改完代码准备提交或开 PR、CI 变红、或判断某个 make 目标是否适用于本次改动时。
---

# dshctl 门禁、提交与发布

## 概要

`make` 是门禁的唯一入口（`../../../Makefile`）。迭代中按改动选检，提交前跑全量：本机实测 `make check` 约 2 分钟（`real 1m56s`，含 `vet` 与约定检查；包并行运行，最慢的 `internal/cli` 与 `internal/service`（各约 110s）决定下限），而单机没有 CI 的三平台矩阵可依赖，全量是本地唯一能替代它的证据——这一点与上游 DSH 仓库"从不默认跑全量"的做法相反，因为那边有别的成本结构。完整门禁表、耗时基线与失败定位见 `references/gates.md`；用行为探针回归 skill 与 AGENTS.md 的改动见 `references/effectiveness-probes.md`。

## 规则

### 1. 按改动选检，提交前跑全量

- 迭代中：`make fmt-check vet` 加受影响包的 `go test ./internal/<pkg>/ -count=1`；提交前 `make check`（fmt-check + conventions + vet + test）必须绿，最终确认再跑 `make ci`。
- 改到哪就追加哪个门禁：改 `internal/nodejs` 或配置层决策加 `make mutation`、`make coverage`；改 `.github/` 加 `make workflow-check`；碰平台文件（`_unix`/`_windows`/`_darwin`/`_linux`/`_other`）加 `make cross` 与本机对应平台测试；改测试隔离或新增 skip 加 `make hermetic`。
- 为什么不能只跑受影响包就提交：跨包契约由测试钉住（如 `internal/service/contracts_test.go` 的三包路径契约），单包绿不代表契约没被别处踩坏。

### 2. 让测试真的重新执行

- 单包/单测试一律加 `-count=1`：Go 会缓存通过的结果，改完实现再跑同一条命令可能直接返回缓存结论，等于没跑。
- 不调大 `TEST_TIMEOUT`（`Makefile` 默认 600s，CI 的两个测试步骤也用同一个值）：`Makefile` 的注释写明它是为了不让"卡住的等待循环"把一次红灯拖成十分钟级的等待，调大只是把挂起藏起来。真要放宽，先说明是哪条测试、为什么它需要更久。
- 不因为一次失败就怀疑门禁本身：先看 `references/gates.md` 的"常见失败信息 → 原因 → 下一步"。

### 3. 三个属性门禁各自断言什么

- `make hermetic`（`../../../scripts/hermetic-check.sh`）：在一次性 HOME 里跑整套测试，并要求测试不在自己的临时目录之外留下任何东西。它红说明某个测试写了真实 HOME、真实状态目录或全局配置——这正是"测试不碰环境"从声明变成被检查属性的地方。
- `make coverage`（`../../../scripts/check-coverage.py`）：`internal/nodejs` 是 100% 硬门禁（这个包决定长跑服务用哪个 Node 运行时），`internal/config`、`internal/service` 只报告不设阈值。给 nodejs 加分支必须同时加测试，否则 CI 直接红。
- `make mutation`（`../../../scripts/mutation-check.py`）：逐条破坏 Node 与配置决策，要求测试失败。输出 `ALIVE`（没被发现）、`INVALID`（变异没编译，什么也没证明）、`BLOCKED`（工具链用不了构建缓存）都算失败。改这些决策必须跑，因为"测试通过"本身不能证明测试会注意到破坏。

### 4. CI 上额外跑什么

- `test` job 在三平台跑 `go vet` 与 `go test -race -count=1 -timeout 600s ./...`；无 race 的复跑与格式化检查只在 ubuntu：race 运行时更慢、调度不同，只在其中一种下通过的测试是值得知道的缺陷。
- `hermetic` job 把多个属性压在一次套件执行上：workflow 结构与书写约定检查、在一次性 HOME 里带覆盖率跑整套、拒绝白名单之外的 `--- SKIP`、以及 `internal/nodejs` 的覆盖率门禁。新增 skip 必须同步 `../../../.github/workflows/ci.yml` 的白名单并说明理由。
- `build` job 有 `needs: [test, hermetic]`：测试不过就不会产出 6 个平台的二进制；`vulncheck` 固定 govulncheck 版本，避免扫描器更新让一个没变的提交变红。

### 5. 提交、PR 与发布

- 提交信息 `<type>: <小写英文句子描述行为变化>`，type 用 feat/fix/test/docs/ci；PR 面向 main，三平台 + hermetic + 6 个构建目标 + govulncheck 必须全绿。
- 发布只通过打 `v*` tag：release 先在三个平台验证再发布 6 个产物；版本、提交、构建时间由 ldflags 注入 `internal/version`，代码里不写版本号。

### 6. 报告纪律

- 只报告真正跑过的命令与它们的输出；没跑的写 pending，不写"应该没问题"。
- 不把 CI 当第一次执行，"推上去赌 CI"等于让复核替你发现失败。
- 失败先定位到包、测试与行号，再谈归因；报告里给出复现命令。

## 验证

- 提交前：`make check`；一次到位或改动了门禁本身：`make ci`。
- 改过 skill 或 AGENTS.md 后：按 `references/effectiveness-probes.md` 的 10 个探针回归。

## 相关文件

- `../../../Makefile`：门禁目标的唯一真源，每个目标都有一行 `## ` 说明。
- `../../../scripts/hermetic-check.sh`、`../../../scripts/check-coverage.py`、`../../../scripts/mutation-check.py`、`../../../scripts/check-workflow.py`、`../../../scripts/check-conventions.py`。
- `../../../.github/workflows/ci.yml`、`../../../.github/workflows/release.yml`。
- `references/gates.md`、`references/effectiveness-probes.md`。

---
name: dshctl-verify
description: 决定 dshctl 一次改动该跑哪些门禁（本地只跑快检与定点复现，全量门禁一律交给 CI）、CI 变红怎么定位，以及提交/PR/发布的机械约束与报告纪律。用于改完代码准备提交或开 PR、CI 变红、判断某个 make 目标是否适用于本次改动、或打算在本机跑 `make check`/`make ci`/`make mutation` 等全量门禁时。
---

# dshctl 门禁、提交与发布

## 概要

`make` 是门禁的命令清单（`../../../Makefile`），不是 CI 的调用入口：CI 的每个 job 直接跑同样的命令，`scripts/check-workflow.py` 的 `check_makefile_parity` 强制两者一致。分工是硬规则：本地只跑快检与定点复现，**全量门禁只在 CI 跑，本地不执行 `make ci`**。快检是 `make fmt-check conventions vet`（秒级静态检查，改 `.github/` 时加 `make workflow-check`）加受影响包的 `go test ./internal/<pkg>/ -count=1`；全量（三平台 race 套件、覆盖率与 hermetic、变异分片、六个交叉目标、下限工具链）由 GitHub Actions 跑。完整门禁表、耗时基线（CI 预算用）与失败定位见 `references/gates.md`；用行为探针回归 skill 与 AGENTS.md 的改动见 `references/effectiveness-probes.md`。

## 规则

### 1. 按改动选检，全量交给 CI

- 本地（快检）：`make fmt-check conventions vet`（改 `.github/` 加 `make workflow-check`），加受影响包的 `go test ./internal/<pkg>/ -count=1`；需要证明某条守卫会红、某个变异会被抓住时，只跑那一条（`-run NAME`、`python3 scripts/mutation-check.py --only NAME`）。
- 本地不跑全量：`make check`、`make ci`、全量 `make test`、`make test-race`、`make mutation`、`make coverage`、`make hermetic`、`make cross` 覆盖的那些命令一律由 CI 执行（`../../../.github/workflows/ci.yml` 里逐条对应、由 `check-workflow.py` 校验一致），本地跑它们只是把 CI 的时间花两遍。
- 改到哪类代码，就在 CI 上看哪个门禁的结论：改 `internal/nodejs` 或配置层决策看 `mutation` 与 `coverage`；改 `.github/` 本地跑 `make workflow-check`（静态检查，秒级），CI 的 hermetic job 也会再跑一遍；碰平台文件（`_unix`/`_windows`/`_darwin`/`_linux`/`_other`）看 `cross` 与三平台 `test`；改测试隔离或新增 skip 看 `hermetic`。CI 里没有对应 job 时，先补 workflow 再推（`dshctl-verify` 的"改门禁本身"）。
- 为什么受影响的包绿了也要等 CI：跨包契约由测试钉住（如 `internal/service/contracts_test.go` 的三包路径契约），单包绿不代表契约没被别处踩坏；而且平台差异只有三平台矩阵能看见。

### 2. 让测试真的重新执行

- 单包/单测试一律加 `-count=1`：Go 会缓存通过的结果，改完实现再跑同一条命令可能直接返回缓存结论，等于没跑。
- 不调大 `TEST_TIMEOUT`（`Makefile` 默认 600s，CI 的两个测试步骤也用同一个值）：`Makefile` 的注释写明它是为了不让"卡住的等待循环"把一次红灯拖成十分钟级的等待，调大只是把挂起藏起来。真要放宽，先说明是哪条测试、为什么它需要更久。
- 不因为一次失败就怀疑门禁本身：先看 `references/gates.md` 的"常见失败信息 → 原因 → 下一步"。

### 3. 三个属性门禁各自断言什么

`make hermetic`、`make coverage`、`make mutation` 分别把"测试不碰真实环境"、"`internal/nodejs` 100% 语句覆盖"与"决策被钉住"变成被检查的属性，覆盖面、失败输出与下一步都见 `references/gates.md` 的门禁表与失败定位。选法不变：改到哪类决策，就在 CI 看对应门禁的结论；`mutation` 覆盖的决策必须让它跑（整套在 CI，本地只用 `--only` 证明单条），因为"测试通过"本身不能证明测试会注意到破坏。

### 4. CI 上额外跑什么

- `test` 只在三平台跑 `-race`，ubuntu 另有无 race 复跑与格式化（race 运行时更慢、调度不同，只在一种下通过的测试是值得知道的缺陷）；`hermetic` 一次执行压上 workflow 结构检查、一次性 HOME、skip 白名单与覆盖率门禁；`mutation` 分 6 个分片并行且刻意不被 `build` 依赖；`floor` 用下限工具链验证 README 承诺的 `1.24+`；`vulncheck` 固定 govulncheck 版本。分片矩阵无洞、`--shard` 的 N、`needs`、版本锁定这些结构属性由 `check-workflow.py` 强制，细节见 `references/gates.md`。
- 新增 skip 必须同步 ci.yml 白名单并说明理由（判据见 `dshctl-testing` 规则 9）；工具链、action 与 govulncheck 的升级是独立的 `ci:` 提交，提交信息说明为什么现在升（见 `AGENTS.md` 的「命令」）。

### 5. 提交、PR 与发布

- 提交信息 `<type>: <小写英文句子描述行为变化>`，type 用 feat/fix/test/docs/ci；任意分支的 push 都触发同一套完整 CI（没有"分支快速版"），分支开发也以 Actions 的结论为准；main 受分支保护：除 `goldens`（只在手动触发时录制金标）外的全部检查绿了才允许合并 PR。
- 发布只通过打 `v*` tag：release 先在三个平台验证再发布 6 个产物；版本、提交、构建时间由 ldflags 注入 `internal/version`，代码里不写版本号。

### 6. 报告纪律

- 只报告真正跑过的命令与它们的输出；没跑的写 pending，不写"应该没问题"。
- 区分"本地快检跑过"与"CI 判定过"：推之前必须有快检结果，推之后必须等 CI 出结论再宣布完成，不能替 CI 下结论，也不能用本地全量替它复现。
- 失败先定位到包、测试与行号，再谈归因；报告里给出复现命令。

## 验证

- 本地：`make fmt-check conventions vet`（改 `.github/` 加 `make workflow-check`）与受影响包的 `go test ./internal/<pkg>/ -count=1`。
- 全量（`make check`、`make ci` 及同级的 race/覆盖率/hermetic/变异/交叉编译）看 CI：推送后读 GitHub Actions 的结论，本地不跑。
- 改过 skill 或 AGENTS.md 后：按 `references/effectiveness-probes.md` 的探针回归（新增了约束就同时加一条探针）。

## 相关文件

- `../../../Makefile`：门禁目标的唯一真源，每个目标都有一行 `## ` 说明。
- `../../../scripts/hermetic-check.sh`、`../../../scripts/check-coverage.py`、`../../../scripts/mutation-check.py`、`../../../scripts/check-workflow.py`、`../../../scripts/check-conventions.py`。
- `../../../.github/workflows/ci.yml`、`../../../.github/workflows/release.yml`。
- `references/gates.md`、`references/effectiveness-probes.md`。

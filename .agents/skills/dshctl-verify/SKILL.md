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

- `make hermetic`（`../../../scripts/hermetic-check.sh`）：在一次性 HOME 里跑整套测试，并要求测试不在自己的临时目录之外留下任何东西。它红说明某个测试写了真实 HOME、真实状态目录或全局配置——这正是"测试不碰环境"从声明变成被检查属性的地方。
- `make coverage`（`../../../scripts/check-coverage.py`）：`internal/nodejs` 是 100% 硬门禁（这个包决定长跑服务用哪个 Node 运行时），`internal/config`、`internal/service` 只报告不设阈值。给 nodejs 加分支必须同时加测试，否则 CI 直接红。
- `make mutation`（`../../../scripts/mutation-check.py`）：逐条破坏 Node 与配置决策，要求测试失败。输出 `ALIVE`（没被发现）、`INVALID`（变异没编译，什么也没证明）、`BLOCKED`（工具链用不了构建缓存）都算失败。改这些决策必须让它跑（整套在 CI，本地只用 `--only` 证明单条），因为"测试通过"本身不能证明测试会注意到破坏。

### 4. CI 上额外跑什么

- `test` job 在三平台跑 `go vet` 与 `go test -race -count=1 -timeout 600s ./...`；无 race 的复跑与格式化检查只在 ubuntu：race 运行时更慢、调度不同，只在其中一种下通过的测试是值得知道的缺陷。
- `hermetic` job 把多个属性压在一次套件执行上：workflow 结构与书写约定检查、在一次性 HOME 里带覆盖率跑整套、拒绝白名单之外的 `--- SKIP`、以及 `internal/nodejs` 的覆盖率门禁。新增 skip 必须同步 `../../../.github/workflows/ci.yml` 的白名单并说明理由。
- `mutation` job 把整套 `scripts/mutation-check.py` 分 6 个分片并行跑（条数以 `--list` 为准，别抄数字），每条决策逐条破坏并要求套件变红。它刻意不被 `build` 依赖：结论只关乎测试强度。`scripts/check-workflow.py` 会检查这个 job 还在、还在跑脚本、有超时、没有 `needs`，并且分片矩阵是 1..N 无洞、`--shard ${{ matrix.shard }}/N` 的 N 与矩阵长度一致——分片把墙钟除以 6，而全部变异仍然各跑一次。没有它，"测试会注意到破坏吗"就没人回答。
- `floor` job 用 `go.mod` 声明的下限工具链（`GOTOOLCHAIN=go1.24.0`）跑 `go vet ./...`：README 承诺的 `1.24+` 只有这一处验证，`check-workflow.py` 把 `go.mod` 的 `go` 指令与这一行绑在一起。工具链、action 与 govulncheck 都按确切值锁定（该检查拒绝 `1.25.x`、`check-latest: true` 与按 tag 引用的 action）；升级它们是独立的 `ci:` 提交，提交信息说明为什么现在升。
- `build` job 有 `needs: [test, hermetic]`：测试不过就不会产出 6 个平台的二进制；`vulncheck` 固定 govulncheck 版本，避免扫描器更新让一个没变的提交变红。

### 5. 提交、PR 与发布

- 提交信息 `<type>: <小写英文句子描述行为变化>`，type 用 feat/fix/test/docs/ci；PR 面向 main，三平台 + hermetic + mutation + 6 个构建目标 + govulncheck 必须全绿。
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

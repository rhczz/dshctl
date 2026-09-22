# 决策: 重写有一个独立裁判（金标 + 账本 + 决策名册）

状态: 已实施

## 问题

大爆炸重写要同时改实现与测试。如果新测试自己写、自己判，它会把"错误的实现"钉成契约：
一次不小心的行为变化会被新的断言忠实记录下来，而"功能不变"就只剩声明。
另一个方向是保留旧测试——但白盒测试写死在旧结构上，正是重写要拆掉的东西。

## 决定

1. **金标 conformance**（`internal/conformance`）：黑盒场景矩阵驱动真实二进制，
   与 v0.2.5 录下的金标逐字节比对（退出码、stdout、stderr、文件树含权限与内容哈希、
   git 引用）。金标只能 `-update` 重录，且 CI 会从参照 tag 重新生成并要求零 diff。
   归一化只覆盖时间戳、pid、临时路径、耗时；harness 自测"旧 vs 旧零差异"。
2. **账本**（`internal/conformance/accounting/accounting.json`）：树上每个测试一行，
   `disposition` 取 `todo/kept/new-test/conformance/differential/merged/obsolete`。
   `check-accounting.py --strict` 是合并闸门：0 个 `todo`，`obsolete` 逐条说明。
3. **决策名册**（`decisions.json`）：`scripts/mutation-check.py` 的 63 条决策以**名字**为
   身份；重写可以换锚点位置与文本，名字不许少，且每条都要被抓住。
4. **加法与豁免清单**（`additions.json`/`exemptions.json`）：差分之外的改动逐条登记
   （消费者 + 测试 + README 记账），未登记的差异一律红。
5. **发布纪律**：重写分支只发 `v0.3.0-rc.N` 预发布（`release.yml` 需要"`-rc` 必须
   `--prerelease`"的规则），切回 main 后发 `v0.3.0`；main 冻结期间只收 bug 修复并双向 cherry-pick。

## 备选方案

- **重写测试、相信新测试**：新测试会为错误的实现背书，且旧测试里积累的历史 bug 知识
  无从迁移。
- **保留旧测试、不改它们**：白盒测试与旧结构绑定，改结构就要改测试，"保留"是幻觉。
- **只靠人工评审**：63 条决策与 762 个测试的行为覆盖不是评审能记住的量。

## 后果

- 重写期间 mutation job 可能红（锚点随代码移动需要同步），但**合并闸门是严格的**：
  金标 + 账本 + 名册三者同时成立才允许合并。
- `internal/conformance` 是测试专用包（`check-orphans.py` 的 SUPPORT 里写明理由），
  它随版本长期保留：参照 tag 可换，差分能力不丢。
- 代价是登记成本：每删一个测试、每改一处表面文案都要写一行账。这是"强度只增"的价钱。

## 验证

- `python3 scripts/check-accounting.py`（默认模式）每次 `make conventions` 都跑；
  `--strict` 是合并前的闸门。
- `go test ./internal/conformance/ -count=1`：金标比对；`-update` 只能重录。
- `python3 scripts/mutation-check.py --only <名字>`：单条决策仍会被抓住。

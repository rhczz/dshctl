# 决策: 全量门禁在 CI 上跑，本地只留快检

状态: 已实施

## 问题

原来的规则是「迭代中跑受影响包，提交前跑全量」：`make check`（约 2 分钟）与
`make ci`（约 4 分钟）在本地跑，`make mutation`（36 条变异，约 40 分钟）也在本地跑。
实际后果有三个：

1. 同一套门禁在一台机器上跑第二遍，而 GitHub Actions 的三平台矩阵本来就跑同一批
   命令，本机那份既不能替代矩阵（没有 Windows/macOS），也不比它多证明什么。
2. 本地跑全量把评审与修 bug 的时间挤到门禁后面：一次改动要等 4 分钟才知道格式或
   跨包契约有没有坏，而这些问题本来推上去两分钟内就有结论。
3. 更要紧的是**变异门禁根本没进 CI**。`ci.yml` 只有 `test`、`hermetic`、`build`、
   `vulncheck` 四个 job，`scripts/mutation-check.py` 从没在流水线里跑过。于是
   「全量交给 CI」如果照字面执行，唯一能回答「测试会注意到破坏吗」的门禁就消失了
   ——它是本次改动里最慢也最容易掉队的一环，而它恰恰是防止「测试存在漏洞」的那一环。

## 决定

分工按**成本与性质**切开，而不是按「谁有空」：

- 本地：`make fmt-check`、`make conventions`、`make vet`（改 `.github/` 时加
  `make workflow-check`）——都是秒级静态检查；加上受影响包的 `go test -count=1`；
  以及为证明「这条守卫会红」「这条变异会被抓住」而跑的**单条** `-run`/`--only`。
- CI：`make check`、`make ci`、全量 `make test`、`test-race`、`mutation`、`coverage`、
  `hermetic`、`cross`。本地不再执行它们，结论以 Actions 为准。
- `make mutation` 作为新的 `mutation` job 进入 `ci.yml`：ubuntu、`timeout-minutes: 60`、
  不写 `needs`（最慢的 job 不放在关键路径上，`build` 也不依赖它——它的结论关乎测试
  强度，不关乎二进制）。`scripts/check-workflow.py` 新增 `check_mutation_gate`：job
  存在、命令里真的跑 `mutation-check.py`（注释不算）、有超时、没有 `needs`。

## 备选方案

- **保持原样（全量本地跑）**：被否。它就是问题本身，且与「不要本地跑 CI」的约束冲突。
- **变异门禁只在发布时跑**（挂到 `release.yml` 的 verify 上）：被否。改动落地到 main
  与发布之间可能隔着很多提交，那时才发现某条决策不再被钉住，要回溯是哪次改动破坏的。
- **按路径触发变异 job**（只有改 `internal/nodejs`/配置层时才跑）：被否。Actions 的
  `paths` 过滤只能作用在整个 workflow 上，per-job 判断要靠 `contains(github.event...)`
  之类的表达式，脆且难验证；36 条变异里有一半涉及 `internal/cli`/`internal/service`，
  「哪些改动算配置决策」本身就是判断，写死在 YAML 里等于把判断藏进表达式。
- **给变异 job 加 `needs: [test]`**：被否。省不了多少算力（job 之间本来就并行），却把
  最慢的一环放进关键路径，还会让 build 的产出等它。
- **本地保留 `make mutation` 作为例外**：被否。它是唯一一个 40 分钟量级的门禁，留在本地
  等于保留「等门禁」这一项成本，而 CI 上它与其他 job 并行，不占用任何人的时间。

## 后果

- 推一次改动的完整结论要等 `mutation` job（预计十几到几十分钟），但它不阻塞 `test`、
  `hermetic`、`build`：三平台测试与产物仍在一两分钟内出结果，变异结论随后补上。
- 以后新增门禁时，第一问变成「它在 CI 的哪个 job 里跑」；如果答案是「本地手跑」，那
  这条门禁就不存在——`check_mutation_gate` 是这条规则的第一份可执行形式。
- `scripts/mutation-check.py` 的条数（当前 36）成了文档里的数字，增删变异要同步
  `references/gates.md` 与 `pinning-tests.md`。
- 本地少了 4 分钟的 `make ci`，代价是「本机全绿」不再等于「CI 全绿」：报告必须区分
  「本地快检跑过」与「CI 判定过」，或者干脆等 CI 出结论再宣布完成。

## 验证

- `python3 scripts/check-workflow.py`：5 个 job（含 `mutation`），并把 `mutation` job 的
  四种回归打红过——job 改名（缺 job）、把 `run:` 换成 `echo`（命令里没有脚本）、加
  `needs`（串行）、删 `timeout-minutes`（无超时）；还原后重新通过。
- `python3 scripts/check-conventions.py`：7 条规则通过（本文件也是其中之一）。
- 本改动推送后由 CI 判定：三平台 `test`、`hermetic`、新增的 `mutation`、六个 `build`
  与 `vulncheck`；本地只跑了上面两条静态检查。

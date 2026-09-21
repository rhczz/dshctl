# 决策: 变异门禁分片、工具链锁定，以及 CI 与 Makefile 的一致

状态: 已实施

## 问题

一次完整结论要等 14 分 26 秒（run 35176900756）：`mutation` job 自己占了 14 分 22 秒，
其余 job 全在 3 分 20 秒内结束。逐条读日志，慢的不是编译：第 1 条变异 39.5s，之后
同为 `./internal/service/` 的变异稳定在 32.9s，说明冷构建只值约 7s，剩下的是测试本身
——`internal/service` 在 ubuntu 上跑 34s，其中 20.0s 是一个测试故意耗尽指纹预算的结果，
而这个包被 20 多条变异各跑一遍。分片是唯一能把它除以 N 而不丢任何一条变异的办法。

同期还有三个"同一提交可能有不同结论"的源头，以及两处文档与现实不符：

- 工具链写 `1.25.x` + `check-latest: true`（5 处），action 按可移动的 tag 引用，
  release 同样浮动：一周前绿的提交可以因为编译器补丁换版而变红，也可以因为变绿而
  掩盖一次真实回归。仓库为 govulncheck 固定版本的理由（`@latest` 是噪声）对这两类
  同样成立，却没有照做。
- README 承诺 `1.24+`，`go.mod` 写 `go 1.24`，而 CI 只装 1.25.x：用 1.25 的工具链
  调用 1.24 不存在的标准库函数，三平台都一样绿。
- 文档说 CI 跑 `make check`/`make ci`/`make cross`，实际 `ci.yml` 里没有任何 `make`：
  同一条门禁在 Makefile 与 workflow 里是两份独立实现，改了一边另一边不会红。

## 决定

- **变异套件按索引分片，六个 job 并行。** `scripts/mutation-check.py --shard i/6` 用
  `位置 % 6 == i-1` 切分：每条变异恰好属于一个分片，六个分片的并集是全部，新增一条
  变异不会把已有变异挪到别的分片。`ci.yml` 的 `mutation` job 变成 `shard: [1..6]`
  矩阵，`fail-fast: false`，仍然没有 `needs`、仍有 `timeout-minutes`。
  `scripts/check-workflow.py` 保证：矩阵是 1..N 无洞无重、`--shard` 的 N 与矩阵长度
  一致、分片序号来自 `matrix.shard` 而不是字面量（六份 `--shard 1/6` 会是六个绿 job
  只覆盖六分之一）。`--only` 与 `--shard` 互斥：一个回答"这条决策被钉住了吗"，一个
  回答"我负责哪一片"，混在一起会得到一个覆盖率取决于算术的命令。
- **指纹预算变成包内字段。** `Service.fingerprint`（未导出，零值取生产常量）与已有的
  `sleep`/`poll`/`grace` 同类：它是包自己的节奏，不是调用方的旋钮。fixture 把它压到
  20ms，于是"预算耗尽后降级并报告"这个行为仍然被完整执行，只是不再花 20 秒；生产值
  仍由 `TestFingerprintTimeoutIsGenerous` 钉住。三个用真实 host 的测试显式恢复生产
  预算——虚构机器立刻回答或永远不回答，真实进程该拿到产品给它的时间。
- **工具链、action、下限三件事都锁死。** `go-version` 写确切补丁 `1.25.14`（去掉
  `check-latest`），action 按 commit SHA 固定（行尾保留可读版本），新增 `floor` job 用
  `GOTOOLCHAIN=go1.24.0` 跑 `go vet ./...`。`check-workflow.py` 拒绝 `X.Y.Z` 之外的
  `go-version`、拒绝 `check-latest: true`、拒绝非 40 位十六进制的 `uses`，并把 `go.mod`
  的 `go` 指令与 `floor` job 的 `GOTOOLCHAIN` 绑在一起。升级这三类 pin 是独立的
  `ci:` 提交。
- **CI 不调用 `make`，但两者由检查绑定。** 每个 job 继续直接跑命令（Windows/macOS
  runner 上 `make` 的可用性不是本仓库想依赖的东西，步骤里也有平台专属逻辑），新增
  `check_makefile_parity`：Makefile 里 `vet`/`test`/`test-race` 的每条命令（`$(TEST_TIMEOUT)`
  按默认值展开）必须逐字出现在 `ci.yml` 里，`build` job 的六个 `goos/goarch` 目标也必须
  都在。`make check`/`make ci` 从此被文档写成"本地别名"而不是"CI 的入口"。
- **变异分类补齐"测试二进制被打死"这一类。** 新的 `internal/host` 变异（探测存在性
  不能结束进程）会让 `Alive` 去 kill 被探测的进程，而包内其它测试探测的是自己，
  于是整个测试二进制被信号带走：输出里只有 `signal: killed` 与包级 `FAIL`，没有
  `--- FAIL` 行，旧分类把它记成 `INVALID`（"没编译，什么也没证明"）——一条**被抓住**
  的变异被报成无人看守。现在 `run()` 在排除构建失败之后，把包级 `FAIL` 或
  `signal:` 也算作 `caught`：构建失败仍然先判，所以 `INVALID` 的含义没有变宽，
  变宽的只是"套件红了"的识别面。
- **测试套件同时补上三处会被静默绕过的契约**：README 的命令表、退出码表与默认值由
  `documentation_test.go` 强制（帮助文案里的退出码表被提成 `helpExitCodes` 常量，测试与
  README 比较同一份），`paths.Env*` 与环境变量、`internal/` 下的包与 AGENTS.md 包地图由
  `check-conventions.py` 的两条新规则强制。

## 备选方案

**缓存 GOCACHE。** 落选：实测冷热差只有约 7s/条（39.5s vs 32.9s），每条变异的成本是
测试运行时间而不是编译，缓存买不到墙钟，只会多一个会失效的活动部件。

**按改动路径过滤变异（只跑改到的文件对应的变异）。** 落选，理由与
`2026-09-17-gates-run-on-ci.md` 相同：判断"哪些改动算配置决策"本身就是判断，写进
YAML 等于把判断藏进表达式；而且一条改动的跨包影响可能让另一处的钉住失效。分片是
"全部照跑"与"少跑"之间的第三条路。

**让 CI 直接调用 `make`。** 落选：三个平台的 runner 是否都自带 `make`、以及平台专属
步骤怎么切进目标，都要押在新的假设上；静态一致性检查能用零运行时风险换到同样的
"两边不许漂移"。

**给变异 job 加 `needs` 或删掉 ubuntu 的 no-race 复跑。** 落选：前者把最慢的一环放回
关键路径；后者看着冗余，但不占墙钟（分片与 Windows job 都比它长），删掉只是拿掉一个
执行制度的信号。

**把 `internal/service` 的测试拆包或加 `t.Parallel()`。** 落选：这个包的测试会绑真实
端口、写真实临时目录，并行化把确定性换成速度，与本仓库的方向相反；分片已经拿到
想要的墙钟。

**缩短那 20 秒等待的另一种做法：给测试一个更短的 context。** 落选：那测的是"context
被取消"，不是"预算耗尽后降级"，是另一条分支。

## 后果

- 墙钟从 14m26s 降到分钟级：六个变异分片各自约 2–4 分钟，最长的 job 变成 Windows 的
  `test`（约 3m16s，`internal/service` 一个包 122s，是 runner 的文件系统与
  Defender 开销，不是某个测试在等）。总 runner 分钟数上升，但仓库是公开仓库、分钟数
  不计费，而等待是人（和 agent）在付。
- 变异的结论现在是六个 job 的合取：某一片红时，失败的变异名就在那一片的日志里；
  `fail-fast: false` 保证其余五片仍然给出结论。
- pin 意味着新的 Go 补丁与新的 action 版本不会自动进入流水线：代价是旧工具链可能积累
  公告，`vulncheck` 是这个代价的报警器，升级则是一次独立的 `ci:` 提交。
- `--only` 仍是本地证明单条变异的标准手段；`--shard` 不是本地门禁的替代品（本地跑它
  只回答"这一片还绿吗"）。
- 已知缺口：`internal/service` 在 Windows 上 122s 的原因只到"包级"这一层，没有逐测试
  证据；要定位需要一次带 `-v` 的 Windows 运行（本地无法执行 Windows 二进制）。

## 验证

- `python3 scripts/check-workflow.py`：新增分片、pin、floor、构建目标与 Makefile 一致性
  五组规则；逐条做回归探针并确认变红后再还原——分片列表有洞、`--shard` 分母与矩阵不符、
  分片序号写成字面量、`go-version` 浮动、加回 `check-latest`、action 回到 `@v5`、
  `floor` 的 `GOTOOLCHAIN` 写错、删掉 `windows/arm64` 构建目标、Makefile 改掉一条门禁
  而 ci.yml 不动：九次全部以预期的信息变红，还原后重新通过。
- `python3 scripts/check-conventions.py`：9 条规则（新增 `readme-env` 与 `package-map`，
  两条都做过变红探针：README 去掉一个环境变量、`internal/` 下加一个未登记的包）。
- `python3 scripts/mutation-check.py --only "a fingerprint that could not be read"`：
  `caught`（`TestStartWithoutAFingerprintStillWorksAndSaysSo` 与
  `TestProcessStartTimeGivesUpCleanly` 两条同时失败），证明缩短预算没有把这条决策
  变成无人看守。
- `go test ./internal/service/ -run TestStartWithoutAFingerprint -count=1`：20.20s → 0.23s。
- 本改动推送后由 CI 判定：六个变异分片、`floor`、三平台 `test`、`hermetic`、六个
  `build` 与 `vulncheck`。

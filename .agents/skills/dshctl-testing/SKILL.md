---
name: dshctl-testing
description: 在 dshctl 写或改测试：hermetic 隔离、真实入口路径、优先真实实现、资源归属与 teardown、并发下的 flake 判据、覆盖率的确切含义、命名与失败信息。用于新增测试、修 flake、补覆盖率，或判断断言该落在哪一层时。
---

# 测试怎么写

## 概要

顺序与断言强度见 `dshctl-tdd`；这个 skill 管**形态与隔离**：测试放在哪一层、能不能碰真实环境、什么时候可以 fake、失败信息怎么让人看懂。这里的每条规则都由仓库里已有的失败模式倒推出来，不是通用偏好。

## 规则

1. **测试必须 hermetic：只在自己的临时目录里写。** 用 `t.TempDir()` 和 `t.Setenv()`，绝不写真实的 `$HOME`、真实状态目录或当前工作目录。需要 HOME 时用 `t.Setenv("HOME", t.TempDir())`（Windows 上是 `USERPROFILE`，用 `t.Setenv` 两个都设最省事）。`make hermetic` 在清空的 HOME 下跑整套并断言没有多余文件；它同时检查环境指向的路径连被创建都不允许——检查把"路径存在"即判 stray（空文件、空目录也算）。

2. **helper 进程的子环境要显式拼装。** 测试需要真实子进程时（`internal/host/helperenv_test.go` 是范例），标记环境变量必须**命名父进程 pid**，这样并发的测试不会互相认领对方的子进程；同时必须先把继承来的同名键剥掉，否则父进程自己的标记会让子进程误判身份。真实实现优先于"用 `sh -c` 拼一段假进程"。

3. **只 fake 两个缝，其余用真实实现。** 可 fake 的只有 `service.OsHost`（让整个生命周期跑在虚构机器上）与 `run.Executor`/`Capturer`/`Outputer`（让外部命令可控）。除此之外优先真实实现：真实的 `atomically`、真实的 `lock`、真实的 `logfile`、真实的 `config.Load`。用真实实现买到的是"它和文件系统真的对得上"，用 fake 买到的是"决策逻辑对"——两者都需要，但不要用 fake 去替代后者。只测 mock 的调用序列等于验证自己写的剧本。

4. **CLI 级承诺要跑真实二进制。** `internal/cli/readonly_test.go` 构建真实二进制、以子进程运行、检查它没有写盘；`documentation_test.go` 读真实 README。理由是"只读"这种承诺在单元层无法证明——只有跑起来才知道它碰了什么。关键工作流还要跑一次真实闭环（`internal/cli/release_test.go` 的 `update → rollback → timeline`），因为单元测试各自通过、接起来不成立是这类功能的典型失败。

5. **验证世界，而不是被测代码的自述。** 断言要落在外部可观测量上：文件是否存在与内容、权限位、锁文件是否还在、端口是否真的被占用、进程是否真的消失、日志里真的出现了那行。不要断言"函数返回了它自己刚写进去的东西"。

6. **资源归属清晰，teardown 一定归还。** 测试里开的端口、起的进程、建的临时目录在 `t.Cleanup` 里归还；测试失败路径也要走到清理，否则一次失败会让后续测试连带失败。真实的 dshctl 在停机时也遵循同一条规则（见 `dshctl-defensive`），测试是它的第一现场。

7. **等待用轮询到条件，不用 `time.Sleep`。** 固定 sleep 在慢机器上是 flake 的来源，在快机器上是浪费时间。断言"某个条件最终成立"时写一个有上界的轮询。

8. **只有单独跑才通过 = spec 的缺陷。** 修 spec（补隔离、修资源竞争、去掉对顺序的隐含依赖），不要重跑、不要加 `-p 1` 掩盖、更不要 skip。`make test` 与 `make test -race` 各跑一次就是为了让这类问题暴露。

9. **缺工具时 Skip 还是 Fatal，按测试的性质决定。** 可选真实工具（`lsof`/`ss`/`netstat`/`ps`）不在时允许 skip——这类测试是在问真实工具一个问题，工具缺席时它没有可断言的东西；CI 里允许的 skip 是一个**明确的白名单**（`.github/workflows/ci.yml` 的 "Fail on unexpected skips" 步骤，共 8 个测试名，含子测试），新增 skip 必须同时加进白名单，否则 CI 会红。反过来，覆盖 dshctl 最具破坏性的那一半（清理残留 checkout 目录）的测试在缺 git 时用 `t.Fatalf`：静默跳过会让这段逻辑在绿灯下无人验证。

10. **命名与失败信息要让人一眼看懂。** 测试名是句子（`TestLoadRejectsWrongJSONTypes`、`FuzzParseVersionNeverInventsARelease`），子测试名是被测输入或场景。失败信息用英文（与标识符、注释一致；中文留给面向操作者的产品文案），第一句说清"期望什么、实际什么"，带上关键输入；断言里用 `t.Fatalf("…: got %q, want %q", got, want)` 而不是只打印 `got`。

11. **`t.Helper()` 与表驱动是默认做法。** 两者在树里都是默认形态（数量随代码变，要引用就现场 `grep -rc`，别抄数字）；辅助函数不加 `t.Helper()` 会让失败行号指向辅助函数而不是调用点。

12. **覆盖率是必要条件，不是充分条件。** `make coverage` 里 `internal/nodejs` 要求 100%（它决定用哪个运行时跑服务），`config`/`service` 只报告。没被覆盖的行往往是死代码或缺少用例，两种情况都值得看一眼；但覆盖率不能替代第 5 条——100% 覆盖的测试仍然可以什么都没断言。

13. **写盘文件的读取器与写入器共用同一套校验。** `Load` 拒绝的形状 `Save` 也必须拒绝，反之亦然：否则一个调用方就能造出自己读不回的文件。`internal/history` 的 `validate` 是范例，两侧调用同一个函数，测试成对出现（`TestLoadRejectsCorruption` 的表 + `TestSaveRefusesADocumentItsReaderWouldReject`）。字段的"存在"与"取值范围"分开校验——`at` 只查非空是不够的。

14. **属性测试用 fuzz，找到的反例是回归。** 解析、裁剪、栈这类有代数性质的逻辑写 `Fuzz*` 目标：`go test` 跑种子语料，本地用 `-fuzztime` 探一段（`internal/history/history_fuzz_test.go`）。fuzz 报出来的输入要留在 `testdata/fuzz/` 并修实现，不许放宽断言或跳过；属性要对不可表达的输入显式设界（JSON 不能携带任意字节，非 UTF-8 会被替换成 U+FFFD）。

15. **"无法探测"的每条路径都要有一个让探测失败的用例。** 与 `dshctl-defensive` 的"探测不了不当结论"配对：`git status` 失败时 update 必须拒绝、`git fetch` 失败时 timeline 必须标注远程未确认、端口探测失败时 stop 必须拒绝——每条都要有一个注入失败的测试，证明它没有被折进成功分支。

## 验证

```sh
go test ./internal/<包>/ -count=1    # 本地：受影响包
make hermetic          # CI：清空 HOME 下跑整套 + 断言没有越界写盘
make coverage          # CI：internal/nodejs 必须 100%
make test-race         # CI：并发问题只有 race 检测器看得见
```

全量门禁只在 CI 跑（`AGENTS.md` 的「命令」一节）：本地写测试时跑受影响包，剩下三项等 CI 的结论；本地跑一遍 hermetic/race 不会比 CI 多证明什么，只是把 CI 的时间花两遍。

## 相关文件

- [测试文件的骨架、范例索引与 hermetic 清单](references/test-anatomy.md)
- [`internal/service/fake_test.go`](../../../internal/service/fake_test.go)：虚构机器与异常路径形状
- [`internal/host/helperenv_test.go`](../../../internal/host/helperenv_test.go)：helper 子进程的环境拼装
- [`internal/cli/readonly_test.go`](../../../internal/cli/readonly_test.go)：真实二进制的只读断言
- [`internal/repo/prune_audit_test.go`](../../../internal/repo/prune_audit_test.go)：`mustCheckout` 为什么在缺 git 时 Fatal
- [`scripts/hermetic-check.sh`](../../../scripts/hermetic-check.sh)：越界写盘的判定方式

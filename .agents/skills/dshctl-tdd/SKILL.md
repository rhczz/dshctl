---
name: dshctl-tdd
description: 在 dshctl 按测试先行推进：先写会失败的测试，守卫先证明会红，bug 先复现。用于开始实现新行为、修 bug、调整既有判断逻辑，或决定该先写表驱动/属性/fuzz/契约测试中的哪一种时。
---

# 测试先行

## 概要

这个 skill 管**顺序与断言强度**：什么时候写测试、怎么写才能证明它真的在守着东西。测试的形态与隔离（hermetic、helper 进程、fake 的边界）见 `dshctl-testing`；两者同时命中时先按本 skill 定顺序，再按那个定写法。

本仓库的测试先行不是教条，而是它已有的一条工程事实：每个决策点都应该有一个会因为该决策改变而失败的测试，`scripts/mutation-check.py` 就是这条规则的可执行形式。

## 规则

1. **先写会失败的测试，再写实现。** 顺序不是仪式：先写测试会强迫你在动手前说清楚"什么算做完了"，而先写实现再补测试，测试会不自觉地围绕已有代码的形状写，于是它证明的是"代码现在这么做"，而不是"需求是这个"。

2. **守卫只有在回归能让它变红时才是守卫。** 新增一条断言、一个检查、一个不变量测试之后，必须证明它会失败：引入那个回归（把判定条件反向、把下限改成闭区间、把校验删掉）→ 跑一次看它红 → 还原。看不了红的测试只是装饰。

3. **bug 先写复现测试。** 先写一个在当前代码上失败的测试，再修。要证明它确实在修复前是红的——临时把实现改动 stash 掉跑一次，或用 `git stash push <实现文件>` 再看红。修完这条测试永久留下，它就是这类 bug 不再复发的守卫。

4. **跨进程与多态行为先在虚构机器上写失败测试。** `start`/`stop`/端口归属/信号这类行为不要一上手就起真实进程：`internal/kernel/fake_test.go` 的虚构进程表（含 `survivesGraceful`、`survivesForce` 这类只在异常路径才用到的形状）、`internal/kernel/host.go` 的 `OsHost`、`internal/run` 的 `Executor` 就是为此存在的。虚构机器先证明决策逻辑；真实进程测试（`internal/host/realps_test.go`、`internal/repo/prune_test.go`）再证明它与真实世界对得上。

5. **断言强度要够。** `err == nil` 只是最低限度；断言要说清期望的形状与内容——表驱动用例配 `wantSubstr` 而不是"没报错"，结果结构逐字段比对而不是只看一个字段。一个改坏了实现仍然能通过的断言，等于没有断言。

6. **改断言的唯一合法理由是行为契约变了**，而且要在提交说明里写清"旧行为是什么、为什么不再成立"。为了让测试过而放宽 `wantSubstr`、删用例、把精确断言换成模糊断言，都属于伪造证据。

7. **一个决策点一个 pinning 测试，新不变量配一个反向用例。** 判定条件、排序、上限、拒绝路径，每一处都该有自己的名字。反向用例指"输入不合法时必须被拒绝"那一条——只有正向通过率的测试锁不住边界。

8. **`internal/nodejs` 的新分支必须带测试。** 该包是 100% 语句覆盖的硬门禁（`make coverage`），不是因为覆盖率高好看，而是因为它决定用哪个运行时跑服务。

9. **测试描述行为，不描述"正确性"。** 一个断言过时了，说明行为变了；这时把测试和实现一起改，并在提交里说明为什么。反过来，实现没变而老测试突然红了，先怀疑环境、flake 与测试之间的顺序依赖，再查配置或依赖的改动——不是去怀疑没改过的实现；新测试对旧实现红则是 TDD 的期望态。

10. **快反馈：本地只跑单包或单测试，全量等 CI。**

    ```sh
    go test ./internal/nodejs/ -run TestAssess -count=1 -v
    ```

    `-count=1` 关掉结果缓存；不加它时"刚改完还是绿的"可能只是缓存。全量本机约 2 分钟（耗时基线见 `dshctl-verify`）。

## 验证

```sh
go test ./internal/<包>/ -run <Test名> -count=1 -v   # 先看红，再看绿
python3 scripts/mutation-check.py --only <名字>        # 要证明某条变异会被抓住时，只跑这一条（整套在 CI 上跑）
```

全量门禁（`make mutation`、`make coverage`、`make test` 等）由 CI 执行，本地不跑；本地只保留上面这种定点复现。`internal/nodejs` 必须 100% 覆盖，加了新分支就加测试，结论在 CI 的 `coverage` 上看。

## 相关文件

- [pinning 测试的形态与红-绿证明](references/pinning-tests.md)
- [`internal/kernel/fake_test.go`](../../../internal/kernel/fake_test.go)：虚构进程表与它的异常路径形状
- [`internal/kernel/contracts_test.go`](../../../internal/kernel/contracts_test.go)：跨包字面量的契约钉法
- [`internal/cli/documentation_test.go`](../../../internal/cli/documentation_test.go)：README 与设置的同步测试
- [`scripts/mutation-check.py`](../../../scripts/mutation-check.py)：把"测试真的会失败吗"变成可执行检查

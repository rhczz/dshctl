# 决策: 任意分支 push 触发完整 CI，main 以分支保护强制全绿

状态: 已实施

## 问题

此前 `ci.yml` 的 `push` 只触发 `main`，`pull_request` 也只对 `main`：一个没有被
提出 PR 的分支什么都不跑，注释里写明的理由是"把流水线集中在要留在历史里的提交上"。
实际后果有两个：

1. 分支上可以堆任何不被 CI 看过的提交，直到它被提出 PR 才受到检验；「分支上验证过」
   没有任何机器证据。
2. 「CI 全绿才能合并」只是纪律：仓库对 `main` 没有分支保护（`protected: false`），
   GitHub 不阻止合并不绿的 PR。

操作者要求：分支 push 与 main 同门槛；合并必须以 CI 全绿为机器强制的门。

## 决定

- `push` 触发扩为 `branches: [main, "**"]`：任何分支的 push 都跑同一套完整流水线，
  没有「分支快速版」。列表里保留 `main`，既有的「push 必须包含 main」守卫原样成立。
- `pull_request` 与 `workflow_dispatch` 触发不变；`release.yml`（只对 `v*` tag）不变。
- 仓库设置对 `main` 启用分支保护：除 `goldens` 外的全部检查列为 required status
  checks（`goldens` 只在 `workflow_dispatch` 运行，列为必需会让每个 PR 永远无法
  合并）；不要求评审、不限制直接 push——个人仓库，直接 push main 的日常保留。
- `cancel-in-progress` 从「只取消 PR 的被取代运行」改为「取消一切非 `main` 引用的
  被取代运行」（`github.ref != 'refs/heads/main'`）：分支连推多个提交时只验证最新
  一个；main 的每次落地仍保留完整记录。
- `scripts/check-workflow.py` 新增守卫：push 触发的分支列表必须含 `**`，否则失败——
  「所有分支都触发」从约定变成被检查的属性。

## 备选方案

- **汇总 gate job**（一个 job `needs` 全部，分支保护只 require 它一个）：更少维护，
  但把「绿」的定义藏进一层间接，且与 `check-workflow.py` 现有的平铺断言风格不一致。
- **Rulesets 代替经典分支保护**：会把直接 push 一起拦下，而直接 push main 是操作者
  的日常路径。
- **分支 push 只跑部分 job**（如去掉 mutation 分片）：违背「同一套门禁」——两个真相
  来源必然漂移，而 mutation 分片正是「测试会注意到破坏吗」的回答。
- **保持现状**（分支不触发、无保护）：被否，见「问题」。

## 后果

- PR 分支的 push 与 pull_request 事件会各跑一次完整流水线（GitHub 的常规代价）；
  concurrency 只在同一 ref 内取消被取代运行，跨事件不去重。
- 每个分支 push 都付 6 个 mutation 分片的成本，换取「分支即门槛」；操作者明确接受。
- 分支保护的 context 列表在仓库设置里、不在本仓库文件中：换仓库或增删 job 时要手动
  同步，`check-workflow.py` 管不到它。
- 推分支立即有 CI 结论可看，不必先开 PR。

## 验证

- `python3 scripts/check-workflow.py` 通过；把 `branches` 临时改回 `[main]` 重跑会红
  （新守卫被证明会红），还原后重新通过。
- `make fmt-check conventions vet workflow-check` 通过。
- 本改动推送后由 CI 判定（这次 push 本身就是新触发的第一次验证）；分支保护设置后用
  API 回读确认 required checks 与实际 job 名一致。

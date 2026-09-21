# 决策: 更新历史是每个 checkout 的位置栈

状态: 已实施

## 问题

`rollback` 必须知道"上一次在哪"。只记录"当前版本 + 上一个版本"不够：操作者要能
一次退多步（`-n K`）。而 git 自己的 reflog 不是 dshctl 的事实源——它会过期、会被
手工操作污染，也说不清哪些位置是 dshctl 部署过的。同时，"当前在哪"这个问题已经
有一个权威答案：`git rev-parse HEAD`。记录文件不该重复它。

## 决定

`<状态目录>/updates.json` 记录每个 checkout 的**位置栈**：

```json
{
  "repos": [
    {"repo": "/abs/checkout",
     "records": [{"commit": "<sha>", "selector": "latest", "at": 1758300000}]}
  ]
}
```

- 组内新→旧，最多 50 条（`MaxRecords`），只保留最近的；组之间互不影响。
- `Visit(records, position)`：目标已在栈中则截断到它（放弃比它新的位置），否则
  前插；结果去重、裁剪。一次移动写两项：先补起点（当前 HEAD，仅当栈顶不是它），
  再写目标——所以"无参数 rollback 回到上一次 update 之前所在的位置"在起点是手工
  切过去的时候也成立。
- `Step(records, current, n)`：当前 HEAD 是虚拟栈顶；HEAD 已在栈中时取它开始的
  后缀，否则前插。第 n 步即虚拟栈第 n 项。
- git 的 HEAD 是"当前在哪"的唯一真源；记录只回答"dshctl 部署过哪些位置"。
- 写入时机：HEAD 实际变化后立刻写（checkout/merge 成功那一刻），即使随后
  install/build 失败。写失败不中止部署：继续构建与恢复启动，最后以退出码 1
  报告"更新历史未写入"。
- 损坏（非普通文件、非法 JSON、非对象、空 repo/commit、重复位置、没有时间戳、空组、
  超过 64 KiB）：`timeline` 警告并跳过历史段；`rollback` 拒绝（退出 4）；`update`
  以当前 HEAD 重建并警告。`Save` 与 `Load` 共用同一个 `validate`：写入器拒绝一切
  读取器会拒收的形状，包括 `at <= 0` 与没有任何位置的组。
- `selector` 是操作者输入原样（`latest`、tag、sha、`-n 2`）；空串只表示"某次移动
  的起点"。

## 备选方案

**用 git reflog 当历史。** 零额外状态，但 reflog 是 git 的实现细节：条目会过期，
手工 rebase/reset 会混进来，`rollback` 会退到操作者从未部署过的位置。工具的历史
必须是自己写的事实。

**只记 current + previous。** 满足单步回退，`-n K` 就要靠连续执行；一次失败或
手工切换后，"上一步"是谁会变得含糊。

**每个 checkout 一个文件（路径哈希命名）。** 多 checkout 隔离更彻底，但状态目录
的文件名会变得不可读，而且"一个事实一个家"已经由分组满足。

**JSONL 追加日志。** 追加便宜，但裁剪、截断语义和原子替换都要自己实现，而文件
只有几十条。

## 后果

- 换来：`rollback` 的每一步都有据可查；手工切换过的起点也被记录；损坏不会伪装
  成"没有历史"。
- 代价：50 条上限之外的旧位置不可回退（可用 `update <sha>` 定点弥补）；手工
  `git checkout` 会产生一条 selector 为空的起点记录。
- fuzz 抓到过三个真实缺陷并已修复：栈中出现重复 commit 时 `Visit` 会保留重复；
  `Save` 会写出超过 64 KiB、`Load` 必然拒收的文档；非 UTF-8 的 checkout 路径在
  JSON 往返中被替换成 U+FFFD（属性测试因此显式排除不可表达的输入，并保留反例语料）。

## 验证

- `internal/history/history_test.go`：往返、未知字段、损坏七形态、权限、截断/
  前插/去重/上限、虚拟栈步进、组隔离。
- `internal/history/history_fuzz_test.go`：`Load` 不发明位置、`Visit` 保持子序列
  且无重复、`Step` 的单调性与边界、Save/Load 双射。
- `internal/app/rollback_test.go`：单步/多步/无历史/损坏/不联网/服务停启。
- `scripts/mutation-check.py`：`returning to a visited position no longer
  truncates the stack`。

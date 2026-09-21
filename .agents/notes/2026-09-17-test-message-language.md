# 决策: 测试失败信息用英文，中文留给面向操作者的文案

状态: 已实施

## 问题

`AGENTS.md` 写"人读的文案中文"，`dshctl-style` 与 `dshctl-testing` 把它展开成
"测试失败信息用中文"。树里的实际做法相反：按 `t.Fatalf`/`t.Errorf` 的首个字符串字面量
统计，英文 2623 条、中文 65 条，其中约 30 条是本次会话新加的。近期提交（`internal/cli`、
`internal/kernel`）也在加英文信息，`internal/repo/prune_audit_test.go` 里被 skill 引用为
范例的那条 `mustCheckout` 信息本身就是英文。

后果不是风格问题而是确定性问题：同一条规则下，两个 agent 会写出两种语言。本次会话里
一个 agent 在 `internal/detach`/`internal/host`/`internal/repo`（这三个包原本 100% 英文）
按规则写了中文信息，另一个 agent 在 `internal/kernel` 也写了中文，评审时必须先判断
"规则对还是树对"。规则与 97.6% 的代码相反时，规则不会被执行，只会被争论。

## 决定

- 语言按**读者**分，不按"人读/码读"分：终端前操作者读的（错误文案、命令 `Help`、
  README）中文；改代码的人读的（测试失败信息、标识符、注释、包文档）英文。
- 树里遗留的中文失败信息不是门禁问题（`check-conventions.py` 不检查这一点），改动那个
  文件时顺手改成英文；不为此单独发起一次全树改写。
- 三处规则同步：`AGENTS.md` 的「其他不变量」、`dshctl-style` 第 6 条、`dshctl-testing`
  第 10 条。判据写进 skill，避免下一个人再从"哪个语言更自然"开始讨论。

## 备选方案

**把 2623 条英文信息改成中文，让树服从原规则。** 落选：改动面是全树测试文件，
review 成本远大于收益，而且这些信息是给开发者的，英文与标识符、注释一致。

**保持规则不动，只要求新信息跟随所在文件的语言。** 落选：那条规则在混用文件里没有
唯一答案（`config_nodeversion_test.go` 是 75 条里 5 条中文），等于把判断重新交给每个
agent——正是这次要消灭的东西。

**不写规则，让评审去管。** 落选：评审不能决定一条写在 skill 里的规则是否作废；本次
两个 agent 的分歧就是这么产生的。

## 后果

- 新写的测试失败信息与树里绝大多数一致，评审不再需要在两种语言之间做裁量。
- 面向操作者的中文文案不受影响：产品错误、`Help`、README 仍是中文，判据是"谁在读"。
- 已知缺口：约 35 条历史中文失败信息留在树里（多在 `internal/kernel`、`internal/config`、
  `internal/nodejs`），只在改动那些文件时顺带处理。

## 验证

- `python3 scripts/check-conventions.py`：10 条规则通过（语言规则是文档约定，不由它检查；
  它检查的是 AGENTS.md 预算与包地图等结构事实）。
- 本次会话新增/改动的失败信息已全部改为英文：`internal/cli/documentation_test.go`、
  `internal/cli/readonly_test.go`、`internal/detach/detach_unix_test.go`、
  `internal/host/facts_unix_test.go`、`internal/host/hermetic_test.go`、
  `internal/repo/prune_test.go`、`internal/kernel/lock_test.go`、
  `internal/kernel/tolerance_test.go`。
- 统计口径：`grep` 每个 `*_test.go` 里 `t.Fatalf`/`t.Errorf`/`t.Fatal`/`t.Error` 的首个
  字符串字面量，按是否含 CJK 分类。

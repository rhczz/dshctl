# 决策: 部署历史按"最久未部署"淘汰整组，文件永远回到上限内

状态: 已实施

## 问题

`maxRecordsDefault=50` 只界**单个 checkout** 的栈深；文档本身还有一个读侧上限
`maxFileBytes=64 KiB`（约一条记录 140 B，一个满栈组约 3.8 KB）。checkout 的**组数**
没有任何裁汰：操作者若干年里迁移、删除、重建 checkout 目录，每个用过的路径都永久
保留自己的组。十来个满栈组即可触顶，之后 `Save` 永久拒绝，`recordDeploy` 失败——
表现为**部署成功却以 `exitcode.Failure` 收尾**，rollback 栈停止更新，唯一恢复手段
是手工删 `updates.json`。这是长寿命累积退化，且此前没有任何测试钉住这个状态。

## 决定

`Store.Save` 在文档超过 `maxFileBytes` 时不再直接拒绝：按"组的最新一条记录的时间"
淘汰最久的组（时间相同取字典序最后的路径，保证确定性），重编码直到文档回到上限内；
被刚部署的组天然时间最新，最后才会被淘汰，且**至少保留一组**。单组自身就超过上限
的情形仍然拒绝——那不是积累，是坏记录。淘汰发生在写入边界，`Load`、`Visit`、
`Step` 的语义不变。

## 备选方案

- **在 `Save` 失败信息里写 remedy（"删除 updates.json"）**：保留数据但让每次
  `update` 永久失败，恢复手段是手工干预，与"部署历史是自愈的状态"不符。
- **在 service 层裁剪**：字节上限属于 history 的契约，service 不应知道
  64 KiB 这个数字；两个写路径会出现两个真相。
- **按路径排序淘汰**（字典序最后的组先走）：与"部署历史 = 位置栈"的语义无关；
  按"最久未部署"淘汰与单栈"最老 position 淘汰"是同一哲学。

## 后果

- 最久未部署的 checkout 的回退栈会消失：这是有意的，与"单个栈最老的 position 淘汰"
  同一哲学（需要时操作者仍可用 `dshctl update <sha>` 显式回到任何版本）。
- 文档被淘汰裁剪后与调用方手里的 `File` 不再逐字节对应：对多组文档，
  `Save` 是"写入文档回到上限内的形状"。fuzz 的往返属性只构造单组文档，不受影响。
- 单组超限仍然响亮拒绝（既有测试 `TestSaveRefusesADocumentTooLargeToRead` 保持原样）。
- 老版本写出的超限文件在磁盘上时，`Load` 报 `ErrCorrupt`（too large），
  `recordDeploy` 已有的"读不了就重建"路径会把文档救回到一组——两层共同自愈。

## 验证

- `TestSaveEvictsTheStalestCheckoutsToStayUnderItsBound`：20 个满栈组（约 102 KB）
  先证明旧行为拒绝写入（红），实现后写入成功、文件回到上限内、最久的组被淘汰、
  最新的组完整保留。
- 变异锚点 `eviction may drop every group, including the one being written`
  （去掉 `&& len(file.Repos) > 1`）：由既有的单组超限测试证明会红。

# 决策: timeline 是唯一会写 .git 的报告命令

状态: 已实施

## 问题

操作者要在更新前看到"当前离远程最新还有多远"。远程最新只有 `git fetch` 之后才
知道，而 fetch 会写仓库的 `.git`（`FETCH_HEAD`、`refs/remotes/origin/*`）。这与
README 与 `readonly_test.go` 的承诺冲突："报告命令零写盘"。两者只能改一个。

## 决定

`timeline` 是这条规则的**具名例外**，边界写进 README、AGENTS.md 与包文档：

- 它联网 `git fetch origin --tags`（不 `--prune`：删除上游已删的远程跟踪引用是操作者的清理，不是读差距的副作用），只写 `.git` 的远程跟踪引用；
- 不写状态目录、不写配置文件、不改工作区（真实二进制测试逐字节断言）；
- 不拿操作锁：看差距不应该被一次长更新挡在锁外；
- fetch 失败时仍打印本地已知的时间线，头部写"远程: 无法获取（原因）"，差距行
  明确标注"基于本地已知状态，远程未确认"，**绝不出现"已是最新"**，并以退出码 4
  结束（"无法探测"在退出码表里就是 4）。

`readonly_test.go` 的零写盘清单不包含 `timeline`；另有一条真实二进制测试断言它
的边界：工作区快照不变、状态目录不存在、`FETCH_HEAD` 出现。

## 备选方案

**默认不 fetch，`--fetch` 才联网。** 默认路径绝对只读，但最容易踩的坑恰好是
"看到的是过期信息还以为是最新"——把正确性交给操作者记住一个开关。

**只读本地已知的 origin/master，永不联网。** 信息可能是几天前的，`timeline` 会
退化成"上次 fetch 时的世界"，而用户要它回答的正是"现在远程到哪了"。

**把预览并入 `update --check`，不新增命令。** fetch 仍然要发生，例外依然存在，
只是换了个命令名；而且"先看差距"会变成一个必须与 update 参数共存的模式。

**fetch 失败直接退出 1、不打印。** 断网时人眼也看不到本地已有的差距；打印已知
状态并明确标注，比什么都不给更有用。

## 后果

- 换来：差距是刚刚确认过的；断网时仍能看到本地已知状态，且不会被误导。
- 代价：`timeline` 比其它报告命令慢（一次网络往返），并且"报告命令零写盘"从
  绝对承诺变成带一处具名例外的承诺——例外本身由测试钉住。
- 已知缺口：`timeline` 与 `update` 并发时可能读到切换过程中的中间状态；两者都
  只读 git 的原子引用，不会看到半个对象，但"当前版本"可能是刚切换完还没构建的
  那个。

## 验证

- `internal/cli/release_test.go`：真实仓库与真实二进制——工作区逐字节不变、状态
  目录不存在、`FETCH_HEAD` 出现、`--json` 可消费、fetch 失败退出 4 且绝不出现
  "已是最新"。
- `internal/app/timeline_test.go`：窗口/tag/省略行、分叉、脏工作区、历史段、
  fetch 失败的本地状态与"远程未确认"。
- `scripts/mutation-check.py`：`a failed fetch still reports the timeline as
  confirmed`、`fetch prunes remote-tracking refs the remote no longer has`。

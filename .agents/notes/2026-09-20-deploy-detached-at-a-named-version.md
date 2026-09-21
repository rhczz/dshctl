# 决策: 部署位置用 detached HEAD，`latest` 固定为 origin/master

状态: 已实施

## 问题

`update` 原来只有一条路：`git pull --ff-only`，也就是永远跟到当前分支的上游。
DeepSeek Harness 处于开发期，破坏性 bug 会被合并进 master；操作者需要把服务钉在
某个 tag 或某个 commit 上，也需要在出问题时离开 master 尖端。`git pull` 只能
前进，不能定点，也不能后退。

## 决定

`update` 接受一个目标版本，三种形态：`latest`（默认）、tag、commit（完整或缩写
hash）。解析在停服之前完成，解析失败不动服务；目标等于当前 HEAD 时短路，不停服、
不构建、不重启。

- `latest` 固定指 `origin` 的 `master`，不跟随当前分支的 upstream；仓库没有
  `origin` 时 `update latest` 在 fetch 之前以退出码 4 拒绝（指定 tag/commit 仍可用，
  离线也能切换）。指定版本时先 fetch，再按 `refs/tags/<s>^{commit}`、
  `<s>^{commit}` 的顺序解析，选择器不得以 `-` 开头（否则会被 git 当成选项）。
- 本地分支名被拒绝（`refs/heads/*`）：`dshctl update master` 读起来像"最新的
  master"，实际会部署本地分支指针——它常常落后于 `origin/master`，而"目标不在
  origin/master 历史上"的警告对它保持沉默（落后的尖端仍是祖先）。要远程最新用
  `latest`，要具体提交用 tag 或 hash。
- 指定 tag/commit 用 `git checkout --detach <commit>`：不移动 master 分支指针，
  服务精确跑在目标提交上。`latest` 则回到 master 分支并 `merge --ff-only
  origin/master`，本地有未推送提交时按 git 的语义拒绝。
- 目标不在 `origin/master` 历史上时只警告（未合并的 release 分支 tag 是合法用法）。

## 备选方案

**跟随当前分支的 upstream。** 保留 `git pull` 的心智模型，但"最新"就取决于操作者
当时在哪个分支上：一个 topic 分支会让 `update` 部署它自己的上游，而操作者以为在
跟 release。harness 只有 master，契约固定反而没有歧义。

**`git reset --hard <目标>` 移动 master。** 不出现 detached HEAD，但会静默丢弃
分支上的本地提交，而且 master 指向旧位置时"分支"这个名字已经不成立。工具的底线
是绝不丢操作者的工作。

**工具自建跟踪分支（`checkout -B dshctl/active`）。** 语义清楚也不 detached，但
多一条需要维护和解释的隐藏分支；回退到某个 tag 时那条分支的指向仍然是个需要回答
的问题。

**在 `timeline` 里全量列出差距内的提交。** 实测相邻两个 release tag 之间就有 164
个 first-parent 提交，全量输出无法阅读。改成"最新窗口 ∪ 差距内全部 tag"。

## 后果

- 换来：更新可以钉在任意 tag/commit 上；出问题能离开 master；同一版本重复执行
  `update` 不再白白停服一次。
- 代价：指定版本后 checkout 处于 detached HEAD，`git status` 会这么说（对部署
  目录是正常语义）；不带参数的 `update` 负责回到 master。
- 已知缺口：运行记录里没有服务所用的 commit，"切换成功但构建失败"期间服务与
  checkout 的版本不一致；`status` 无法对比两者。

## 验证

- `internal/repo/release_test.go`：真实 git 夹具（bare remote + peer clone）钉住
  fetch/解析/计数/first-parent/detached 切换/快进与分叉拒绝/脏检查，以及本地分支
  被拒、`origin/master` 仍可解析。
- `internal/app/update_test.go`：短路、tag 切换不动 master、脏工作区拒绝、
  未知版本不停服、离线降级、无 origin 时 latest 拒绝而 tag 仍可用、历史外目标警告、
  记录写失败仍完成部署。
- `internal/app/rollback_test.go`：`TestUpdateAfterARollbackReturnsToMaster`
  钉住完整闭环（update → rollback 后 detached → update 回到 master）。
- `scripts/mutation-check.py`：`the version already deployed is rebuilt and
  restarted anyway`、`a worktree with tracked changes is switched anyway`、
  `a local branch name resolves as a version`、`HEAD is refused as a local
  branch`、`a no-op update forgets the checkout it ran against`、`latest is
  fetched without an origin`、`a sha selector is repeated in the target line`。

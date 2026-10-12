# 决策: Unix 归属证据在进程组之后补上父进程链

状态: 已实施

## 问题

harness 的 `package.json` 把 `packageManager` 升级为 pnpm@11 之后，`dshctl start` 在
真实 checkout 上必失败：服务器由本次启动拉起并已监听端口，dshctl 却报 "port is held
by another process"，随后把刚启动的服务清掉。进程树取证表明根因不在 harness：pnpm 11
的脚本执行器为了把终端中断转发给脚本自己，将每个 package script 放进一个 detach 出来
的 shell 会话（`sh -c`，新进程组、新会话）。dshctl 生成的 `pnpm <script>` 包装进程是
setsid 的组长，而真正监听端口的 node 的 pgid 是那个 `sh` 的 pid，不再是包装进程的 pid。
Unix 端的 `DescendsFrom` 只认进程组（`Getpgid(pid) == ancestor`），于是
`waitForListening` 把自家监听器判成外来者，`adoptSurvivor` 也认不出中断启动的幸存者。

pnpm 10 时代脚本留在调用者进程组里，这条证据成立；harness 把 packageManager 钉到
pnpm 11 后不再成立。把 pnpm 11 装到本机也不能回避：detach 是 pnpm 11 脚本执行器自己的
行为（进程树里 11.28.5 直接产生那个 `sh -c`），不是版本自切换工具的副产物。

## 决定

Unix 的 `host.DescendsFrom(ancestor, pid)` 按两段证据回答，进程组优先：

1. `Getpgid(pid) == ancestor`（原行为不变）——组比组长活得久，中断启动的幸存者依赖这
   一点被收养；组 id 在成员存活期间不可被复用，所以这一段的答案不会指认陌生者。
2. 组不覆盖时，沿父进程链向上走到 `ancestor`（Linux 直接读 `/proc/<pid>/stat`，其他
   Unix 用 `ps -o ppid=`；读不到即没有证据；步数上限 64）。

链的证据与 Windows 端已经在用的完全同构，成立性依赖内核的两条事实：父进程活着时
`ppid` 指向真实父进程；父进程死后的孤儿被重挂到 init，而不是任何被复用的 pid。因此
能走到活着的 ancestor 的链，就是 ancestor 真正产生的树；链断（中间进程已消失）时答案
退回"无法证明"，不猜。

所有权模型不变：运行记录 + 启动时间指纹仍是唯一的终止依据；本次只修"监听器是不是本次
启动的"这一就绪证据。

## 备选方案

- **绕开 package script，直接以 node 运行 harness 入口。** 把 dshctl 与 harness 的
  script 内容（`node --import tsx/esm apps/cli/src/bin.ts`）耦合起来，script 再变一次
  就要再修一次，也用不上 pnpm 对 PATH 的补充。
- **用环境变量关掉 pnpm 的版本自切换。** detach 在 pnpm 11 的脚本执行器里，与本机装
  的是哪个 pnpm 无关，关不掉；且会把 checkout 钉回 pnpm 10，对抗它自己声明的工具链。
- **只靠等待窗口放宽（端口先自由 + 持锁就认领）。** 放弃对"外部进程恰好抢占端口"的
  防御，静默接受陌生监听器，破坏"探测不了不当结论"。
- **只修等待路径，不改 `DescendsFrom`。** `adoptSurvivor`/`listenerStillOurs` 用的是同
  一证据，仍然坏着，中断启动的幸存者永远无法被收养。

## 后果

- start/restart 在 pnpm 11 形状下恢复；`stop` 本就按记录 pid + 指纹直接发信号，不受
  影响。
- 已知缺口：detach 形状下包装进程若在服务器存活时先死，链断且组不覆盖，幸存者只能
  人工收尾（fail-closed，报错不猜）。pnpm 10 形状没有这个缺口，因为组比组长活得久。
- 已知缺口（同源、未随本次修复）：`run.killTree` 在同步命令被取消时也只信号进程组，
  pnpm 11 detach 出来的构建脚本可能逃过清理；`cleanupFailedStart` 对 detach 形状依赖
  pnpm 自身的信号转发，转发失败时以"端口仍被占用"如实上报。
- 组证据落空时，链的每一跳要在 Linux 之外执行一次 `ps`；频率由等待轮询决定，链长
  为个位数。

## 验证

- `internal/host`：`TestDescendsFromReachesAChildThatLeftTheGroup`（detach 后的子进程
  必须被认领）、`TestDescendsFromKeepsAnsweringForAGroupWhoseLeaderExited`（组长被收割
  后组仍回答）、`TestDescendsFromUsesTheProcessGroup`（组优先）、
  `TestDescendsFromRejectsTargetsThatDoNotExist`（不可证明即拒绝）。
- `internal/service`：`TestStartAcrossAWrapperThatDetachesTheServer`（真实进程端到端：
  启动成功、记录指认监听器、stop 到达静止）。
- 变异：`python3 scripts/mutation-check.py --only "a script that left the group is no
  longer reached through the parent chain"` 与 `--only "a group stops answering for its
  members once the leader was reaped"` 各自可证明被测试抓住。

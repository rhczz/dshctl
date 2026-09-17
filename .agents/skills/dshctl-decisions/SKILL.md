---
name: dshctl-decisions
description: 在 dshctl 动手前定夺「这个值该固定还是该可配」「这段代码属于哪一层」「要不要引入抽象」「这个决定要不要写决策记录」。含三问决策程序、现有值的分类范例、边界校验位置、依赖方向与新包准入。用于新增设置、新包、新接口、跨层搬逻辑或任何契约性决定时。
---

# 动手前的决定

## 概要

这个 skill 管的是**写代码之前**的三类判断：一个值该写死还是做成配置、一段逻辑属于哪一层、要不要引入抽象或新包。判断错了，后面写得再干净也是错的形状：把安全不变量做成开关，等于给未来留一个可以关掉安全性的入口；把只该属于 `host` 的系统调用搬进 `service`，等于让虚构机器的测试缝失效。

本 skill 不写代码流程（见 `dshctl-tdd`），也不写门禁怎么跑（见 `dshctl-verify`）。

## 规则

1. **新增任何可调值之前，先走三问决策程序。** 三个问题按顺序问，任何一问答不上来就写死：

   1. **它随部署变化吗？** 不同机器、不同安装、不同操作者会取不同值吗？否 → 常量。
   2. **今天就有消费者要设它吗？** 不是"以后可能有人要"，而是现在就有具体的人或场景要改它。否 → 常量，或者要求调用方显式传参，而不是加一个带默认值的设置项。
   3. **它是协议常量、外部规范或安全不变量吗？** 是 → **写死**，并且不允许被任何 flag、环境变量或配置项覆盖。

2. **`DEFAULT_*` 常量和测试钩子不是可配置性。** 一个值有默认值不等于它可配置；把测试注入点当成配置项，会让测试专用的形状进入公开表面。判据是"谁会改它、通过什么入口改"。

3. **新增 `config.File` 字段必须同时给出当前消费者。** 说明谁会用这个键、用它解决什么，否则不加。这条来自上游约定的 "reserves `Config` fields for deployment-varying choices with a current consumer"。

4. **默认值只在 `config.Load` 一处解析。** 四层优先级（flag > env > 文件 > 默认）与每一项的来源打印都集中在那里；操作函数内部不得再出现隐藏的默认值（`if timeout == 0 { timeout = 30 }` 这类写法就是把解析分成了两处）。调用方拿到的是已经解析完的设置。

5. **配置错误在最早可判定点响亮失败。** 未知键拒绝而不是忽略，缺 pnpm/git/构建产物拒绝启动而不是降级，非法取值指名道姓地报出是哪个文件、哪个字段。静默跳过缺失的东西会让操作者以为配置生效了。

6. **校验只放在四处边界**：CLI 参数、配置文件、状态/日志文件、外部命令输出（`node -v`、`lsof`/`ss`/`netstat`/`ps`）。内部结构体之间是做类型保证的同一进程边界，不重复校验；反过来，从文件或子进程读回来的任何东西都必须当成不可信输入。

7. **依赖方向不可逆**：`cmd/dshctl → internal/cli → internal/service → 叶子`，跨层调用只经 `service`。系统调用只出现在 `host` 以及 `run`/`detach`/`logfile`/`atomically`/`lock`/`state`/`cmd/dshctl` 的平台文件里，上层不得出现 `if windows`。新增一条 import 边是设计决定，不是实现细节；`scripts/check-conventions.py` 会拦下未登记的边。

8. **接口只为可测性存在。** 当前全仓库只有四个接口：`run.Executor`/`Capturer`/`Outputer` 与 `service.OsHost`。它们存在，是因为那样就能把整个生命周期跑在虚构机器上。新增接口时，文档必须写明它买到了什么测试能力；"将来可能换实现"不是理由。

9. **新包准入三条同时成立**：它有独立的不变量、它能被独立测试、它不引入反向依赖。三条缺一条就把代码放进已有的包。

10. **契约性决定写进 `.agents/notes/`，与代码同一提交。** 触发条件是改变行为、契约、磁盘或配置格式、流程，或完成一次"该固定还是该可配"的判断；判据见 `.agents/notes/README.md`（"纯机械"= 单文件 + 无可观察行为变化 + 无磁盘格式变化 + 无门禁变化，四条同时成立才豁免）。`## 备选方案` 必写：没有记录它击败了什么的决定，会不断被重新讨论。

## 验证

```sh
python3 scripts/check-conventions.py     # 依赖方向、零第三方依赖、决策记录格式
go list -f '{{.ImportPath}} <- {{join .Imports " "}}' ./... | sed 's#github.com/rhczz/dshctl/##g' | sort
```

第二条命令打印真实的 import 图；新增一条边时，用它确认方向并更新 `check-conventions.py` 里的允许边集合。

## 相关文件

- [固定 vs 配置的完整分类与误判清单](references/fixed-vs-configurable.md)
- [`internal/config/config.go`](../../../internal/config/config.go)：四层解析、`File` 的指针字段与校验
- [`internal/paths/paths.go`](../../../internal/paths/paths.go)：环境变量常量与路径解析
- [`internal/service/host.go`](../../../internal/service/host.go)：`OsHost` 接口及其"为虚构机器而存在"的文档
- [`scripts/check-conventions.py`](../../../scripts/check-conventions.py)：分层允许边与零依赖检查
- [`.agents/notes/README.md`](../../notes/README.md)：决策记录何时写、怎么写

# 测试文件的骨架与范例

## 骨架

```go
package service

import (
	"context"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/exitcode"
)

// TestStopReportsAProcessThatSurvivesTheForceSignal 的骨架：停机必须到达静止，
// 对优雅请求和强杀都无动于衷的进程必须被报告为"没有结束"，而不是被宣称已停。
func TestStopReportsAProcessThatSurvivesTheForceSignal(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.mu.Lock()
	f.host.processes[4321].survivesGraceful = true
	f.host.processes[4321].survivesForce = true
	f.host.mu.Unlock()

	_, err := f.Stop(context.Background())
	wantCode(t, err, exitcode.Failure)
	if !strings.Contains(err.Error(), "强制结束后仍然存在") {
		t.Fatalf("Stop error = %v, want a still-alive report", err)
	}
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}, {4321, host.Force}})
	if !f.host.isAlive(4321) {
		t.Fatal("the stubborn process was reported as ended")
	}
}
```

（这个骨架锚定在真实测试 `internal/app/lifecycle_test.go` 的 `TestStopReportsAProcessThatSurvivesTheForceSignal` 上；新增生命周期测试照 `newFixture(t)` 的用法写，虚构进程表在 `internal/app/fake_test.go`。）

要点：`newFixture(t)` 是唯一入口，`f.host` 是虚构机器（进程表、端口、信号都记在它身上）；动作走真实入口 `f.Stop`；断言用 fixture 自带的 `wantSignals`/`wantCode`/`isAlive` 落在外部可观测行为上；失败信息说清期望与实际的差距。

## 范例索引

| 想学什么 | 看哪里 |
|---|---|
| 虚构机器怎么建、异常路径怎么表达 | `internal/app/fake_test.go`（虚构进程表，读它的 `newFixture` 与 `fakeHost`） |
| 跨包字面量的契约钉法 | `internal/app/contracts_test.go` |
| 真实二进制的只读承诺 | `internal/cli/readonly_test.go` |
| README 与设置常量/配置键的同步 | `internal/cli/documentation_test.go` |
| 表驱动 + 每种错误形状一行 | `internal/config/config_audit_test.go` |
| "还有第三种答案吗" 的审计测试 | `internal/logfile/logfile_audit_test.go` |
| helper 子进程的环境拼装 | `internal/host/helperenv_test.go` |
| 真实进程/工具探测 | `internal/host/realps_test.go` |
| 缺工具时为什么 Fatal 而不是 Skip | `internal/repo/prune_audit_test.go` 的 `mustCheckout` |
| fuzz 与回归种子 | `internal/**/testdata/fuzz/` |

## hermetic 清单

写测试前：

- 需要 HOME 时设 `t.Setenv("HOME", t.TempDir())`，Windows 上再设 `USERPROFILE`；不要假设测试运行在 CI 的干净机器上。
- 状态目录、日志路径、repo 路径全部指向 `t.TempDir()` 下的子目录。
- 不读、不写仓库工作目录里的文件（测试要用的 fixture 通过 `testdata/` 或内联字符串）。

写测试后自检：

- `make hermetic`：在空 HOME 下跑整套，并断言没有文件被创建在测试自己的临时目录之外；环境变量指向的路径连被创建都不允许——检查把"路径存在"即判 stray（空文件、空目录也算）。
- 如果测试必须创建真实文件，确认它们在 `t.TempDir()` 下，并且失败路径也会走到清理。

## helper 子进程模式

需要真实子进程时（`internal/host/helperenv_test.go`）的硬要求：

1. 标记环境变量命名**父进程 pid**，让并发的测试各自认领自己的子进程。
2. 子进程启动前把继承来的同名键剥掉，否则父进程的标记会让子进程误判自己是 helper。
3. 子进程侧用显式传入的程序路径，不依赖 PATH 上的同名程序。
4. 子进程的 stdout/stderr 在测试失败时要能看到，否则失败信息只有"退出码 1"。

## fake 什么、真实什么

| 组件 | 选择 | 理由 |
|---|---|---|
| `app.OsHost` | fake | 系统调用与进程表不可控；fake 让"优雅停不掉"这类路径可测 |
| `run.Executor`/`Capturer`/`Outputer` | fake | 外部命令的输出形状需要在测试里精确构造 |
| `config.Load` | 真实 | 解析与优先级本身就是被测行为 |
| `atomically` | 真实 | 原子替换与 fsync 只有真跑才能验证 |
| `lock` | 真实 | 锁的语义在 inode 上，fake 掉就什么都没测 |
| `logfile` | 真实 | 轮转要验证"同一个 inode"和子进程 append 句柄 |
| 端口探测 | 两者都要 | fake 测决策分支，真实探测测试证明它和世界对得上 |

## 反模式

- 断言 mock 被调用的次数与顺序 → 验证的是剧本，不是行为。
- 用 `time.Sleep` 等异步结果 → 慢机器 flake、快机器浪费；改成有上界的轮询。
- 测试之间共享状态目录或端口 → 只有单跑才过；修隔离，不要加 `-p 1`。
- 为了让测试通过而 `t.Skip` → CI 会拒绝未登记的 skip；新增合法 skip 必须同时进白名单。
- 在测试里复制一份**推导型**实现常量（如把某个由其他量算出来的阈值抄一遍）→ 实现改了测试跟着改，守不住任何东西；对协议常量与安全不变量则相反：`internal/config/config_nodeversion_test.go` 用字面量 `"24.12.0"` 对比 `MinNodeVersion` 正是守卫——改常量会红，逼着实现与文档一起改。

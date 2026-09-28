# 决策: status/record 的 JSON 不再携带无人读的键

状态: 已实施

## 问题

减法审计（2026-09）确认了一批"只写不读"的 JSON 字段：`Record.UpdatedAt`、`Record.Phase`（唯一合法值恒为 `running`）与 `Status.RepoReady`、`Status.RecordedPhase`、`Status.RecordedNodeVersion`、`Status.RecordedNodePath`、`Status.LockHolder`。它们由 service 写入、由任何渲染器与生产路径零消费，README 从未记载。每个字段都是线格式的一部分，却没有任何一个消费者为它们的存在作证；键一旦发布就需要永久兼容，维护者却说不清它们为谁服务。

## 决定

从 `domain.Record` 删除 `Phase`、`UpdatedAt`，从 `domain.Status` 删除 `RepoReady`、`RecordedPhase`、`RecordedNodeVersion`、`RecordedNodePath`、`LockHolder`，连同各自的写入点（`records.go` 的 `Stamp`、`observe.go`/`applyRecord` 的搬运）。保留的事实及其替代住处：

- "记录只属于一个已在端口上应答的服务器"这一不变量改由包文档陈述，行为仍由 start/stop 生命周期测试钉住。
- 运行时事实（node 版本、二进制路径）的真源是运行记录本身；`doctor` 的 "runtime record" 行（`Record.Describe`）继续承载它，测试逐字钉住。
- `status --json` 的机器键面收敛为 key-pin 测试（`TestStatusJSONCarriesTheStateMachineFields`）所列 10 个键；`lockUnreadable` 保留——它是"探测不了绝不当作没有"不变量在线格式里的体现，不属于本批减法。

金标（三平台）同步重录：status --json 输出不再含 `"repoReady"`。

## 备选方案

**保留字段并补消费者。** 例如让渲染器读 `RecordedNodeVersion`。落选：`doctor` 文本行与记录本身已承载同一事实，第三份渲染只会增加"同一事实多户"的漂移面。

**只删未钉的键。** 落选：`repoReady` 虽被 key-pin 测试钉住，但钉住它是"记录现状"而非"承诺消费"——测试文档写明它守的是重命名会红，删除键属于行为契约变更，随提交一并改测试与金标。

## 后果

旧的 `status --json` 消费者若解析过这些键，会取到零值（`omitempty` 的键直接消失）。README 从未记载它们，仓库内的脚本是零风险面；外部脚本按键是否存在来判定，取到缺失键即零值。运行记录文件（旧版写入的）多出的 `phase`/`updatedAt` 键被未知键容忍的读取路径忽略，无需迁移。

每行状态 JSON 少 1-3 个键，省去约 25 行生产代码与 5 处写入点；新 record 字段的搬运点从 6 个降到 3 个（`applyRecord`）。

## 验证

- `go test ./internal/service/ ./internal/cli/ ./internal/domain/ ./internal/state/ ./internal/conformance/ -count=1`
- `TestStatusJSONCarriesTheStateMachineFields`（键面收窄后的钉）、`TestDescribe`（诊断行不再含 phase）、`TestDoctorReportsEveryRowInOrder`（runtime record 行措辞随金标同步）
- 三平台金标 `TestConformance` 全绿；`make conventions`（账本 schema 无新增 todo）

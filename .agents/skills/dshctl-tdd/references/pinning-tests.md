# pinning 测试的形态与红-绿证明

一个 pinning 测试钉住的是一条**决策**：判定条件、顺序、上限、拒绝路径、契约字面量。它的价值不在于覆盖率，而在于"当有人改变这个决策时它会变红"。

## 五种形态，按决策的类别选

**表驱动 + 子测试**：同一决策的多个取值，这是仓库的默认形态（数量随代码变，要引用就现场 `grep`）。

```go
cases := []struct {
    name       string
    document   string
    wantSubstr string
}{ /* … */ }
for _, testCase := range cases {
    t.Run(testCase.name, func(t *testing.T) { /* … */ })
}
```

用途：解析、校验、分类、排序。`internal/config/config_audit_test.go` 的 `TestLoadRejectsWrongJSONTypes` 是范例——每种错误形状一行，且断言里带字段名。

**契约钉**：跨文件、跨包、跨进程的字面量必须一致，且改动是协调改动。范例：`internal/service/contracts_test.go` 把构建标记路径在 `config`、`repo`、`service` 三个包的推导结果钉成同一个字符串；`internal/cli/documentation_test.go` 把 `paths.Env*` 常量与 `config.File` 的 json tag 钉进 README。写这类测试的理由是失败模式是静默的：两边漂移后，构建成功会被报成"缺构建产物"。

**属性 / 边界审计**：`*_audit_test.go` 这个名字下的测试回答的是"这个函数还能给出第三种答案吗"。范例：`internal/logfile/logfile_audit_test.go` 的 `TestAllMatchesReportsWhenTheWindowMissedTheMatch` 区分"没有匹配"与"匹配在搜索窗口之外"——前者等待更久也没用，要如实上报"日志里没有地址"；后者必须上报搜索被截断，而不是宣称没有。凡是返回值多于"成功/失败"两态的地方，都该有这一类。

**fuzz**：输入来自外部（日志行、netstat 表、版本号字符串）。种子提交进 `testdata/fuzz/`，回归用例永久保留；fuzz 函数名本身是一句断言，例如 `FuzzParseVersionNeverInventsARelease`。

**真实进程 / 真实工具**：决策依赖真实世界的形状时才用。`internal/host/realps_test.go`、`internal/repo/prune_audit_test.go` 的 `mustCheckout` 是范例——它在缺少 git 时 `t.Fatalf` 而不是 `t.Skip`，理由写在注释里：静默跳过会让 dshctl 最具破坏性的那一半（清理残留目录）在绿灯下无人验证。

## 红-绿证明的三种做法

1. **先跑一次**。写完测试、实现还没改时跑它，确认红，并且红在**期望的那条断言**上（不是编译错误、不是别的用例带崩）。
2. **临时撤回实现**。实现已经写完才发现该补测试时，用 `git stash push <实现文件>` 把实现撤掉再跑一次；确认红，然后 `git stash pop`。先确认测试只依赖被测行为、不依赖新实现才有的符号，否则 stash 后红的是编译错误而不是断言。
3. **交给 `make mutation`**。改动 `internal/nodejs` 或配置层决策时不必手工做：`scripts/mutation-check.py` 会逐条做字面替换、跑相关包、要求失败、再还原。它覆盖的条数以 `python3 scripts/mutation-check.py --list` 为准（别抄数字；CI 把它切成 6 个分片跑，`check-workflow.py` 保证分片无洞），其中包括：

   - 下限判定是开区间还是闭区间
   - 已测试的大版本是否必须一致
   - 被拒绝的版本是否必须给出补救办法
   - 版本请求是否被归一化（`latest` 的含义）
   - 从二进制读到的版本是否必须长得像版本
   - 版本管理器与 PATH 的先后
   - 转发条目背后的真实解释器是否被解析
   - 探测是否有上界
   - `--node` 是否真的生效
   - 配置文件与环境变量谁优先

   新增一条同类决策时，往脚本的 `MUTATIONS` 里加一条字面替换——加不进去（比如实现里没有可替换的字面量）通常说明这个决策没有被写成一个可测的形状。

## 断言强度的反例

- `if err != nil { t.Fatal(err) }` 之后不检查返回值 → 顺序错了、字段错了都发现不了。
- `wantSubstr` 取一个两种错误都会包含的词（如"失败"）→ 用错误里真正区分彼此的部分。
- 只断言错误非 nil 而不看退出码归类 → `exitcode.Usage` 与 `exitcode.Preflight` 混用不会被发现。
- 只在 happy path 上跑一遍 → 拒绝路径没有反向用例，边界放开时会静默通过。

## 什么时候不该写测试

不写测试的情况只有一种：这段代码不承载决策（纯转发、纯格式化）。即使如此，如果它的输出是外部契约（`--json` 字段名、帮助文案），仍然要有测试钉住——`documentation_test.go` 就是为这种"没有逻辑但有承诺"的地方存在的。

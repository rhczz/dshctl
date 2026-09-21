# 重写期的账本与金标

`internal/conformance/accounting/accounting.json` 给树上每个测试一行；删除或搬走一个测试时，
把它的 `disposition` 从 `todo` 改成 `kept`/`new-test`/`conformance`/`differential`/`merged`/`obsolete`
并写明证据，`python3 scripts/check-accounting.py` 会拒绝一行无声消失。`obsolete` 只允许用于
"旧实现的偶然行为且不在冻结表面内"，逐条说明。黑盒行为由 `internal/conformance` 的金标钉住：
金标从参照版本录制，改它等于改契约（见 `dshctl-architecture`）。

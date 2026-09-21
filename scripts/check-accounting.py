#!/usr/bin/env python3
"""Check the rewrite ledger against the tree it describes.

The v0.3 branch replaces the implementation and rewrites the test suite, so the
tests themselves stop being the specification. The ledger under
internal/conformance/accounting/ is what takes their place: every test the tree
contains has a row, every row says what replaces it, and every decision the
mutation script pins is on the roster. This check is what makes the ledger
load-bearing instead of decorative — a test that is deleted without a
disposition is exactly the failure the whole apparatus exists to prevent.

Usage:
    check-accounting.py [--strict]

--strict is the merge gate: no undecided rows, every decision ported, every
piece of evidence filled in.
"""

from __future__ import annotations

import argparse
import ast
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
ACCOUNTING = ROOT / "internal" / "conformance" / "accounting"
MUTATION_SCRIPT = ROOT / "scripts" / "mutation-check.py"

# TestMain is a hook rather than a test, so the ledger skips it.
TEST_FUNC = re.compile(r"^func (Test|Fuzz|Benchmark)(?!Main\b)[A-Za-z0-9_]*\(")
DISPOSITIONS = ("todo", "new-test", "conformance", "differential", "merged", "obsolete")
NEEDS_EVIDENCE = ("new-test", "conformance", "differential", "merged", "obsolete")
ADDITIONS = {
    "mutating-json": "--json on the six mutating commands",
    "log-level": "--log-level / DSHCTL_LOG_LEVEL for stderr diagnostics",
    "log-step-lines": "step timing lines appended inside a log section",
    "version-json-fields": "version --json carries toolchain and module",
    "doctor-json-fields": "doctor --json carries the same build facts",
}


def fail(message: str) -> None:
    print(f"检查失败: {message}", file=sys.stderr)


def load(name: str) -> object:
    path = ACCOUNTING / name
    if not path.exists():
        raise ValueError(f"缺少 {path.relative_to(ROOT)}")
    return json.loads(path.read_text(encoding="utf-8"))


def tree_tests() -> set[str]:
    """Test ids present in the tree, scanned independently of the generator."""
    found = set()
    for path in sorted(ROOT.rglob("*_test.go")):
        if any(part in {".git", "bin", "dist", "node_modules"} for part in path.parts):
            continue
        relative = path.relative_to(ROOT).as_posix()
        for line in path.read_text(encoding="utf-8").splitlines():
            if TEST_FUNC.match(line):
                found.add(f"{relative}::{line[len('func '):line.index('(')]}")
    return found


def script_decisions() -> set[str]:
    """Decision names pinned by the mutation script, read from its literal."""
    tree = ast.parse(MUTATION_SCRIPT.read_text(encoding="utf-8"))
    names = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.AnnAssign):
            target, value = node.target, node.value
        elif isinstance(node, ast.Assign):
            target, value = node.targets[0], node.value
        else:
            continue
        if not (isinstance(target, ast.Name) and target.id == "MUTATIONS"):
            continue
        if not isinstance(value, ast.List):
            continue
        for element in value.elts:
            if isinstance(element, ast.Tuple):
                names.add(ast.literal_eval(element.elts[0]))
    return names


def check_accounting(strict: bool) -> int:
    rows = load("accounting.json")
    if not isinstance(rows, list):
        fail("accounting.json 不是列表")
        return 1
    seen: set[str] = set()
    problems = []
    undecided = 0
    for row in rows:
        test = row.get("test", "")
        if test in seen:
            problems.append(f"重复行: {test}")
        seen.add(test)
        disposition = row.get("disposition", "")
        if disposition not in DISPOSITIONS:
            problems.append(f"{test}: 未知 disposition {disposition!r}")
            continue
        if disposition == "todo":
            undecided += 1
        if strict and disposition in NEEDS_EVIDENCE and not row.get("evidence"):
            problems.append(f"{test}: {disposition} 缺少 evidence")
        if strict and disposition == "obsolete" and not row.get("note"):
            problems.append(f"{test}: obsolete 必须写明原因")
    present = tree_tests()
    for test in sorted(present - seen):
        problems.append(f"树里有测试但账本没有行: {test}")
    for test in sorted(seen - present):
        row = next(r for r in rows if r["test"] == test)
        if row.get("disposition") == "todo":
            problems.append(f"测试已从树中消失但仍未决定 disposition: {test}")
    if strict and undecided:
        problems.append(f"仍有 {undecided} 行是 todo")
    return report(problems, f"账本 {len(rows)} 行，树中测试 {len(present)} 个，未决定 {undecided}")


def check_decisions(strict: bool) -> int:
    rows = load("decisions.json")
    if not isinstance(rows, list):
        fail("decisions.json 不是列表")
        return 1
    roster = {row.get("name", "") for row in rows}
    script = script_decisions()
    problems = []
    for name in sorted(script - roster):
        problems.append(f"变异脚本里有名册缺失的决策: {name}")
    for name in sorted(roster - script):
        problems.append(f"名册里有变异脚本已删除的决策: {name}")
    if strict:
        for row in rows:
            if row.get("status") != "ported":
                problems.append(f"决策尚未迁移: {row.get('name')}")
            elif not row.get("new_path"):
                problems.append(f"决策已标记迁移但没有新锚点: {row.get('name')}")
            elif not (ROOT / str(row["new_path"])).exists():
                problems.append(f"决策的新锚点文件不存在: {row.get('new_path')}")
    return report(problems, f"决策名册 {len(rows)} 条，变异脚本 {len(script)} 条")


def check_additions(strict: bool) -> int:
    rows = load("additions.json")
    if not isinstance(rows, list):
        fail("additions.json 不是列表")
        return 1
    problems = []
    ids = set()
    for row in rows:
        identifier = row.get("id", "")
        ids.add(identifier)
        if identifier not in ADDITIONS:
            problems.append(f"加法清单里有未登记的项: {identifier}")
        if strict:
            if not row.get("consumer") or not row.get("test") or not row.get("readme"):
                problems.append(f"加法项缺少消费者/测试/README 记账: {identifier}")
    for identifier in sorted(set(ADDITIONS) - ids):
        problems.append(f"加法清单缺少已批准的项: {identifier}")
    return report(problems, f"加法清单 {len(rows)} 项")


def check_exemptions(strict: bool) -> int:
    rows = load("exemptions.json")
    if not isinstance(rows, list):
        fail("exemptions.json 不是列表")
        return 1
    problems = []
    if strict:
        for row in rows:
            for field in ("scenario", "field", "old", "new", "reason", "test"):
                if not row.get(field):
                    problems.append(f"豁免项缺少 {field}: {row.get('id', '?')}")
    return report(problems, f"豁免清单 {len(rows)} 项")


def report(problems: list[str], summary: str) -> int:
    if problems:
        for problem in problems:
            fail(problem)
        return 1
    print(f"账本检查通过（{summary}）")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--strict", action="store_true", help="merge gate: no undecided rows")
    args = parser.parse_args()
    status = 0
    for check in (check_accounting, check_decisions, check_additions, check_exemptions):
        status |= check(args.strict)
    return status


if __name__ == "__main__":
    sys.exit(main())

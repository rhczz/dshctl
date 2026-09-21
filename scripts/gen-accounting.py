#!/usr/bin/env python3
"""Generate the rewrite's accounting artifacts from the tree they describe.

The v0.3 rewrite replaces the implementation and rewrites the test suite, so the
old tests cannot be the specification any more. What replaces them is a ledger:
every test the tree contains gets a row, every row gets a disposition from a
closed set, and the strict check refuses to let the branch merge while a row is
still undecided. This script keeps that ledger in sync with the tree: it adds
rows for new tests, keeps the dispositions a human wrote, and records the
decisions pinned by scripts/mutation-check.py as a roster.

Usage:
    gen-accounting.py [--check]

--check fails when regenerating would change a file, which is how CI notices a
branch that added a test without a ledger row.
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

# TestMain is a hook rather than a test, so the ledger skips it.
TEST_FUNC = re.compile(r"^func (Test|Fuzz|Benchmark)(?!Main\b)[A-Za-z0-9_]*\(")
MUTATION_SCRIPT = ROOT / "scripts" / "mutation-check.py"

# The dispositions a row may carry. `todo` is the only one that blocks a merge:
# it means nobody has decided yet what replaces this test.
DISPOSITIONS = ("todo", "new-test", "conformance", "differential", "merged", "obsolete")


def test_rows() -> list[dict[str, str]]:
    """Every test function in the tree, with the package it belongs to."""
    rows = []
    for path in sorted(ROOT.rglob("*_test.go")):
        if any(part in {".git", "bin", "dist", "node_modules"} for part in path.parts):
            continue
        relative = path.relative_to(ROOT).as_posix()
        package = path.parent.relative_to(ROOT).as_posix()
        for line in path.read_text(encoding="utf-8").splitlines():
            match = TEST_FUNC.match(line)
            if match:
                name = line[len("func "):line.index("(")]
                rows.append({"test": f"{relative}::{name}", "file": relative, "package": package})
    return rows


def mutation_elements() -> list[ast.expr]:
    """The MUTATIONS literal, which is an annotated assignment in the script."""
    tree = ast.parse(MUTATION_SCRIPT.read_text(encoding="utf-8"))
    for node in ast.walk(tree):
        if isinstance(node, ast.AnnAssign):
            target, value = node.target, node.value
        elif isinstance(node, ast.Assign):
            target, value = node.targets[0], node.value
        else:
            continue
        if isinstance(target, ast.Name) and target.id == "MUTATIONS" and isinstance(value, ast.List):
            return value.elts
    raise ValueError("scripts/mutation-check.py 里找不到 MUTATIONS")


def decision_rows() -> list[dict[str, object]]:
    """The roster of decisions pinned by the mutation script, by identity."""
    roster = []
    for element in mutation_elements():
        if not isinstance(element, ast.Tuple):
            continue
        name = ast.literal_eval(element.elts[0])
        path = ast.literal_eval(element.elts[1])
        packages = ast.literal_eval(element.elts[4])
        roster.append({"name": name, "old_path": path, "packages": packages})
    return roster


def merge(previous: list[dict], fresh: list[dict], key: str, defaults: dict) -> list[dict]:
    """Keep what a human decided, refresh what the tree decides."""
    known = {row[key]: row for row in previous}
    merged = []
    for row in fresh:
        old = known.pop(row[key], None)
        entry = dict(row)
        for field, value in defaults.items():
            entry.setdefault(field, old.get(field, value) if old else value)
        merged.append(entry)
    # Rows whose subject left the tree stay in the ledger: a deleted test is a
    # decision somebody made, and it is exactly the row that must be accounted.
    for row in known.values():
        entry = dict(row)
        for field, value in defaults.items():
            entry.setdefault(field, value)
        merged.append(entry)
    return merged


def render(path: pathlib.Path, payload: object) -> str:
    return json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=False) + "\n"


def write(path: pathlib.Path, payload: object, check: bool) -> bool:
    text = render(path, payload)
    if check:
        if not path.exists() or path.read_text(encoding="utf-8") != text:
            print(f"生成结果与磁盘不一致: {path.relative_to(ROOT)}", file=sys.stderr)
            return False
        return True
    path.parent.mkdir(parents=True, exist_ok=True)
    if not path.exists() or path.read_text(encoding="utf-8") != text:
        path.write_text(text, encoding="utf-8")
        print(f"已写入 {path.relative_to(ROOT)}")
    return True


def load(path: pathlib.Path) -> list[dict]:
    if not path.exists():
        return []
    return json.loads(path.read_text(encoding="utf-8"))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true", help="fail instead of writing")
    args = parser.parse_args()

    accounting = merge(
        load(ACCOUNTING / "accounting.json"),
        test_rows(),
        key="test",
        defaults={"behavior": "", "disposition": "todo", "evidence": "", "note": ""},
    )
    decisions = merge(
        load(ACCOUNTING / "decisions.json"),
        decision_rows(),
        key="name",
        defaults={"new_path": None, "status": "pending"},
    )

    ok = write(ACCOUNTING / "accounting.json", accounting, args.check)
    ok = write(ACCOUNTING / "decisions.json", decisions, args.check) and ok
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())

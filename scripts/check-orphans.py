#!/usr/bin/env python3
"""Report Go files and packages nothing in the tree reaches.

A rewrite that moves code leaves orphans: a package nobody imports any more, a
file whose package clause disagrees with its neighbours, a leftover directory
that still compiles. The compiler cannot see any of these — they are exactly the
code that compiles and is never called.

Two things are checked, both mechanically decidable:

  * every package under internal/ and cmd/ is imported by something, or is the
    entry point, or is listed in SUPPORT with a reason;
  * every file in a package declares that package.

Exported symbols with no reader are deliberately not checked: deciding that with
a text search produces false positives on reflection, on platform files and on
test-only use, and a gate that cries wolf is a gate somebody turns off. Review
carries that question. That is why SUPPORT exists as a list of reasons rather
than a suppression file.

Usage:
    check-orphans.py
"""

from __future__ import annotations

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
MODULE = "github.com/rhczz/dshctl/"

IMPORT = re.compile(r'^\s*(?:[\w.]+ )?"(github\.com/rhczz/dshctl/[^"]+)"', re.MULTILINE)
PACKAGE = re.compile(r"^package (\w+)", re.MULTILINE)

# Packages that are legitimately not imported by anything: each entry needs the
# reason it does not have a caller, so the list cannot grow quietly.
SUPPORT = {
    "internal/conformance": "黑盒套件与差分预言机的宿主，只被 go test 使用",
}

# The entry point is the root of the import graph.
ROOTS = {"cmd/dshctl"}


def go_files() -> list[pathlib.Path]:
    files = []
    for path in sorted(ROOT.rglob("*.go")):
        relative = path.relative_to(ROOT).as_posix()
        if relative.startswith(("docs/", "bin/", "dist/")) or "/.git/" in relative:
            continue
        if relative.split("/")[0] not in {"cmd", "internal"}:
            continue
        files.append(path)
    return files


def package_of(path: pathlib.Path) -> str:
    return path.relative_to(ROOT).parent.as_posix()


def importers() -> dict[str, set[str]]:
    """Map each internal package to the packages that import it."""
    graph: dict[str, set[str]] = {}
    for path in go_files():
        text = path.read_text(encoding="utf-8")
        owner = package_of(path)
        for match in IMPORT.finditer(text):
            target = match.group(1)[len(MODULE):].rstrip("/")
            graph.setdefault(target, set()).add(owner)
    return graph


def mismatched_files() -> list[str]:
    """Files whose package clause differs from the first file of the directory."""
    declared: dict[str, str] = {}
    problems = []
    for path in go_files():
        match = PACKAGE.search(path.read_text(encoding="utf-8"))
        if match is None:
            continue
        directory = package_of(path)
        clause = match.group(1)
        # A _test package suffix (`foo_test`) is the external test package.
        base = clause[:-5] if clause.endswith("_test") else clause
        if directory not in declared:
            declared[directory] = base
        elif declared[directory] != base:
            problems.append(f"{path.relative_to(ROOT)}: package {clause}，但同目录是 {declared[directory]}")
    return problems


def orphan_packages(graph: dict[str, set[str]]) -> list[str]:
    packages = {package_of(path) for path in go_files()}
    orphans = []
    for package in sorted(packages - ROOTS - set(SUPPORT)):
        callers = {owner for owner in graph.get(package, set()) if owner != package}
        if callers:
            continue
        # A package whose files are all tests is a suite, not code: `go test`
        # reaches it, and there is nothing for a production importer to import.
        files = [path for path in go_files() if package_of(path) == package]
        if all(path.name.endswith("_test.go") for path in files):
            continue
        orphans.append(package)
    return orphans


def main() -> int:
    graph = importers()
    problems = [f"没有任何包引用 {package}" for package in orphan_packages(graph)]
    problems += mismatched_files()
    # A support entry that no longer exists is stale bookkeeping.
    packages = {package_of(path) for path in go_files()}
    for package in sorted(set(SUPPORT) - packages):
        problems.append(f"SUPPORT 里的 {package} 已经不存在")
    if problems:
        for problem in problems:
            print(f"检查失败: {problem}", file=sys.stderr)
        return 1
    count = len(packages)
    print(f"孤儿检查通过（{count} 个包都被引用，SUPPORT {len(SUPPORT)} 项）")
    return 0


if __name__ == "__main__":
    sys.exit(main())

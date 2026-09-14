#!/usr/bin/env python3
"""Gate statement coverage of chosen packages from a Go coverage profile.

A threshold is worth having where a package decides something a test cannot
observe afterwards — here, which Node runtime a long-running service uses and how
its release is recorded. Reading the profile directly means that check costs no
second test run: one `go test -v -coverprofile=...` execution is enough to
inspect skips, stray files and coverage together.

Usage:
    check-coverage.py PROFILE [--require PACKAGE=PERCENT]... [--report PACKAGE|all]...

The package names are import-path suffixes, so `internal/nodejs` is what the
profile's `github.com/rhczz/dshctl/internal/nodejs/nodejs.go` entries are
grouped under.
"""

from __future__ import annotations

import argparse
import pathlib
import sys
from collections import defaultdict

# The module path every profile entry is prefixed with.
MODULE = "github.com/rhczz/dshctl/"

# A profile rounds percentages to one decimal, so a requirement that is met
# exactly can miss the comparison by a rounding error: 100% of 1 statement
# divided by 1 is exact, but 2/3*100 is not 66.7 in binary floating point.
EPSILON = 0.05


def parse(profile: pathlib.Path) -> dict[str, tuple[int, int]]:
    """Return {package: (covered, total)} counted in statements."""
    counts: dict[str, list[int]] = defaultdict(lambda: [0, 0])
    try:
        text = profile.read_text()
    except OSError as error:
        print(f"无法读取覆盖率文件: {error}", file=sys.stderr)
        raise SystemExit(2)

    for line in text.splitlines():
        if not line or line.startswith("mode:"):
            continue
        try:
            location, statements, count = line.rsplit(" ", 2)
            path = location.split(":")[0]
            total = int(statements)
            hit = int(count)
        except ValueError:
            print(f"无法解析覆盖率文件中的这一行: {line!r}", file=sys.stderr)
            raise SystemExit(2)
        if not path.startswith(MODULE):
            continue
        package = str(pathlib.PurePosixPath(path[len(MODULE) :]).parent)
        counts[package][1] += total
        if hit > 0:
            counts[package][0] += total
    return {name: (covered, total) for name, (covered, total) in counts.items()}


def percent(covered: int, total: int) -> float:
    """The statement-weighted percentage, as `go tool cover` reports it."""
    if total == 0:
        return 100.0
    return 100.0 * covered / total


def uncovered(profile: pathlib.Path, package: str, limit: int = 5) -> list[str]:
    """Return the first uncovered blocks of a package, for a failure message."""
    prefix = MODULE + package + "/"
    lines: list[str] = []
    for line in profile.read_text().splitlines():
        if not line or line.startswith("mode:") or not line.startswith(prefix):
            continue
        location, _, count = line.rsplit(" ", 2)
        if int(count) == 0:
            lines.append(location.replace(MODULE, ""))
        if len(lines) == limit:
            break
    return lines


def normalize(name: str) -> str:
    """Accept an import path, a package suffix, or a directory spelling."""
    trimmed = name.strip().removeprefix(MODULE).strip("/")
    return trimmed


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profile", type=pathlib.Path, help="a -coverprofile file")
    parser.add_argument(
        "--require",
        action="append",
        default=[],
        metavar="PACKAGE=PERCENT",
        help="fail unless PACKAGE reaches PERCENT (repeatable)",
    )
    parser.add_argument(
        "--report",
        action="append",
        default=[],
        metavar="PACKAGE|all",
        help="print coverage for one more package, or for all of them",
    )
    arguments = parser.parse_args()

    coverage = parse(arguments.profile)
    if not coverage:
        print(f"{arguments.profile} 里没有本模块的覆盖率数据", file=sys.stderr)
        return 2

    failures: list[str] = []
    for requirement in arguments.require:
        package, separator, wanted = requirement.partition("=")
        if not separator:
            parser.error(f"--require 需要 PACKAGE=PERCENT 形式: {requirement!r}")
        name = normalize(package)
        try:
            threshold = float(wanted)
        except ValueError:
            parser.error(f"--require 的百分比不是数字: {wanted!r}")
        covered, total = coverage.get(name, (0, 0))
        got = percent(covered, total)
        verdict = "ok  " if got + EPSILON >= threshold else "FAIL"
        print(f"{verdict} {name}: {got:.1f}% (要求 {threshold:g}%，{covered}/{total} 条语句)")
        if verdict == "FAIL":
            failures.append(name)
            for block in uncovered(arguments.profile, name):
                print(f"       未覆盖: {block}")

    reported = {normalize(name) for name in arguments.report if name != "all"}
    if "all" in arguments.report:
        reported.update(coverage)
    for name in sorted(reported):
        if any(normalize(requirement.partition("=")[0]) == name for requirement in arguments.require):
            continue  # already printed as a requirement
        covered, total = coverage.get(name, (0, 0))
        print(f"     {name}: {percent(covered, total):.1f}% ({covered}/{total} 条语句)")

    if failures:
        print(f"\n覆盖率未达标: {', '.join(failures)}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

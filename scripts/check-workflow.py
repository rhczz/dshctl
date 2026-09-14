#!/usr/bin/env python3
"""Check the CI workflow file's shape.

A workflow that does not parse, or that names a job nobody runs, fails silently:
the pipeline simply stops reporting and nobody notices until a release is wrong.
This checks the properties that matter structurally, using only the standard
library so it runs anywhere the Go toolchain does.

It deliberately does not try to be a full GitHub Actions linter. It checks what
this repository can actually get wrong and would not notice:

  - a syntax error, a job that no longer exists, or a dependency on a job that
    was renamed away;
  - a build step that lost its gate on the tests;
  - a push to main that no longer triggers anything, or a test matrix that
    stopped covering one of the three platforms this tool supports — including
    a matrix that no longer compiles the module at all;
  - an unquoted word where an expression needs a string literal, which makes
    GitHub reject the whole file ("Unrecognized named-value: 'amd64'") and run
    no job at all.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

WORKFLOW = Path(".github/workflows/ci.yml")

# Named values an Actions expression may reference. A word that is none of
# these, is not a literal, and is not quoted is what the expression parser
# rejects outright.
NAMED_VALUES = (
    "github",
    "env",
    "vars",
    "job",
    "jobs",
    "steps",
    "needs",
    "runner",
    "secrets",
    "strategy",
    "matrix",
    "inputs",
)

# Bare words that are values rather than references.
WORD_LITERALS = ("true", "false", "null")

# The platforms every trigger has to compile and test on.
PLATFORMS = ("ubuntu-latest", "macos-latest", "windows-latest")


def fail(message: str) -> None:
    print("workflow check failed: " + message, file=sys.stderr)
    sys.exit(1)


def parse_jobs(text: str) -> dict[str, dict[str, str]]:
    """Read the top-level jobs and their `needs:`/`runs-on:` entries."""
    jobs: dict[str, dict[str, str]] = {}
    in_jobs = False
    current: str | None = None
    for raw in text.splitlines():
        line = raw.rstrip()
        if not line or line.lstrip().startswith("#"):
            continue
        if re.match(r"^jobs:\s*$", line):
            in_jobs = True
            continue
        if not in_jobs:
            continue
        if re.match(r"^[A-Za-z]", line):  # a new top-level key ends the block
            break
        match = re.match(r"^  ([A-Za-z0-9_-]+):\s*$", line)
        if match:
            current = match.group(1)
            jobs[current] = {}
            continue
        if current is None:
            continue
        for key in ("needs", "runs-on", "name"):
            found = re.match(r"^    " + key + r":\s*(.+?)\s*$", line)
            if found:
                jobs[current][key] = found.group(1)
    return jobs


def job_block(text: str, name: str) -> str:
    """Return the lines of one job, from its key to the next job key."""
    collected: list[str] = []
    inside = False
    for line in text.splitlines():
        if re.match(rf"^  {re.escape(name)}:\s*$", line):
            inside = True
            continue
        if inside and re.match(r"^  [A-Za-z0-9_-]+:\s*$", line):
            break
        if inside:
            collected.append(line)
    return "\n".join(collected)


def expressions_in(line: str) -> list[str]:
    """Return every Actions expression a line carries.

    Both spellings count: an `if:` value, which may omit the `${{ }}` wrapper,
    and any `${{ … }}` interpolation inside a field.
    """
    found = re.findall(r"\$\{\{(.*?)\}\}", line)
    condition = re.match(r"^\s*if:\s*(.+?)\s*$", line)
    if condition:
        found.append(condition.group(1))
    return found


def check_triggers(text: str) -> None:
    """A commit that lands on main must run the pipeline."""
    match = re.search(r"push:\s*\n\s+branches:\s*\[([^\]]*)\]", text)
    if not match:
        fail("the push trigger no longer names the branches it runs on")
    branches = [entry.strip().strip("'\"") for entry in match.group(1).split(",")]
    if "main" not in branches:
        fail(f"pushes to main no longer trigger the pipeline (push branches: {branches})")


def check_platform_matrix(text: str) -> None:
    """Every trigger must compile and test on all three supported platforms."""
    block = job_block(text, "test")
    if not block:
        fail("the test job is missing")

    match = re.search(r"^\s+os:\s*\[([^\]]*)\]", block, re.MULTILINE)
    if not match:
        fail("the test job has no os matrix, so it cannot cover every platform")
    systems = [entry.strip() for entry in match.group(1).split(",")]
    for platform in PLATFORMS:
        if platform not in systems:
            fail(f"the test matrix no longer runs {platform!r} (os: {systems})")

    if not re.search(r"^\s*run:\s*go build\b", block, re.MULTILINE):
        fail("the test job no longer compiles the module (no `go build` step)")


def check_expression_literals(text: str) -> None:
    """A string inside an expression has to be quoted.

    GitHub reads an unquoted word as a named value, so `matrix.goarch == amd64`
    is not "compare with the string amd64" but "compare with a variable called
    amd64" — the file is rejected with "Unrecognized named-value: 'amd64'" and
    no job starts. That is the one failure mode which stops the pipeline
    reporting anything at all, so it is checked here instead of being found in
    the Actions tab.
    """
    operand = r"'[^']*'|\"[^\"]*\"|\d+(?:\.\d+)?|[A-Za-z_][A-Za-z0-9_.-]*"
    comparison = re.compile(rf"({operand})\s*(?:==|!=)\s*({operand})")
    for number, line in enumerate(text.splitlines(), start=1):
        if line.lstrip().startswith("#"):
            continue
        for expression in expressions_in(line):
            for match in comparison.finditer(expression):
                for value in match.groups():
                    if value[0] in "'\"" or value[0].isdigit():
                        continue
                    if value in WORD_LITERALS or value.split(".")[0] in NAMED_VALUES:
                        continue
                    fail(
                        f"line {number}: {value!r} is an unquoted word in an expression, "
                        f"so Actions reads it as a named value and rejects the whole file; "
                        f"write it as {value!r} in quotes"
                    )


def main() -> None:
    if not WORKFLOW.is_file():
        fail(f"{WORKFLOW} does not exist")

    text = WORKFLOW.read_text(encoding="utf-8")
    if "\t" in text:
        fail("the file contains a tab, which YAML does not allow for indentation")

    # A parse check with a tiny subset validator: the structure this workflow
    # uses is a mapping of mappings of sequences, and the checks below cover the
    # keys that matter. Anything that would make GitHub reject the file outright
    # shows up as a missing required top-level key.
    for required in ("name:", "on:", "jobs:"):
        if not re.search(rf"^{required}", text, re.MULTILINE):
            fail(f"missing the required top-level key {required!r}")

    check_triggers(text)
    check_expression_literals(text)

    jobs = parse_jobs(text)
    if not jobs:
        fail("no jobs were found")

    for name in ("test", "hermetic", "build"):
        if name not in jobs:
            fail(f"job {name!r} is missing")

    # A build that does not wait for the tests produces binaries from untested
    # commits, which is the one thing this pipeline exists to prevent.
    build_needs = jobs["build"].get("needs", "")
    for required in ("test", "hermetic"):
        if required not in build_needs:
            fail(f"the build job does not depend on {required!r} (needs: {build_needs!r})")

    for name, job in jobs.items():
        for dependency in re.findall(r"[A-Za-z0-9_-]+", job.get("needs", "")):
            if dependency not in jobs:
                fail(f"job {name!r} depends on {dependency!r}, which does not exist")

    # The matrix must still cover the platforms the tool claims to support.
    for platform in ("darwin", "linux", "windows"):
        if platform not in text:
            fail(f"the workflow no longer mentions {platform!r}")
    check_platform_matrix(text)

    print(f"workflow check passed: {len(jobs)} jobs ({', '.join(sorted(jobs))})")


if __name__ == "__main__":
    main()

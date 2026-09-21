#!/usr/bin/env python3
"""Check the workflow files' shape.

A workflow that does not parse, or that names a job nobody runs, fails silently:
the pipeline simply stops reporting, and nobody notices until a release is wrong.
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
  - a release that no longer runs on tags, publishes without the tests having
    run first, drops a platform, or stops attaching the artifacts;
  - an unquoted word where an expression needs a string literal, which makes
    GitHub reject the whole file ("Unrecognized named-value: 'amd64'") and run
    no job at all.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

CI = Path(".github/workflows/ci.yml")
RELEASE = Path(".github/workflows/release.yml")

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

# The targets a release has to publish.
RELEASE_TARGETS = (
    "darwin/amd64",
    "darwin/arm64",
    "linux/amd64",
    "linux/arm64",
    "windows/amd64",
    "windows/arm64",
)


def fail(message: str) -> None:
    print("workflow check failed: " + message, file=sys.stderr)
    sys.exit(1)


def read(path: Path) -> str:
    if not path.is_file():
        fail(f"{path} does not exist")
    text = path.read_text(encoding="utf-8")
    if "\t" in text:
        fail(f"{path} contains a tab, which YAML does not allow for indentation")
    return text


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


def check_expression_literals(text: str, path: Path) -> None:
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
                        f"{path}:{number}: {value!r} is an unquoted word in an expression, "
                        f"so Actions reads it as a named value and rejects the whole file; "
                        f"write it as {value!r} in quotes"
                    )


def check_platform_matrix(text: str) -> None:
    """Every platform the tool ships for must be compiled and tested on itself."""
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

    # `go vet` is the compile gate on each platform: it type-checks every package
    # *and* its test files, which `go build` does not. Dropping it would leave the
    # matrix compiling nothing per platform, so it is asserted here.
    if not re.search(r"^\s*run:\s*go vet\b", block, re.MULTILINE):
        fail("the test job no longer runs `go vet`, which is what compiles every package and its tests on this platform")

    # The race build is the strict test run. A plain run on its own would let a
    # data race through, so the pipeline must ask for the detector explicitly.
    if not re.search(r"^\s*run:\s*go test -race\b", block, re.MULTILINE):
        fail("the test job no longer runs `go test -race`, which is the strict test run")


def check_hermetic_gates(text: str) -> None:
    """The checks the hermetic job exists for must survive a refactor."""
    block = job_block(text, "hermetic")
    if not block:
        fail("the hermetic job is missing")
    for needle, description in (
        ("check-workflow.py", "the workflow-shape check"),
        ("check-conventions.py", "the writing and structure conventions"),
        ("check-accounting.py", "the rewrite ledger of every test the tree contained"),
        ("check-orphans.py", "the orphan-package check"),
        ("hermetic-check.sh", "the throwaway-HOME run"),
        ("-coverprofile=", "the coverage profile of that run"),
        ("check-coverage.py", "the coverage gate"),
        ("--require internal/nodejs=100", "the coverage requirement on the Node resolution"),
        (r"grep -E '^[[:space:]]*--- SKIP'", "the unexpected-skip scan"),
        ("TestLsofNamesTheSocketThisProcessHolds", "the documented skip allow-list"),
    ):
        if needle not in block:
            fail(f"the hermetic job no longer runs {description} ({needle!r})")


def check_mutation_gate(text: str) -> None:
    """The mutation sweep has to keep running, and keep covering everything.

    It is the only gate that decides whether the tests would notice a broken
    decision, and it is the slowest one, so the temptation to drop it is real.
    The project keeps it off developers' machines, so a workflow that stops
    running it retires the gate without anybody noticing.

    It runs as a shard matrix, which buys wall clock at the price of a new
    failure mode: a shard list that covers four of six partitions, or a `--shard
    I/N` whose N disagrees with the matrix, silently drops mutations while every
    job stays green. Both are checked here, so the sharded sweep is provably the
    same gate as the serial one.
    """
    block = job_block(text, "mutation")
    if not block:
        fail("the mutation job is missing, so nothing breaks a decision to test the suite")
    # Comments in this job talk about the script, so the needle has to be looked
    # for in the commands: a job that documents the sweep and runs `echo` is a
    # retired gate with a convincing comment.
    commands = "\n".join(line for line in block.splitlines() if not line.lstrip().startswith("#"))
    if "mutation-check.py" not in commands:
        fail("the mutation job no longer runs scripts/mutation-check.py")
    if not re.search(r"^\s*timeout-minutes:\s*\d+\s*$", block, re.MULTILINE):
        fail("the mutation job has no timeout-minutes, so a hang would hold a runner for hours")
    if "needs:" in block:
        fail("the mutation job waits on another job, which puts the slowest gate on the critical path")

    shards = re.search(r"^\s+shard:\s*\[([^\]]*)\]", block, re.MULTILINE)
    if not shards:
        fail(
            "the mutation job has no shard matrix: the sweep must run in parallel shards, "
            "each covering one deterministic partition of scripts/mutation-check.py"
        )
    listed = [entry.strip() for entry in shards.group(1).split(",")]
    if not all(entry.isdigit() for entry in listed):
        fail(f"the mutation shard matrix is not a list of numbers (shard: {listed})")
    numbers = [int(entry) for entry in listed]
    if sorted(numbers) != list(range(1, len(numbers) + 1)):
        fail(
            f"the mutation shard matrix is not 1..N without gaps or repeats (shard: {listed}): "
            "a hole means some mutations never run"
        )
    # The index must come from the matrix, not from a literal: six copies of
    # `--shard 1/6` would be six green jobs covering one sixth of the sweep.
    argument = re.search(r"--shard\s+\$\{\{\s*matrix\.shard\s*\}\}/(\d+)", commands)
    if not argument:
        fail(
            "the mutation job does not run the sweep with `--shard ${{ matrix.shard }}/N`, so it "
            "is not partitioned across the shard matrix"
        )
    if int(argument.group(1)) != len(numbers):
        fail(
            f"the mutation job runs `--shard I/{argument.group(1)}` but the matrix has "
            f"{len(numbers)} shards, so some partitions are never executed"
        )


def check_toolchain_pins(text: str, path: Path) -> None:
    """The toolchain and the actions are pinned, not floating.

    Every gate in this repository is deterministic on purpose — the govulncheck
    version is pinned for exactly this reason — but a floating `1.25.x` picks a
    different compiler patch every week, so the same commit can pass in one week
    and fail in another without anything changing. A movable action tag has the
    same effect. Both are checked here; bumping either is a deliberate commit.
    """
    for number, line in enumerate(text.splitlines(), start=1):
        if line.lstrip().startswith("#"):
            continue
        version = re.search(r"go-version:\s*\"?([^\"\s]+)\"?", line)
        if version:
            value = version.group(1)
            if not re.fullmatch(r"\d+\.\d+\.\d+", value):
                fail(
                    f"{path}:{number}: go-version {value!r} is not an exact patch release; "
                    "write X.Y.Z so the same commit always builds with the same toolchain"
                )
        if re.search(r"check-latest:\s*true", line):
            fail(
                f"{path}:{number}: check-latest: true lets the toolchain move under a fixed "
                "go-version; the version is the pin"
            )
        uses = re.search(r"uses:\s*([^\s#]+)", line)
        if uses:
            reference = uses.group(1)
            if "@" not in reference:
                fail(f"{path}:{number}: {reference!r} has no version at all")
            revision = reference.rsplit("@", 1)[1]
            if not re.fullmatch(r"[0-9a-f]{40}", revision):
                fail(
                    f"{path}:{number}: {reference!r} rides a movable tag; pin the action to the "
                    "commit SHA and keep the human-readable version in a trailing comment"
                )


def check_floor_gate(text: str) -> None:
    """The minimum toolchain the README promises is the one CI exercises.

    `go vet` under the pinned toolchain compiles a call to a standard library
    function that did not exist in the version go.mod declares, so the promise
    is only checked by asking for the floor toolchain by name. The name is
    derived from go.mod, so bumping the module's requirement without moving this
    line fails here rather than in a user's terminal.
    """
    block = job_block(text, "floor")
    if not block:
        fail("the floor job is missing, so nothing verifies the minimum toolchain the README promises")
    module = Path("go.mod").read_text(encoding="utf-8")
    declared = re.search(r"^go\s+(\d+\.\d+)", module, re.MULTILINE)
    if not declared:
        fail("go.mod has no `go` directive, so the minimum toolchain is undefined")
    wanted = f"go{declared.group(1)}.0"
    if f"GOTOOLCHAIN: {wanted}" not in block:
        fail(
            f"the floor job does not run with GOTOOLCHAIN: {wanted}, the version go.mod "
            f"declares as the minimum (found: {[l.strip() for l in block.splitlines() if 'GOTOOLCHAIN' in l] or 'nothing'})"
        )
    if not re.search(r"^\s*(GOTOOLCHAIN=\S+\s+)?go (vet|build)\b", block, re.MULTILINE):
        fail("the floor job does not compile the tree with the floor toolchain")


def check_build_targets(text: str) -> None:
    """Every platform the tool ships for is still compiled by the pipeline."""
    for target in RELEASE_TARGETS:
        goos, goarch = target.split("/")
        includes = re.search(rf"\{{\s*goos:\s*{goos},\s*goarch:\s*{goarch}\s*\}}", text)
        pair = re.search(rf"\b{goos}/{goarch}\b", text)
        if not includes and not pair:
            fail(f"the build job no longer compiles {target}")


def check_makefile_parity(text: str) -> None:
    """What the Makefile defines as a gate is what CI runs.

    The Makefile is documented as the single entry point for the gates, but the
    workflow runs the commands inline — so the two can drift, and a gate that
    stays in `make ci` while disappearing from the pipeline is exactly the
    failure this whole file exists to prevent. Each gate command the Makefile
    defines must appear in ci.yml; `$(TEST_TIMEOUT)` is expanded with the
    Makefile's own default so the comparison is literal.
    """
    makefile = Path("Makefile").read_text(encoding="utf-8")
    default_timeout = re.search(r"^TEST_TIMEOUT\s*\?=\s*(\S+)", makefile, re.MULTILINE)
    timeout = default_timeout.group(1) if default_timeout else "600s"
    for target in ("vet", "test", "test-race"):
        recipe = re.search(rf"^{re.escape(target)}:\n((?:\t.*\n)+)", makefile, re.MULTILINE)
        if not recipe:
            fail(f"the Makefile no longer defines a `{target}` gate")
        for raw in recipe.group(1).splitlines():
            command = raw.strip().replace("$(TEST_TIMEOUT)", timeout)
            if command and command not in text:
                fail(
                    f"ci.yml does not run `{command}`, which the Makefile defines as the "
                    f"`{target}` gate: the pipeline and the documented entry point disagree"
                )


def check_ci(text: str) -> None:
    """The continuous integration workflow."""
    for required in ("name:", "on:", "jobs:"):
        if not re.search(rf"^{required}", text, re.MULTILINE):
            fail(f"{CI}: missing the required top-level key {required!r}")

    match = re.search(r"push:\s*\n\s+branches:\s*\[([^\]]*)\]", text)
    if not match:
        fail(f"{CI}: the push trigger no longer names the branches it runs on")
    branches = [entry.strip().strip("'\"") for entry in match.group(1).split(",")]
    if "main" not in branches:
        fail(f"{CI}: pushes to main no longer trigger the pipeline (push branches: {branches})")

    jobs = parse_jobs(text)
    if not jobs:
        fail(f"{CI}: no jobs were found")

    for name in ("test", "hermetic", "mutation", "floor", "build"):
        if name not in jobs:
            fail(f"{CI}: job {name!r} is missing")

    # A build that does not wait for the tests produces binaries from untested
    # commits, which is the one thing this pipeline exists to prevent.
    build_needs = jobs["build"].get("needs", "")
    for required in ("test", "hermetic"):
        if required not in build_needs:
            fail(f"{CI}: the build job does not depend on {required!r} (needs: {build_needs!r})")

    for name, job in jobs.items():
        for dependency in re.findall(r"[A-Za-z0-9_-]+", job.get("needs", "")):
            if dependency not in jobs:
                fail(f"{CI}: job {name!r} depends on {dependency!r}, which does not exist")

    # The matrix must still cover the platforms the tool claims to support.
    for platform in ("darwin", "linux", "windows"):
        if platform not in text:
            fail(f"{CI}: the workflow no longer mentions {platform!r}")
    check_platform_matrix(text)
    check_hermetic_gates(text)
    check_mutation_gate(text)
    check_floor_gate(text)
    check_build_targets(text)
    check_makefile_parity(text)

    return jobs


def check_release(text: str) -> dict[str, dict[str, str]]:
    """The release workflow: a tag publishes the six platform binaries."""
    for required in ("name:", "on:", "jobs:"):
        if not re.search(rf"^{required}", text, re.MULTILINE):
            fail(f"{RELEASE}: missing the required top-level key {required!r}")

    match = re.search(r"tags:\s*\[([^\]]*)\]", text)
    if not match:
        fail(f"{RELEASE}: no tag trigger, so a release would never run")
    patterns = [entry.strip().strip("'\"") for entry in match.group(1).split(",")]
    if not any(pattern.startswith("v") for pattern in patterns):
        fail(f"{RELEASE}: a version tag does not trigger a release (tags: {patterns})")

    if not re.search(r"^permissions:\s*\n\s+contents:\s*write\s*$", text, re.MULTILINE):
        fail(f"{RELEASE}: publishing needs `permissions: contents: write`")

    jobs = parse_jobs(text)
    if "release" not in jobs:
        fail(f"{RELEASE}: job 'release' is missing")

    # A release that does not wait for the tests publishes binaries from a commit
    # nobody verified, which is the one thing a release pipeline must not do.
    release_needs = jobs["release"].get("needs", "")
    if not release_needs.strip():
        fail(f"{RELEASE}: the release job does not depend on the verification job")
    for dependency in re.findall(r"[A-Za-z0-9_-]+", release_needs):
        if dependency not in jobs:
            fail(f"{RELEASE}: the release job depends on {dependency!r}, which does not exist")
        block = job_block(text, dependency)
        if not re.search(r"^\s*run:\s*go test\b", block, re.MULTILINE):
            fail(f"{RELEASE}: job {dependency!r} does not run the tests")

    # Every platform the tool supports has to appear as a build target, written
    # either as a "goos/goarch" pair or as the two matrix keys a few characters
    # apart. A looser match would accept a file that merely mentions the words
    # somewhere, which is how a dropped platform would slip through.
    for target in RELEASE_TARGETS:
        goos, goarch = target.split("/")
        pair = re.search(rf"\b{goos}/{goarch}\b", text)
        matrix = re.search(
            rf"goos['\"]?\s*[:=]\s*['\"]?{goos}['\"]?[,\s]{{1,40}}?goarch['\"]?\s*[:=]\s*['\"]?{goarch}\b",
            text,
        )
        if not pair and not matrix:
            fail(f"{RELEASE}: {target} is no longer built for the release")

    # Both spellings are load-bearing: the first tag of a release creates it, and
    # a re-run replaces the artifacts of the release that is already there.
    for command in ("gh release create", "gh release upload"):
        if command not in text:
            fail(f"{RELEASE}: {command!r} is gone, so a release can no longer be published or re-run")

    return jobs


def main() -> None:
    ci_text = read(CI)
    release_text = read(RELEASE)

    check_expression_literals(ci_text, CI)
    check_expression_literals(release_text, RELEASE)
    check_toolchain_pins(ci_text, CI)
    check_toolchain_pins(release_text, RELEASE)

    ci_jobs = check_ci(ci_text)
    release_jobs = check_release(release_text)

    print(
        f"workflow check passed: ci has {len(ci_jobs)} jobs ({', '.join(sorted(ci_jobs))}), "
        f"release has {len(release_jobs)} jobs ({', '.join(sorted(release_jobs))})"
    )


if __name__ == "__main__":
    main()

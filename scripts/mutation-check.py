#!/usr/bin/env python3
"""Mutate the Node model's implementation and check that the suite notices.

A passing test suite proves nothing on its own: a test that asserts the wrong
thing, or an assertion that was quietly relaxed, passes just as happily as a
correct one. This script answers the opposite question — "would the suite catch
this?" — by breaking one decision at a time and requiring the run to fail.

Every mutation is a literal replacement in a tracked source file. The file is
restored afterwards, whatever happens, and a mutation that leaves the tree
unchanged is reported as a bug in this script rather than as a passing check:
a mutation that never applied proves nothing.

Usage:
    python3 scripts/mutation-check.py [--list] [--only NAME] [-v]
"""

from __future__ import annotations

import argparse
import pathlib
import signal
import subprocess
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parent.parent

# Each entry is (name, path, original text, mutated text, packages to test).
# The text is matched literally, so a reformatting of the implementation makes
# this script fail loudly instead of silently stopping to test anything.
MUTATIONS: list[tuple[str, str, str, str, list[str]]] = [
    (
        "the floor is exclusive instead of inclusive",
        "internal/nodejs/nodejs.go",
        "if Compare(installation.Version, minimum) < 0 {",
        "if Compare(installation.Version, minimum) <= 0 {",
        ["./internal/nodejs/", "./internal/service/"],
    ),
    (
        "any other major version counts as verified",
        "internal/nodejs/nodejs.go",
        "if majorOf(installation.Version) != majorOf(tested) {",
        "if false {",
        ["./internal/nodejs/", "./internal/service/"],
    ),
    (
        "a refused release is no longer given a remedy",
        "internal/nodejs/nodejs.go",
        "Remedy: Remedies(minimum, tested),",
        'Remedy: "",',
        ["./internal/nodejs/"],
    ),
    (
        "the request is normalized away, so 'latest' means discovery",
        "internal/nodejs/nodejs.go",
        "requested := strings.TrimSpace(prefs.Version)",
        'requested := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prefs.Version), "v"))',
        ["./internal/nodejs/"],
    ),
    (
        "a release read from a binary is accepted without looking like one",
        "internal/nodejs/nodejs.go",
        "\tif version == \"\" || !startsWithDigit(version) {\n\t\treturn Installation{}, fmt.Errorf(",
        "\tif false {\n\t\treturn Installation{}, fmt.Errorf(",
        ["./internal/nodejs/"],
    ),
    (
        "the managers are consulted before the release request",
        "internal/nodejs/nodejs.go",
        "for _, candidate := range candidates {\n\t\tif Matches(candidate.version, requested) {",
        "for _, candidate := range candidates {\n\t\tif false {",
        ["./internal/nodejs/"],
    ),
    (
        "the real interpreter behind a forwarding entry is ignored",
        "internal/nodejs/nodejs.go",
        "if filepath.Dir(execPath) != installation.BinDir {",
        "if false {",
        ["./internal/nodejs/"],
    ),
    (
        "the probe is unbounded",
        "internal/nodejs/nodejs.go",
        "ctx, cancel := context.WithTimeout(ctx, probeTimeout)",
        "ctx, cancel := context.WithCancel(ctx)",
        ["./internal/nodejs/"],
    ),
    (
        "the --node flag is ignored",
        "internal/config/config.go",
        "switch {\n\tcase overrides.NodeVersion != nil && strings.TrimSpace(*overrides.NodeVersion) != \"\":",
        "switch {\n\tcase false:",
        ["./internal/config/", "./internal/service/"],
    ),
    (
        "the settings document outranks the environment for the release",
        "internal/config/config.go",
        "\tcase environment != \"\":\n\t\tsettings.NodeVersion = environment\n\t\tsources.NodeVersion = \"env\"\n\tcase configured != \"\":\n\t\tsettings.NodeVersion = configured\n\t\tsources.NodeVersion = \"file\"",
        "\tcase configured != \"\":\n\t\tsettings.NodeVersion = configured\n\t\tsources.NodeVersion = \"file\"\n\tcase environment != \"\":\n\t\tsettings.NodeVersion = environment\n\t\tsources.NodeVersion = \"env\"",
        ["./internal/config/", "./internal/service/"],
    ),
    (
        "the --config flag is ignored",
        "internal/config/config.go",
        "if override != nil && strings.TrimSpace(*override) != \"\" {",
        "if override != nil && false {",
        ["./internal/config/", "./internal/cli/"],
    ),
    (
        "an undetermined release is written into the document",
        "internal/config/config.go",
        "if version := strings.TrimSpace(settings.NodeVersion); version != \"\" {",
        "if version := strings.TrimSpace(settings.NodeVersion); true {",
        ["./internal/config/", "./internal/cli/"],
    ),
    (
        "the write-back replaces the operator's document with defaults",
        "internal/config/config.go",
        "\tif !found {\n\t\thome, err := paths.Home()",
        "\tif found {\n\t\thome, err := paths.Home()",
        ["./internal/config/"],
    ),
    (
        "the release is recorded even when the document names one",
        "internal/config/config.go",
        "writeNodeVersion := release != \"\" && document.NodeVersion == nil",
        "writeNodeVersion := release != \"\"",
        ["./internal/config/", "./internal/service/"],
    ),
    (
        "the checkout is recorded even when the document names one",
        "internal/config/config.go",
        "writeRepoDir := checkout != \"\" && !documentDecidesCheckout(document, guess)",
        "writeRepoDir := checkout != \"\"",
        ["./internal/config/", "./internal/service/"],
    ),
    (
        "a copy of the built-in guess counts as a decision",
        "internal/config/config.go",
        "\t\tif resolved != guess {\n\t\t\tsettings.ConfiguredRepoDir = resolved\n\t\t}",
        "\t\tsettings.ConfiguredRepoDir = resolved",
        ["./internal/config/", "./internal/service/", "./internal/cli/"],
    ),
    (
        "provisioning freezes the checkout guess into the document",
        "internal/config/config.go",
        "if settings.RepoDir != \"\" && settings.RepoDir != guessRepoDir {",
        "if settings.RepoDir != \"\" {",
        ["./internal/config/", "./internal/cli/"],
    ),
    (
        "a document that carries an unresolvable checkout is overwritten",
        "internal/config/config.go",
        "\tresolved, err := paths.Resolve(*document.RepoDir)\n\tif err != nil {\n\t\treturn true\n\t}\n\treturn resolved != guess",
        "\tresolved, err := paths.Resolve(*document.RepoDir)\n\tif err != nil {\n\t\treturn false\n\t}\n\treturn resolved != guess",
        ["./internal/config/"],
    ),
    (
        "a configured timeout is not range-checked before it becomes a duration",
        "internal/config/config.go",
        "\tif seconds > MaxTimeoutSeconds {\n\t\treturn 0, usagef(\"%s 不能超过 %d 秒: %d\", name, MaxTimeoutSeconds, seconds)\n\t}",
        "\tif false {\n\t\treturn 0, usagef(\"%s 不能超过 %d 秒: %d\", name, MaxTimeoutSeconds, seconds)\n\t}",
        ["./internal/config/"],
    ),
    (
        "an override of the configured release is not reported",
        "internal/service/start.go",
        "\ts.reportNodeOverride(installation)\n\ts.reportRepoOverride()",
        "\ts.reportRepoOverride()",
        ["./internal/service/"],
    ),
    (
        "an override of the configured checkout is not reported",
        "internal/service/start.go",
        "\ts.reportNodeOverride(installation)\n\ts.reportRepoOverride()",
        "\ts.reportNodeOverride(installation)",
        ["./internal/service/"],
    ),
    (
        "the environment that lost to the document is not reported",
        "internal/service/start.go",
        "if s.Settings.Sources.NodeVersion != \"env\" {\n\t\treturn\n\t}",
        "if s.Settings.Sources.NodeVersion == \"env\" {\n\t\treturn\n\t}",
        ["./internal/service/"],
    ),
    (
        "the environment that beat the document's checkout is not reported",
        "internal/service/start.go",
        "\tif s.Settings.Sources.RepoDir != \"env\" {\n\t\treturn\n\t}",
        "\tif s.Settings.Sources.RepoDir == \"env\" {\n\t\treturn\n\t}",
        ["./internal/service/"],
    ),
    (
        "the runtime is left out of the final record",
        "internal/service/start.go",
        "\t\tURL:         s.urlFromLog(ctx),\n\t\tNodeVersion: installation.Version,\n\t\tNodePath:    installation.NodePath,\n\t\tRepoDir:     s.Settings.RepoDir,\n\t}",
        "\t\tURL: s.urlFromLog(ctx),\n\t}",
        ["./internal/service/"],
    ),
    (
        "the checkout is left out of the final record",
        "internal/service/start.go",
        "\t\tNodePath:    installation.NodePath,\n\t\tRepoDir:     s.Settings.RepoDir,\n\t}\n\tif err := s.Record.Save(record); err != nil {",
        "\t\tNodePath:    installation.NodePath,\n\t}\n\tif err := s.Record.Save(record); err != nil {",
        ["./internal/service/"],
    ),
    (
        "the checkout is left out of the record written before the port answers",
        "internal/service/start.go",
        "\t\tNodePath:    installation.NodePath,\n\t\tRepoDir:     s.Settings.RepoDir,\n\t}); err != nil {",
        "\t\tNodePath:    installation.NodePath,\n\t}); err != nil {",
        ["./internal/service/"],
    ),
    (
        "an adopted survivor loses the checkout it was started from",
        "internal/service/start.go",
        "NodePath:    record.NodePath,\n\t\tRepoDir:     record.RepoDir,",
        "NodePath:    record.NodePath,",
        ["./internal/service/"],
    ),
    (
        "a start that finds the service running does not reconcile the document",
        "internal/service/start.go",
        "\ttarget := observed.record.RepoDir",
        "\ttarget := \"\"",
        ["./internal/service/"],
    ),
    (
        "a build does not record the checkout it built",
        "internal/service/build.go",
        "\ts.writeBack(s.Settings.RepoDir, \"\")\n\treturn nil\n}\n\n// RunUpdate",
        "\treturn nil\n}\n\n// RunUpdate",
        ["./internal/service/"],
    ),
    (
        "the running instance's checkout is not compared with the configuration",
        "internal/service/start.go",
        "\tif running == \"\" || running == s.Settings.RepoDir {\n\t\treturn\n\t}",
        "\tif running == \"\" || running != \"\" {\n\t\treturn\n\t}",
        ["./internal/service/"],
    ),
    (
        "a sibling server from another checkout blocks a build",
        "internal/service/build.go",
        "\t\tif record.RepoDir != \"\" && record.RepoDir != s.Settings.RepoDir {\n\t\t\tcontinue\n\t\t}",
        "\t\tif false {\n\t\t\tcontinue\n\t\t}",
        ["./internal/service/"],
    ),
    (
        "the record no longer names the checkout it was started from",
        "internal/state/state.go",
        "RepoDir string `json:\"repoDir,omitempty\"`",
        "RepoDir string `json:\"-\"`",
        ["./internal/state/", "./internal/service/"],
    ),
    (
        "an adopted survivor loses the runtime it runs",
        "internal/service/start.go",
        "NodeVersion: record.NodeVersion,\n\t\tNodePath:    record.NodePath,",
        "",
        ["./internal/service/"],
    ),
    (
        "the doctor row hides why a release is unverified",
        "internal/service/doctor.go",
        "add(\"Node\", CheckWarn, detail+\"；\"+verdict.Reason)",
        "add(\"Node\", CheckWarn, detail)",
        ["./internal/service/"],
    ),
    (
        "the record no longer names the release it was started with",
        "internal/state/state.go",
        "if r.NodeVersion != \"\" {\n\t\tbuilder.WriteString(\", node=\")\n\t\tbuilder.WriteString(r.NodeVersion)\n\t}",
        "",
        ["./internal/service/"],
    ),
]


def run(packages: list[str]) -> tuple[str, str]:
    """Run the given packages' tests and classify the outcome.

    Returns one of:
      "passed"  — the suite accepted the mutated program.
      "caught"  — a test failed, which is the evidence this script is after.
      "invalid" — the program no longer builds, or something else went wrong
                  before a test could run. That is not evidence: a mutation that
                  does not compile is not a decision the suite pinned.
    """
    result = subprocess.run(
        ["go", "test", "-count=1", *packages],
        cwd=ROOT,
        capture_output=True,
        text=True,
    )
    output = result.stdout + result.stderr
    if result.returncode == 0:
        return "passed", output
    if "Caches/go-build" in output and "operation not permitted" in output:
        # The toolchain could not write its build cache, so nothing was
        # compiled. Reporting that as a surviving mutation would blame the suite
        # for the environment.
        return "environment", output
    if "build failed" in output or "cannot use" in output or "[build failed]" in output:
        return "invalid", output
    if "--- FAIL" in output or "panic:" in output or "test timed out" in output:
        return "caught", output
    return "invalid", output


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--list", action="store_true", help="list the mutations and exit")
    parser.add_argument("--only", help="run just the mutation whose name contains this")
    parser.add_argument("-v", "--verbose", action="store_true", help="show the failing output")
    arguments = parser.parse_args()

    if arguments.list:
        for name, path, _, _, packages in MUTATIONS:
            print(f"{name}\n    {path} -> {', '.join(packages)}")
        return 0

    selected = [entry for entry in MUTATIONS if not arguments.only or arguments.only in entry[0]]
    if not selected:
        print(f"no mutation matches {arguments.only!r}", file=sys.stderr)
        return 2

    survivors: list[str] = []
    for name, path, original, mutated, packages in selected:
        target = ROOT / path
        text = target.read_text()
        if text.count(original) != 1:
            print(f"SKIP  {name}\n      {path}: the anchor appears {text.count(original)} times, not once", file=sys.stderr)
            survivors.append(name)
            continue
        target.write_text(text.replace(original, mutated, 1))
        # A mutation that is applied and never restored leaves the tree broken for
        # whoever runs next, and being killed mid-run is exactly when that
        # happens: a signal turns into SystemExit here, so the restore below
        # still runs.
        previous = {
            signal.SIGINT: signal.getsignal(signal.SIGINT),
            signal.SIGTERM: signal.getsignal(signal.SIGTERM),
        }
        signal.signal(signal.SIGINT, lambda *_: sys.exit(130))
        signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
        started = time.time()
        try:
            outcome, output = run(packages)
        finally:
            target.write_text(text)
            for received, handler in previous.items():
                signal.signal(received, handler)
        elapsed = time.time() - started
        failed = [line for line in output.splitlines() if line.startswith("--- FAIL")]
        if outcome == "passed":
            print(f"ALIVE   {name}  ({elapsed:.1f}s) — the suite did not notice")
            survivors.append(name)
        elif outcome == "caught":
            print(f"caught  {name}  ({elapsed:.1f}s, {len(failed)} failing tests)")
        elif outcome == "environment":
            print(f"BLOCKED {name}  ({elapsed:.1f}s) — the toolchain could not use its build cache; set GOCACHE", file=sys.stderr)
            survivors.append(name)
        else:
            print(f"INVALID {name}  ({elapsed:.1f}s) — the mutation did not compile, so it proves nothing", file=sys.stderr)
            survivors.append(name)
        if arguments.verbose:
            for line in failed[:5]:
                print(f"        {line}")
            if outcome != "caught":
                print("\n".join(output.splitlines()[:20]))

    if survivors:
        print(f"\n{len(survivors)} mutation(s) survived or were invalid: the suite does not pin them", file=sys.stderr)
        return 1
    print(f"\nall {len(selected)} mutations were caught")
    return 0


if __name__ == "__main__":
    sys.exit(main())

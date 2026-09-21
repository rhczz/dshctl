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
    python3 scripts/mutation-check.py [--list] [--only NAME] [--shard I/N] [-v]

The sweep is partitioned deterministically by index, so it can run as N parallel
CI jobs without either of them covering the same mutation or leaving one out.
"""

from __future__ import annotations

import argparse
import pathlib
import re
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
        "\tif !found {\n\t\tif s.Home == \"\" {",
        "\tif found {\n\t\tif s.Home == \"\" {",
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
        "\treturn resolved != guess\n}",
        "\treturn resolved == guess\n}",
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
        "the selection is sorted by number instead of by what the command is about",
        "internal/config/config.go",
        "return StateSelection{Ports: configuredFirst(ports, s.Port)}, nil",
        "sort.Ints(ports)\n\treturn StateSelection{Ports: configuredFirst(ports, ports[0])}, nil",
        ["./internal/config/", "./internal/cli/"],
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
        "\ts.writeBack(s.Settings.RepoDir, \"\")\n\treturn nil\n}",
        "\treturn nil\n}",
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
        "internal/domain/record.go",
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
        "internal/domain/record.go",
        "if r.NodeVersion != \"\" {\n\t\tbuilder.WriteString(\", node=\")\n\t\tbuilder.WriteString(r.NodeVersion)\n\t}",
        "",
        ["./internal/service/"],
    ),
    (
        "a fingerprint that could not be read is reported as a readable one",
        "internal/service/start.go",
        "\t\tif time.Now().After(deadline) {\n\t\t\treturn 0\n\t\t}",
        "\t\tif time.Now().After(deadline) {\n\t\t\treturn time.Now().Unix()\n\t\t}",
        ["./internal/service/"],
    ),
    (
        "following a log replays everything already in it",
        "internal/logfile/follow.go",
        "skipExisting = position == 0 && fileExists(path)",
        "skipExisting = false",
        ["./internal/logfile/"],
    ),
    (
        "the detached child stays in dshctl's session",
        "internal/detach/detach_unix.go",
        "return &syscall.SysProcAttr{Setsid: true}",
        "return &syscall.SysProcAttr{}",
        ["./internal/detach/"],
    ),
    (
        "the detachment attribute never reaches the kernel",
        "internal/detach/detach.go",
        "func Start(cmd *exec.Cmd) (*Process, error) {\n\tif err := cmd.Start(); err != nil {",
        "func Start(cmd *exec.Cmd) (*Process, error) {\n\tcmd.SysProcAttr = nil\n\tif err := cmd.Start(); err != nil {",
        ["./internal/detach/"],
    ),
    (
        "a permission refusal is read as the process being gone",
        "internal/host/ports_unix.go",
        "return err == nil || errors.Is(err, syscall.EPERM)",
        "return err == nil || errors.Is(err, syscall.EINTR)",
        ["./internal/host/"],
    ),
    (
        "probing whether a process exists ends it",
        "internal/host/ports_unix.go",
        "\terr = process.Signal(syscall.Signal(0))",
        "\terr = process.Kill()",
        ["./internal/host/"],
    ),
    (
        "a directory git still tracks is treated as residue",
        "internal/repo/prune.go",
        "\tif _, ok := tracked[path.Clean(relative)]; ok {\n\t\treturn Candidate{}, false\n\t}\n",
        "",
        ["./internal/repo/"],
    ),
    (
        "git's tracked paths are read in their quoted form",
        "internal/repo/prune.go",
        '\targs := []string{"-C", r.Dir, "ls-files", "-z", "--"}',
        '\targs := []string{"-C", r.Dir, "ls-files", "--"}',
        ["./internal/repo/"],
    ),
    (
        "a lock that was never won is reported as a generic failure",
        "internal/service/observe.go",
        "func withLockValue[T any](ctx context.Context, s *Service, fn func() (T, error)) (T, error) {\n\tvar zero T\n\theld, err := lock.Acquire(ctx, s.Settings.LockFile(), s.Settings.LockTimeout)\n\tif err != nil {\n\t\treturn zero, exitcode.Wrap(exitcode.LockTimeout, err)\n\t}",
        "func withLockValue[T any](ctx context.Context, s *Service, fn func() (T, error)) (T, error) {\n\tvar zero T\n\theld, err := lock.Acquire(ctx, s.Settings.LockFile(), s.Settings.LockTimeout)\n\tif err != nil {\n\t\treturn zero, exitcode.Wrap(exitcode.Failure, err)\n\t}",
        ["./internal/service/"],
    ),
    (
        "a multi-instance operation no longer serializes on the state directory",
        "internal/service/observe.go",
        "func stickyLock[T any](ctx context.Context, s *Service, fn func() (T, error)) (T, error) {\n\tvar zero T\n\theld, err := lock.Acquire(ctx, s.Settings.LockFile(), s.Settings.LockTimeout)\n\tif err != nil {\n\t\treturn zero, exitcode.Wrap(exitcode.LockTimeout, err)\n\t}\n\tdefer held.Release()",
        "func stickyLock[T any](ctx context.Context, s *Service, fn func() (T, error)) (T, error) {\n\tvar zero T",
        ["./internal/service/"],
    ),
    (
        "a bare stop covers only the configured instance",
        "internal/service/ports.go",
        "\t\tvar result StopAllResult\n\t\tfor _, port := range selection.Ports {",
        "\t\tvar result StopAllResult\n\t\tfor _, port := range selection.Ports[:1] {",
        ["./internal/service/"],
    ),
    (
        "the stop deadline is a hundred times more patient than the budget",
        "internal/service/observe.go",
        "func (s *Service) waitForStopped(ctx context.Context, timeout time.Duration) error {\n\tdeadline := time.Now().Add(timeout)",
        "func (s *Service) waitForStopped(ctx context.Context, timeout time.Duration) error {\n\tdeadline := time.Now().Add(100 * timeout)",
        ["./internal/service/"],
    ),
    (
        "the fingerprint absorbs no clock granularity",
        "internal/service/observe.go",
        "\tfingerprintTolerance = 5 * time.Second",
        "\tfingerprintTolerance = 0",
        ["./internal/service/"],
    ),
    (
        "the port fixture hands a role to a port it already used",
        "internal/service/fake_test.go",
        "\t\tif reservedPorts.handed[port] {\n\t\t\tcontinue\n\t\t}\n\t\tif reservedPorts.handed == nil {\n\t\t\treservedPorts.handed = map[int]bool{}\n\t\t}\n\t\treservedPorts.handed[port] = true\n\t\treturn port",
        "\t\treturn port",
        ["./internal/service/"],
    ),
    (
        "a cancelled run kills only the process it started",
        "internal/run/process_unix.go",
        "\t_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)",
        "\t_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)",
        ["./internal/run/"],
    ),
    (
        "a sibling manifest no longer protects a subtree from pruning",
        "internal/repo/prune.go",
        "\tif hasSiblingManifest(rootFS, relative) {\n\t\t// The parent ships its own package.json, so this directory is part of\n\t\t// the project rather than leftovers from a deleted package.\n\t\treturn Candidate{}, false\n\t}\n",
        "",
        ["./internal/repo/"],
    ),
    (
        "the version already deployed is rebuilt and restarted anyway",
        "internal/service/update.go",
        "\tif target.Commit == current {",
        "\tif false {",
        ["./internal/service/"],
    ),
    (
        "a worktree with tracked changes is switched anyway",
        "internal/service/update.go",
        "\t} else if dirty {",
        "\t} else if false && dirty {",
        ["./internal/service/"],
    ),
    (
        "returning to a visited position no longer truncates the stack",
        "internal/history/history.go",
        "\t\tif at <= index {",
        "\t\tif at <= index && false {",
        ["./internal/history/", "./internal/service/"],
    ),
    (
        "a local branch name resolves as a version",
        "internal/repo/release.go",
        "\t\tif strings.HasPrefix(strings.TrimSpace(name), \"refs/heads/\") {",
        "\t\tif false && strings.HasPrefix(strings.TrimSpace(name), \"refs/heads/\") {",
        ["./internal/repo/", "./internal/service/"],
    ),
    (
        "a no-op update forgets the checkout it ran against",
        "internal/service/update.go",
        "\t\t// A no-op is still a successful run against this checkout, and the\n\t\t// document records the checkout a successful run used.\n\t\ts.writeBack(s.Settings.RepoDir, \"\")\n\t\treturn nil\n\t}",
        "\t\treturn nil\n\t}",
        ["./internal/service/"],
    ),
    (
        "latest is fetched without an origin",
        "internal/service/update.go",
        "\t\tif !hasOrigin {",
        "\t\tif false && !hasOrigin {",
        ["./internal/service/"],
    ),
    (
        "a position without a timestamp is accepted",
        "internal/history/history.go",
        "\t\t\tif record.At <= 0 {",
        "\t\t\tif false && record.At <= 0 {",
        ["./internal/history/"],
    ),
    (
        "fetch prunes remote-tracking refs the remote no longer has",
        "internal/repo/release.go",
        '"fetch", originRemote, "--tags"',
        '"fetch", originRemote, "--tags", "--prune"',
        ["./internal/repo/"],
    ),
    (
        "a sha selector is repeated in the target line",
        "internal/service/update.go",
        "\tif strings.HasPrefix(commit, request.target) {",
        "\tif false && strings.HasPrefix(commit, request.target) {",
        ["./internal/service/"],
    ),
    (
        "HEAD is refused as a local branch",
        "internal/repo/release.go",
        "\tif selector != \"HEAD\" {",
        "\tif true {",
        ["./internal/repo/"],
    ),
    (
        "a language nobody speaks is rendered instead of falling back to English",
        "internal/i18n/i18n.go",
        "\t// \"C\" and \"POSIX\" are the absence of a locale, not a language.\n\treturn EN",
        "\t// \"C\" and \"POSIX\" are the absence of a locale, not a language.\n\treturn ZH",
        ["./internal/i18n/"],
    ),
    (
        "two layers may claim the same message id",
        "internal/i18n/catalog.go",
        "\t\t\tif _, exists := merged[id]; exists {",
        "\t\t\tif _, exists := merged[id]; false && exists {",
        ["./internal/i18n/"],
    ),
    (
        "the product trusts netstat before the tools that name the owner",
        "internal/service/hosttools.go",
        '\tPort:    []string{"lsof", "ss", "netstat"},',
        '\tPort:    []string{"netstat", "ss", "lsof"},',
        ["./internal/host/", "./internal/service/"],
    ),
    (
        "the record store forgets the bound the product gave it",
        "internal/service/records.go",
        "\t\tMaxBytes: maxRecordBytes,",
        "\t\tMaxBytes: 0,",
        ["./internal/service/"],
    ),
    (
        "the log markers stop naming the product that wrote them",
        "internal/service/logformat.go",
        '\tProduct: "dshctl",',
        '\tProduct: "",',
        ["./internal/service/", "./internal/conformance/"],
    ),
    (
        "a failed mutating run is reported as a success in its document",
        "internal/cli/jsonmode.go",
        "\tdocument := mutatingDocument{Command: name, OK: err == nil, Result: result, Events: events.list()}",
        "\tdocument := mutatingDocument{Command: name, OK: true, Result: result, Events: events.list()}",
        ["./internal/cli/"],
    ),
    (
        "the log level flag is ignored, so only the file and environment decide",
        "internal/cli/cli.go",
        "\tif parsed.logLevelSet {",
        "\tif false {",
        ["./internal/cli/"],
    ),
    (
        "the toolchain that built the binary is left unnamed",
        "internal/version/version.go",
        "\treturn info.GoVersion, info.Main.Path",
        '\treturn "", info.Main.Path',
        ["./internal/version/"],
    ),
    (
        "a failed fetch still reports the timeline as confirmed",
        "internal/cli/commands.go",
        "\tif !report.Fetched {",
        "\tif false {",
        ["./internal/cli/"],
    ),
]


def run(packages: list[str]) -> tuple[str, str]:
    """Run the given packages' tests and classify the outcome.

    Returns one of:
      "passed"  — the suite accepted the mutated program.
      "caught"  — the suite went red, which is the evidence this script is after.
                  A test binary that dies by signal counts: it did not survive
                  the mutation, and only a build failure proves nothing.
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
    # A build failure is decided first: its output also carries FAIL lines, and
    # a mutation that never compiled proves nothing about the suite.
    if "build failed" in output or "cannot use" in output or "[build failed]" in output:
        return "invalid", output
    if "[setup failed]" in output or "no Go files" in output or "matched no packages" in output:
        # A package that does not exist fails `go test` instantly and prints
        # FAIL. Reading that as "the suite noticed" would turn every entry whose
        # package moved into a false caught, so it is invalid: nothing ran.
        return "invalid", output
    if "--- FAIL" in output or "panic:" in output or "test timed out" in output:
        return "caught", output
    if re.search(r"^FAIL\s+\S+", output, re.MULTILINE) or "signal: " in output:
        # The suite failed without a per-test line: the test binary itself was
        # killed, or it aborted. A mutation like "probing a process ends it"
        # takes the whole binary down with the test that observes it, and that
        # is the suite noticing — classifying it as invalid would report a
        # pinned decision as unpinned.
        return "caught", output
    return "invalid", output


def audit() -> int:
    """Check every anchor against the tree without running a test.

    A stale anchor is the signature of moved code: the sharded sweep catches it
    one shard per run, which is a slow way to learn that a file was renamed. This
    is the same check for the whole list at once.
    """
    stale = []
    for name, path, original, _, _ in MUTATIONS:
        file = ROOT / path
        if not file.exists():
            stale.append(f"{name}: {path} 不存在")
            continue
        if file.read_text(encoding="utf-8").count(original) != 1:
            stale.append(f"{name}: {path} 里的锚点不唯一或不存在")
    if stale:
        for entry in stale:
            print(f"检查失败: {entry}", file=sys.stderr)
        return 1
    print(f"锚点检查通过（{len(MUTATIONS)} 条都唯一匹配）")
    return 0


def select(entries: list, only: str | None, shard: str | None) -> list:
    """Return the mutations this invocation owns, in list order.

    The partition is index modulo the shard count: every entry belongs to exactly
    one shard, the union of all shards is the whole list, and adding an entry
    never moves an existing one to a different shard. That is what makes a sharded
    sweep the same gate as a serial one rather than a sample of it.
    """
    selected = [entry for entry in entries if not only or only in entry[0]]
    if not selected:
        print(f"no mutation matches {only!r}", file=sys.stderr)
        sys.exit(2)
    if shard is None:
        return selected
    try:
        index, count = shard.split("/", 1)
        index, count = int(index), int(count)
    except ValueError:
        print(f"--shard wants I/N, not {shard!r}", file=sys.stderr)
        sys.exit(2)
    if count < 1 or not 1 <= index <= count:
        print(f"--shard {shard!r} is out of range: I must be within 1..N", file=sys.stderr)
        sys.exit(2)
    return [entry for position, entry in enumerate(selected) if position % count == index - 1]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--list", action="store_true", help="list the mutations and exit")
    parser.add_argument("--only", help="run just the mutation whose name contains this")
    parser.add_argument("--audit", action="store_true", help="check every anchor against the tree")
    parser.add_argument(
        "--shard",
        help="run one deterministic partition of the sweep, written I/N (CI runs all N in parallel)",
    )
    parser.add_argument("-v", "--verbose", action="store_true", help="show the failing output")
    arguments = parser.parse_args()

    if arguments.audit:
        return audit()

    if arguments.list:
        for name, path, _, _, packages in MUTATIONS:
            print(f"{name}\n    {path} -> {', '.join(packages)}")
        return 0

    if arguments.only and arguments.shard:
        # The two answer different questions ("does this one decision hold?" and
        # "which part of the sweep am I?"), and combining them would make a
        # command whose coverage depends on arithmetic nobody reads back.
        print("--only and --shard are alternatives, not a combination", file=sys.stderr)
        return 2

    selected = select(MUTATIONS, arguments.only, arguments.shard)
    if arguments.shard:
        print(f"shard {arguments.shard}: {len(selected)} of {len(MUTATIONS)} mutations")

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
    scope = f" in shard {arguments.shard}" if arguments.shard else ""
    print(f"\nall {len(selected)} mutations{scope} were caught")
    return 0


if __name__ == "__main__":
    sys.exit(main())

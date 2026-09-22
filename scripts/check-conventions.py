#!/usr/bin/env python3
"""Check the repository's writing, structure, and dependency conventions.

Some of these rules live in prose only — AGENTS.md and the project skills state
them, and review is what keeps them true. The ones that can be decided
mechanically live here instead, as a check that fails rather than a sentence
somebody has to remember.

Every threshold is set from the repository's current state, so enabling this
check turns a green tree red for no reason: a gate that starts red is a gate
that gets switched off. When a rule is genuinely wrong for a real case, narrow
the rule and record why here; do not delete it.

Usage:
    check-conventions.py [--list]
"""

from __future__ import annotations

import argparse
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent

# Build output and VCS metadata are not sources; nothing here is ours to check.
SKIP_DIRS = {".git", "bin", "dist", "node_modules", ".pnpm-store"}

# A Go comment carries a sentence, not a paragraph. Measured at HEAD: the widest
# comment is 83 columns and none reaches 88, so 88 is a ceiling that only a new
# outlier trips.
MAX_COMMENT_COLUMNS = 88

# Standing instructions are read in full on every session; the skills are read
# on demand and keep their long tables in references/.
MAX_AGENTS_BYTES = 8192
MAX_SKILL_BYTES = 8192

# A reference is a lookup table, not a second skill: past this size it is really
# a document that should be split.
MAX_REFERENCE_BYTES = 16384

# The session catalog renders only name and description, and truncates the
# description at 500 characters — a description longer than that is invisible
# text.
MAX_DESCRIPTION_CHARS = 500

KEBAB = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")

# The exact frontmatter keys the local skill provider understands.
SKILL_KEYS = {
    "name",
    "description",
    "whenToUse",
    "metadata",
    "user-invocable",
    "disable-model-invocation",
}

# Each of these must appear in AGENTS.md, because a rule that is not there is
# not in every session's context.
AGENTS_SECTIONS = (
    "固定 vs 配置",
    "TDD",
    "风格",
    "架构",
    "工作流",
    "完成定义",
    "Skill 路由",
)

# The internal import graph, measured with `go list`. An edge that is not listed
# here is a new dependency, and a new dependency between layers is a design
# decision rather than an implementation detail.
#
# The vertical order the front-ends see:
#
#   cmd/dshctl -> internal/cli -> internal/service -> internal/service?  (see below)
#
# Today the engine and the commands share internal/service, and the model and the
# message catalog are the leaves every layer may use:
#
#   internal/domain  (the model: states, records, positions; no imports at all)
#   internal/i18n    (the message catalog; no imports at all)
#   infrastructure   (host, run, detach, lock, logfile, state, repo, nodejs,
#                     config, paths, exitcode, version, atomically, logging)
LAYERS = {
    "cmd/dshctl": {"internal/cli"},
    "internal/cli": {
        "internal/config",
        "internal/domain",
        "internal/exitcode",
        "internal/history",
        "internal/atomically",
        "internal/detach",
        "internal/history",
        "internal/host",
        "internal/i18n",
        "internal/logging",
        "internal/paths",
        "internal/run",
        "internal/lock",
        "internal/logfile",
        "internal/nodejs",
        "internal/state",
        "internal/repo",
        "internal/run",
        "internal/service",
        "internal/version",
    },
    "internal/service": {
        "internal/config",
        "internal/detach",
        "internal/domain",
        "internal/exitcode",
        "internal/history",
        "internal/host",
        "internal/atomically",
        "internal/detach",
        "internal/history",
        "internal/host",
        "internal/i18n",
        "internal/logging",
        "internal/paths",
        "internal/run",
        "internal/lock",
        "internal/logfile",
        "internal/logfile",
        "internal/logging",
        "internal/nodejs",
        "internal/paths",
        "internal/repo",
        "internal/run",
        "internal/state",
        "internal/version",
    },
    # Leaf packages may carry a message catalog: i18n is a capability leaf, and
    # a package's words belong to the package that speaks them.
    "internal/atomically": {"internal/i18n"},
    "internal/detach": {"internal/i18n"},
    "internal/host": {"internal/i18n"},
    "internal/logfile": {"internal/i18n"},
    "internal/logging": {"internal/i18n", "internal/logfile"},
    "internal/paths": {"internal/i18n"},
    "internal/run": {"internal/i18n"},
    "internal/config": {
        "internal/atomically",
        "internal/exitcode",
        "internal/i18n",
        "internal/logging",
        "internal/paths",
    },
    "internal/state": {"internal/atomically", "internal/i18n"},
    "internal/history": {"internal/atomically", "internal/i18n", "internal/state"},
    "internal/nodejs": {"internal/i18n", "internal/paths", "internal/run"},
    "internal/lock": {"internal/i18n"},
    "internal/repo": {"internal/i18n", "internal/run"},
    "internal/version": {"internal/buildinfo"},
    # Every remaining package is a leaf: it may import the standard library and
    # nothing else of ours.
}

MODULE = "github.com/rhczz/dshctl/"

# Files whose text form matters: a missing or doubled final newline shows up as
# noise in every later diff.
TEXT_SUFFIXES = {".go", ".md", ".mod", ".py", ".sh", ".yml", ".yaml", ".json", ".txt"}
TEXT_NAMES = {"Makefile", ".gitignore", "LICENSE"}

NOTE_NAME = re.compile(r"^\d{4}-\d{2}-\d{2}-[a-z0-9]+(?:-[a-z0-9]+)*\.md$")
NOTE_SECTIONS = ("## 问题", "## 决定", "## 备选方案", "## 后果", "## 验证")
NOTE_SKIP = {"README.md", "_template.md"}

FENCE = re.compile(r"```.*?```", re.DOTALL)
LINK = re.compile(r"\]\(([^)]+)\)")
CODE_SPAN = re.compile(r"`([^`\n]+)`")
REMOTE_LINK = re.compile(r"^(?:[a-z][a-z0-9+.-]*:|#|//)", re.IGNORECASE)

# A path in a code span is as much a promise as a link target: `references/x.md`
# and `../../../Makefile` both tell the reader where to look next, and a skill
# that points at a file nobody wrote is worse than one that says nothing.
PATH_LIKE = re.compile(r"^(?:references/|\.{2}/)")

# A constant that names an environment variable, as internal/paths declares them:
# the declaration and the literal an operator types.
ENV_CONSTANT = re.compile(r"^\s*(Env[A-Za-z0-9]+)\s*=\s*\"([^\"]+)\"", re.MULTILINE)

# Generated files that must never enter the history: they change with the
# toolchain that produced them, so every unrelated commit would carry noise, and
# a byte-code cache records the absolute paths of the machine that wrote it.
TRACKED_ARTIFACTS = (
    re.compile(r"(^|/)__pycache__/"),
    re.compile(r"\.pyc$"),
    re.compile(r"^bin/"),
    re.compile(r"^dist/"),
    re.compile(r"\.test$"),
)

RULES: dict[str, str] = {
    "agents-md": "AGENTS.md 存在、不超过预算、带每个必写小节，且 Skill 路由表与 skill 目录一一对应",
    "skills": "每个 SKILL.md 的 frontmatter、命名、描述长度、体积与相对链接",
    "notes": "每条决策记录的头部、必备小节与文件名",
    "go-comments": f"Go 注释不超过 {MAX_COMMENT_COLUMNS} 列",
    "go-forbidden": "非测试 Go 文件里没有 panic 与 init",
    "go-imports": "零第三方依赖，且内部 import 只沿允许的层方向",
    "readme-env": "internal/paths 里的每个环境变量都被 README 记录",
    "package-map": "internal/ 下的每个包都出现在 AGENTS.md 的包地图里",
    "tracked-artifacts": "被 git 跟踪的文件里没有生成的产物（bin/、dist/、__pycache__、*.pyc、*.test）",
    "trailing-newline": "文本文件恰好以一个换行结尾",
}


def fail(rule: str, message: str) -> str:
    """Render one problem the caller prints and counts."""
    return f"{rule}: {message}"


def source_files(suffix: str) -> list[pathlib.Path]:
    """Return every source file with this suffix, in a stable order."""
    found: list[pathlib.Path] = []
    for path in ROOT.rglob(f"*{suffix}"):
        if any(part in SKIP_DIRS for part in path.relative_to(ROOT).parts):
            continue
        found.append(path)
    return sorted(found)


def read(path: pathlib.Path) -> str:
    return path.read_text(encoding="utf-8")


def rel(path: pathlib.Path) -> str:
    return path.relative_to(ROOT).as_posix()


def is_test(path: pathlib.Path) -> bool:
    return path.name.endswith("_test.go")


def check_agents_md() -> list[str]:
    """AGENTS.md is the standing orders: presence, budget, required sections, routing."""
    problems: list[str] = []
    path = ROOT / "AGENTS.md"
    if not path.is_file():
        return [fail("agents-md", "AGENTS.md 不存在")]
    text = read(path)
    size = len(text.encode("utf-8"))
    if size > MAX_AGENTS_BYTES:
        problems.append(
            fail("agents-md", f"AGENTS.md 有 {size} 字节，超过预算 {MAX_AGENTS_BYTES}；把细节移到 skill")
        )
    for section in AGENTS_SECTIONS:
        if section not in text:
            problems.append(fail("agents-md", f"缺少必写小节或字样: {section}"))
    # The routing table is how a skill ever gets loaded: a bundle nobody routes to
    # is dead weight, and a row pointing at a missing bundle is a dangling promise.
    skills_root = ROOT / ".agents" / "skills"
    if skills_root.is_dir():
        bundles = {path.name for path in skills_root.iterdir() if path.is_dir()}
        mentioned = set(re.findall(r"`(dshctl-[a-z0-9-]+)`", text))
        for name in sorted(bundles - mentioned):
            problems.append(fail("agents-md", f"skill {name} 没有出现在 Skill 路由表里"))
        for name in sorted(mentioned - bundles):
            problems.append(fail("agents-md", f"路由表提到 {name}，但 .agents/skills 下没有它"))
    return problems


def parse_frontmatter(text: str, where: str) -> tuple[dict[str, str], str]:
    """Return the frontmatter mapping and the body of one SKILL.md."""
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        raise ValueError(f"{where}: 缺少 frontmatter 起始 ---")
    data: dict[str, str] = {}
    for index, line in enumerate(lines[1:], start=2):
        if line.strip() == "---":
            return data, "\n".join(lines[index:])
        if not line.strip():
            continue
        key, separator, value = line.partition(":")
        if not separator:
            raise ValueError(f"{where}:{index}: frontmatter 行不是 key: value")
        key = key.strip()
        if key not in SKILL_KEYS:
            raise ValueError(f"{where}:{index}: 未知 frontmatter 键 {key!r}")
        if key in data:
            raise ValueError(f"{where}:{index}: frontmatter 键 {key!r} 重复")
        data[key] = value.strip()
    raise ValueError(f"{where}: frontmatter 没有结束 ---")


def relative_links(body: str) -> list[str]:
    """Return the local markdown targets in a body, ignoring code fences."""
    targets: list[str] = []
    for match in LINK.finditer(FENCE.sub("", body)):
        target = match.group(1).strip()
        if not target or REMOTE_LINK.match(target) or target.startswith("<"):
            continue
        targets.append(target.split("#", 1)[0])
    return [target for target in targets if target]


def relative_code_paths(body: str) -> list[str]:
    """Return the local paths a body names inside code spans."""
    paths: list[str] = []
    for match in CODE_SPAN.finditer(FENCE.sub("", body)):
        span = match.group(1).strip()
        if PATH_LIKE.match(span):
            paths.append(span)
    return paths


def check_skills() -> list[str]:
    """Every skill bundle: valid routing metadata, honest links, bounded size."""
    problems: list[str] = []
    root = ROOT / ".agents" / "skills"
    if not root.is_dir():
        return [fail("skills", ".agents/skills 不存在")]
    bundles = sorted(path for path in root.iterdir() if path.is_dir())
    if not bundles:
        return [fail("skills", ".agents/skills 下没有任何 skill")]
    for bundle in bundles:
        path = bundle / "SKILL.md"
        where = rel(path)
        if not path.is_file():
            problems.append(fail("skills", f"{where} 不存在"))
            continue
        text = read(path)
        size = len(text.encode("utf-8"))
        if size > MAX_SKILL_BYTES:
            problems.append(
                fail("skills", f"{where} 有 {size} 字节，超过预算 {MAX_SKILL_BYTES}；长表移到 references/")
            )
        try:
            data, body = parse_frontmatter(text, where)
        except ValueError as error:
            problems.append(fail("skills", str(error)))
            continue
        name = data.get("name", "")
        description = data.get("description", "")
        if not body.strip():
            problems.append(fail("skills", f"{where}: 正文为空，skill 加载后没有任何内容"))
        if name != bundle.name:
            problems.append(fail("skills", f"{where}: name {name!r} 与目录名 {bundle.name!r} 不一致"))
        if not KEBAB.match(name):
            problems.append(fail("skills", f"{where}: name {name!r} 不是 kebab-case"))
        if not description:
            problems.append(fail("skills", f"{where}: 缺少 description"))
        elif len(description) > MAX_DESCRIPTION_CHARS:
            problems.append(
                fail(
                    "skills",
                    f"{where}: description 有 {len(description)} 字符，超过目录渲染上限 {MAX_DESCRIPTION_CHARS}",
                )
            )
        for target in dict.fromkeys(relative_links(body)):
            if not (bundle / target).exists():
                problems.append(fail("skills", f"{where}: 相对链接指向不存在的 {target}"))
        for target in dict.fromkeys(relative_code_paths(body)):
            if not (bundle / target).exists():
                problems.append(fail("skills", f"{where}: 正文提到的 {target} 不存在"))
        problems.extend(check_bundle_documents(bundle))
    return problems


def check_bundle_documents(bundle: pathlib.Path) -> list[str]:
    """References carry links too, and they resolve from their own directory."""
    problems: list[str] = []
    for path in sorted(bundle.rglob("*.md")):
        if path.name == "SKILL.md":
            continue
        where = rel(path)
        text = read(path)
        size = len(text.encode("utf-8"))
        if size > MAX_REFERENCE_BYTES:
            problems.append(
                fail("skills", f"{where} 有 {size} 字节，超过预算 {MAX_REFERENCE_BYTES}；拆成多个 references")
            )
        for target in dict.fromkeys(relative_links(text) + relative_code_paths(text)):
            if not (path.parent / target).exists():
                problems.append(fail("skills", f"{where}: 指向不存在的 {target}"))
    return problems


def check_notes() -> list[str]:
    """Decision records: the header block, the mandatory sections, the filename."""
    problems: list[str] = []
    root = ROOT / ".agents" / "notes"
    if not root.is_dir():
        return [fail("notes", ".agents/notes 不存在")]
    for path in sorted(root.glob("*.md")):
        if path.name in NOTE_SKIP:
            continue
        where = rel(path)
        if not NOTE_NAME.match(path.name):
            problems.append(fail("notes", f"{where}: 文件名应为 yyyy-mm-dd-kebab-case.md"))
        text = read(path)
        if not text.startswith("# 决策: "):
            problems.append(fail("notes", f"{where}: 第一行应为 '# 决策: <标题>'"))
        status = re.search(r"^状态: (.*)$", text, re.MULTILINE)
        if not status:
            problems.append(fail("notes", f"{where}: 缺少 '状态:' 行"))
        else:
            value = status.group(1).strip()
            if value != "已实施" and not value.startswith("已否决 — "):
                problems.append(fail("notes", f"{where}: 状态取值只能是 '已实施' 或 '已否决 — 原因'"))
        for section in NOTE_SECTIONS:
            if section not in text:
                problems.append(fail("notes", f"{where}: 缺少小节 {section}"))
    return problems


def check_go_comments() -> list[str]:
    """A comment states one contract; a wrapped paragraph belongs in a doc."""
    problems: list[str] = []
    for path in source_files(".go"):
        if is_test(path):
            continue
        for number, line in enumerate(read(path).splitlines(), start=1):
            if line.lstrip().startswith("//") and len(line) > MAX_COMMENT_COLUMNS:
                problems.append(
                    fail("go-comments", f"{rel(path)}:{number}: 注释 {len(line)} 列，超过 {MAX_COMMENT_COLUMNS}")
                )
    return problems


def check_go_forbidden() -> list[str]:
    """panic ends a service the operator asked to manage; init hides an order."""
    problems: list[str] = []
    for path in source_files(".go"):
        if is_test(path):
            continue
        for number, line in enumerate(read(path).splitlines(), start=1):
            if line.lstrip().startswith("//"):
                continue
            if re.search(r"\bpanic\(", line):
                problems.append(fail("go-forbidden", f"{rel(path)}:{number}: 出现 panic"))
            if re.match(r"^func init\(\)", line):
                problems.append(fail("go-forbidden", f"{rel(path)}:{number}: 出现 init"))
    return problems


def imports_of(text: str) -> list[str]:
    """Return every import path in one Go source file."""
    paths: list[str] = []
    in_block = False
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("import ("):
            in_block = True
            rest = stripped[len("import ("):]
            if rest.endswith(")"):
                in_block = False
                rest = rest[:-1]
            paths.extend(re.findall(r'"([^"]+)"', rest))
            continue
        if in_block:
            if stripped.startswith(")"):
                in_block = False
            else:
                paths.extend(re.findall(r'"([^"]+)"', stripped))
            continue
        if stripped.startswith("import "):
            paths.extend(re.findall(r'"([^"]+)"', stripped))
    return paths


def check_go_imports() -> list[str]:
    """The module stays standard-library only, and layers only point one way."""
    problems: list[str] = []
    for path in source_files(".go"):
        package = rel(path).rsplit("/", 1)[0]
        allowed = LAYERS.get(package, set())
        for imported in imports_of(read(path)):
            if imported.startswith(MODULE):
                if is_test(path):
                    # Tests may reach across layers to build fixtures; the layer
                    # gate is about production edges. The third-party gate below
                    # still applies to tests.
                    continue
                target = imported[len(MODULE):]
                if target not in allowed:
                    problems.append(
                        fail("go-imports", f"{rel(path)}: {package} 依赖 {target}，不在允许的层方向内")
                    )
                continue
            if "." in imported.split("/", 1)[0]:
                problems.append(fail("go-imports", f"{rel(path)}: 引入第三方模块 {imported}"))
    # The zero-dependency promise is a go.mod fact too: a require row is the only
    # way a third-party import (test or not) can even compile.
    mod = ROOT / "go.mod"
    if mod.is_file():
        for number, line in enumerate(read(mod).splitlines(), start=1):
            if re.match(r"^require(?: |\(|$)", line):
                problems.append(fail("go-imports", f"go.mod:{number}: 出现 require，违反零第三方依赖"))
    return problems


def check_readme_env() -> list[str]:
    """Every environment variable the tool reads is in the operator contract.

    The Go test that guards the README iterates a hand-copied list, so a new
    `paths.Env*` constant would work, stay undocumented, and leave the test green:
    the person who needs the documentation most is the one who cannot read the
    source to find it. The list is derived from the source here instead.
    """
    problems: list[str] = []
    readme = ROOT / "README.md"
    if not readme.is_file():
        return [fail("readme-env", "README.md 不存在")]
    documented = read(readme)
    for path in sorted((ROOT / "internal" / "paths").glob("*.go")):
        if is_test(path):
            continue
        for name, value in ENV_CONSTANT.findall(read(path)):
            if value not in documented:
                problems.append(
                    fail("readme-env", f"{rel(path)}: 环境变量 {name}={value} 没有被 README 记录")
                )
    return problems


def check_package_map() -> list[str]:
    """Every package in the tree appears in the map an agent reads first.

    The map is where a new package's reason to exist is stated, and nothing else
    compares it with the tree: a package can appear under `internal/` and stay
    invisible to every reader of AGENTS.md while each individual gate stays
    green.
    """
    problems: list[str] = []
    agents = ROOT / "AGENTS.md"
    if not agents.is_file():
        return [fail("package-map", "AGENTS.md 不存在")]
    text = read(agents)
    internal = ROOT / "internal"
    if not internal.is_dir():
        return problems
    for directory in sorted(path for path in internal.iterdir() if path.is_dir()):
        if not any(directory.glob("*.go")):
            continue
        if f"`{directory.name}`" not in text and f"internal/{directory.name}" not in text:
            problems.append(
                fail("package-map", f"internal/{directory.name} 没有出现在 AGENTS.md 的包地图里")
            )
    return problems


def check_tracked_artifacts() -> list[str]:
    """No generated file is part of the history.

    .gitignore only stops a file from being *added*; one that was added before
    the rule stays tracked and keeps rewriting itself, so the promise "no build
    output in the repository" is checked against the index rather than assumed.
    The check needs git, which is not optional here: the Makefile stamps the
    version from `git describe`, so a tree without git cannot be built anyway.
    """
    problems: list[str] = []
    result = subprocess.run(
        ["git", "ls-files"], cwd=ROOT, capture_output=True, text=True
    )
    if result.returncode != 0:
        return [fail("tracked-artifacts", "git ls-files 失败，无法确认生成的产物没有被跟踪")]
    for path in result.stdout.splitlines():
        for pattern in TRACKED_ARTIFACTS:
            if pattern.search(path):
                problems.append(
                    fail(
                        "tracked-artifacts",
                        f"{path} 是被跟踪的生成产物: 用 `git rm --cached` 移出索引并写进 .gitignore",
                    )
                )
                break
    return problems


def check_trailing_newline() -> list[str]:
    """Exactly one final newline, or every later diff carries a stray blank line."""
    problems: list[str] = []
    for path in sorted(ROOT.rglob("*")):
        if not path.is_file():
            continue
        if any(part in SKIP_DIRS for part in path.relative_to(ROOT).parts):
            continue
        if path.suffix not in TEXT_SUFFIXES and path.name not in TEXT_NAMES:
            continue
        text = read(path)
        if not text:
            problems.append(fail("trailing-newline", f"{rel(path)} 是空文件"))
        elif not text.endswith("\n"):
            problems.append(fail("trailing-newline", f"{rel(path)} 结尾没有换行"))
        elif text.endswith("\n\n"):
            problems.append(fail("trailing-newline", f"{rel(path)} 结尾有多个换行"))
    return problems


CHECKS = {
    "agents-md": check_agents_md,
    "skills": check_skills,
    "notes": check_notes,
    "go-comments": check_go_comments,
    "go-forbidden": check_go_forbidden,
    "go-imports": check_go_imports,
    "readme-env": check_readme_env,
    "package-map": check_package_map,
    "tracked-artifacts": check_tracked_artifacts,
    "trailing-newline": check_trailing_newline,
}


def main() -> int:
    parser = argparse.ArgumentParser(description="检查本仓库的书写、结构与依赖约定")
    parser.add_argument("--list", action="store_true", help="只列出规则，不执行检查")
    arguments = parser.parse_args()
    if arguments.list:
        for name in CHECKS:
            print(f"{name}\n    {RULES[name]}")
        return 0
    problems: list[str] = []
    for name, check in CHECKS.items():
        problems.extend(check())
    if problems:
        for problem in problems:
            print(f"检查失败: {problem}", file=sys.stderr)
        print(f"检查失败: 共 {len(problems)} 项；规则见 --list", file=sys.stderr)
        return 1
    print(f"约定检查通过（{len(CHECKS)} 条规则）")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

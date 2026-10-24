#!/usr/bin/env python3
"""Format editable source without touching generated code or runtime data."""

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
PACKAGE = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))
GOLINES_VERSION = PACKAGE["config"]["golinesVersion"]
PRETTIER_VERSION = PACKAGE["devDependencies"]["prettier"]
SHFMT_VERSION = PACKAGE["config"]["shfmtVersion"]
BLACK_VERSION = PACKAGE["config"]["blackVersion"]
GOLINES_FLAGS = [
    "--base-formatter=gofmt",
    "--max-len=120",
    "--no-reformat-tags",
    "--no-shorten-comments",
    "--no-keep-annotations",
]


def run(command, **kwargs):
    return subprocess.run(command, cwd=ROOT, check=True, **kwargs)


def tool(name, override, local):
    candidate = os.environ.get(override) or str(local)
    executable = shutil.which(candidate)
    if executable is None and override not in os.environ:
        executable = shutil.which(name)
    if executable is None:
        raise RuntimeError(f"Missing {name}; run `make format-tools` first.")
    return executable


def install():
    """Install pinned development tools; never modify the application's go.mod."""
    run(["npm", "ci", "--ignore-scripts", "--no-audit", "--no-fund"])
    destination = ROOT / ".local" / "tools" / "bin"
    destination.mkdir(parents=True, exist_ok=True)
    environment = dict(os.environ, GOBIN=str(destination), GOWORK="off")
    for module, version in (
        ("github.com/segmentio/golines", GOLINES_VERSION),
        ("mvdan.cc/sh/v3/cmd/shfmt", SHFMT_VERSION),
    ):
        run(["go", "install", f"{module}@{version}"], env=environment)
    python_tools = ROOT / ".local" / "tools" / "python"
    run([sys.executable, "-m", "venv", str(python_tools)])
    run(
        [
            str(python_tools / "bin/python"),
            "-m",
            "pip",
            "install",
            "--disable-pip-version-check",
            f"black=={BLACK_VERSION}",
        ]
    )
    print("Installed pinned development formatters.")


def verify_go_module(executable, module, version):
    # `go install` builds may report "vdev"; module build metadata is authoritative.
    metadata = run(["go", "version", "-m", executable], capture_output=True, text=True).stdout
    modules = [line.split() for line in metadata.splitlines()]
    expected = ["mod", module, version]
    if not any(fields[:3] == expected for fields in modules):
        raise RuntimeError(f"Expected {module} {version}; run `make format-tools`.")


def verify_versions(golines, prettier, shfmt, black):
    verify_go_module(golines, "github.com/segmentio/golines", GOLINES_VERSION)
    verify_go_module(shfmt, "mvdan.cc/sh/v3", SHFMT_VERSION)
    version = run([prettier, "--version"], capture_output=True, text=True).stdout.strip()
    if version != PRETTIER_VERSION:
        raise RuntimeError(f"Expected Prettier {PRETTIER_VERSION}; run `make format-tools`.")
    black_version = run([black, "--version"], capture_output=True, text=True).stdout.split()
    if black_version[:2] != ["black,", BLACK_VERSION]:
        raise RuntimeError(f"Expected Black {BLACK_VERSION}; run `make format-tools`.")


def source_files():
    go_files = sorted(
        path.relative_to(ROOT).as_posix()
        for directory in ("api", "cmd", "internal", "services", "tests")
        for path in (ROOT / directory).rglob("*.go")
        if not path.name.endswith(".pb.go")
    )
    web_files = sorted(
        path.relative_to(ROOT).as_posix()
        for directory, pattern in (
            ("web/dist", "*.js"),
            ("web/dist", "*.css"),
            ("web/tests", "*.cjs"),
            ("tests", "*.cjs"),
        )
        for path in (ROOT / directory).glob(pattern)
        if path.name != "constants.js"
    )
    web_files.extend([".prettierrc.json", "package.json", "package-lock.json"])
    python_files = sorted(
        path.relative_to(ROOT).as_posix()
        for directory in (".", "scripts", "deploy", "tests")
        for path in (ROOT / directory).glob("*.py")
    )
    shell_files = sorted(
        path.relative_to(ROOT).as_posix()
        for directory in (".", "scripts", "deploy/mysql")
        for pattern in ("*.sh", "*.command")
        for path in (ROOT / directory).glob(pattern)
    )
    return go_files, web_files, python_files, shell_files


def format_source(check):
    golines = tool("golines", "LUMO_GOLINES", ROOT / ".local/tools/bin/golines")
    prettier = tool("prettier", "LUMO_PRETTIER", ROOT / "node_modules/.bin/prettier")
    shfmt = tool("shfmt", "LUMO_SHFMT", ROOT / ".local/tools/bin/shfmt")
    black = tool("black", "LUMO_BLACK", ROOT / ".local/tools/python/bin/black")
    verify_versions(golines, prettier, shfmt, black)
    go_files, web_files, python_files, shell_files = source_files()
    mode = "--list-files" if check else "--write-output"
    formatted = run([golines, *GOLINES_FLAGS, mode, *go_files], capture_output=True, text=True)
    pending = formatted.stdout.strip()
    if pending:
        print(pending)
    if not check:
        # Nested long expressions may need more than one shortening pass.
        for _ in range(5):
            remaining = run(
                [golines, *GOLINES_FLAGS, "--list-files", *go_files], capture_output=True, text=True
            ).stdout.strip()
            if not remaining:
                break
            run([golines, *GOLINES_FLAGS, "--write-output", *go_files])
        else:
            raise RuntimeError("Go formatter did not converge after five passes.")
    result = 1 if check and pending else 0
    commands = [
        [prettier, "--check" if check else "--write", *web_files],
        [black, *(["--check", "--diff"] if check else []), *python_files],
        [shfmt, "-ln", "posix", "-i", "2", "-ci", "-d" if check else "-w", *shell_files],
    ]
    for command in commands:
        status = subprocess.run(command, cwd=ROOT).returncode
        result = status or result
    if check and pending:
        print("Go formatting differs; run `make format`.", file=sys.stderr)
    action = "Checked" if check else "Formatted"
    print(
        f"{action} {len(go_files)} Go, {len(web_files)} JS/CSS/config, "
        f"{len(python_files)} Python and {len(shell_files)} Shell files."
    )
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--check", action="store_true", help="Check only; do not write source files")
    mode.add_argument("--install", action="store_true", help="Install pinned development tools")
    arguments = parser.parse_args()
    if arguments.install:
        install()
        return 0
    return format_source(arguments.check)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Formatting failed: {error}", file=sys.stderr)
        if isinstance(error, subprocess.CalledProcessError) and error.stderr:
            print(error.stderr, file=sys.stderr)
        sys.exit(1)

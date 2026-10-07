"""Select, update, and commit verified Go bindings for the Actions workflow."""

from __future__ import annotations

import argparse
import os
import re
import subprocess
from pathlib import Path

import yaml


def git(root: Path, *args: str) -> str:
    return subprocess.run(
        ["git", *args], cwd=root, check=True, capture_output=True, text=True
    ).stdout


def select_targets(catalog: Path, requested: str) -> list[tuple[str, str, str]]:
    if requested.strip() == "all":
        requested = ""
    packages = yaml.safe_load(catalog.read_text(encoding="utf-8"))["packages"]
    targets = {}
    for key, package in packages.items():
        if not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", key):
            raise ValueError(f"invalid package key: {key}")
        config = package["languages"].get("go")
        if config is None:
            continue
        directory = f"bindings/{key}/go"
        if config["sourceDirectory"] != directory:
            raise ValueError(f"{key}:go: unexpected source directory")
        version = str(config["languageVersion"])
        if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
            raise ValueError(f"{key}:go: invalid Go version")
        targets[f"{key}:go"] = (f"{key}:go", directory, version)
    keys = (
        [item.strip() for item in requested.split(",")]
        if requested.strip()
        else sorted(targets)
    )
    if not keys or len(set(keys)) != len(keys):
        raise ValueError("select at least one Go target without duplicates")
    for key in keys:
        if key not in targets:
            raise ValueError(
                f"unknown or disabled target: {key}; this workflow enables Go only"
            )
    selected = [targets[key] for key in sorted(keys)]
    if len({target[2] for target in selected}) != 1:
        raise ValueError("one generation run requires one exact Go version")
    return selected


def summarize(message: str) -> None:
    print(message)
    if path := os.environ.get("GITHUB_STEP_SUMMARY"):
        with Path(path).open("a", encoding="utf-8") as stream:
            stream.write(message + "\n\n")


def update(
    root: Path, targets: list[tuple[str, str, str]], rc: Path, core: Path
) -> None:
    tooling = root / "tooling" / "extension-bindings"
    args = [
        str(rc.resolve()),
        "bindings",
        "update",
        "--packages",
        str(tooling / "packages.yaml"),
        "--toolchain-lock",
        str(tooling / "toolchain.lock.yaml"),
        "--core-schema",
        str(core.resolve()),
    ]
    for key, _, _ in targets:
        args.extend(["--target", key])
    # update verifies all candidates before atomically replacing generated source.
    subprocess.run(args, cwd=root, check=True)
    summarize(f"Verified and synchronized {len(targets)} Go binding targets.")


def commit(root: Path, targets: list[tuple[str, str, str]], source_commit: str) -> bool:
    if not re.fullmatch(r"[0-9a-f]{40}", source_commit):
        raise ValueError("a full source commit is required")
    if git(root, "rev-parse", "HEAD").strip() != source_commit:
        raise ValueError("checkout moved after generation started")
    if git(root, "diff", "--cached", "--name-only", "-z"):
        raise ValueError("index must be empty before staging generated files")
    changed = set(
        filter(
            None,
            (
                git(root, "diff", "--name-only", "--no-renames", "-z", "HEAD")
                + git(root, "ls-files", "--others", "--exclude-standard", "-z")
            ).split("\0"),
        )
    )
    directories = [directory for _, directory, _ in targets]
    unexpected = sorted(
        path
        for path in changed
        if not any(path.startswith(directory + "/") for directory in directories)
    )
    if unexpected:
        raise ValueError(
            f"refusing to commit changes outside selected bindings: {unexpected}"
        )
    if not changed:
        summarize("Go bindings are current; no commit was created.")
        return False
    remote = git(root, "ls-remote", "origin", "refs/heads/main").split()
    if not remote or remote[0] != source_commit:
        raise ValueError("main moved during generation; rerun against its current head")
    git(root, "add", "-A", "--", *directories)
    git(
        root,
        "-c",
        "user.name=github-actions[bot]",
        "-c",
        "user.email=41898282+github-actions[bot]@users.noreply.github.com",
        "commit",
        "-m",
        "chore(bindings): regenerate Go bindings",
        "-m",
        f"Generated and verified from {source_commit}.",
    )
    # A concurrent push also fails Git's fast-forward check. Never rebase or force.
    git(root, "push", "origin", "HEAD:refs/heads/main")
    generated_commit = git(root, "rev-parse", "HEAD").strip()
    summarize(f"Committed verified Go bindings to main: `{generated_commit}`.")
    return True


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["configure", "update", "commit"])
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--targets", default="")
    parser.add_argument("--rc", type=Path)
    parser.add_argument("--core-schema", type=Path)
    parser.add_argument("--source-commit", default="")
    args = parser.parse_args()
    root = args.root.resolve()
    try:
        targets = select_targets(
            root / "tooling/extension-bindings/packages.yaml", args.targets
        )
        if args.command == "configure":
            if output := os.environ.get("GITHUB_OUTPUT"):
                with Path(output).open("a", encoding="utf-8") as stream:
                    stream.write(f"go-version={targets[0][2]}\n")
                    stream.write(
                        f"source-commit={git(root, 'rev-parse', 'HEAD').strip()}\n"
                    )
            summarize(
                "Selected Go targets: " + ", ".join(target[0] for target in targets)
            )
        elif args.command == "update":
            if args.rc is None or args.core_schema is None:
                raise ValueError("update requires --rc and --core-schema")
            update(root, targets, args.rc, args.core_schema)
        else:
            commit(root, targets, args.source_commit)
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        if isinstance(error, subprocess.CalledProcessError) and error.stderr:
            print(error.stderr)
        parser.exit(1, f"binding generation failed: {error}\n")


if __name__ == "__main__":
    main()

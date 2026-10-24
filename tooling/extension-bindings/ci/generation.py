"""Shared target selection and commit-back for the generate-<language>.py scripts."""

from __future__ import annotations

import argparse
import os
import re
import subprocess
from pathlib import Path

import yaml

LANGUAGES = {"go": "Go", "python": "Python", "typescript": "TypeScript", "rust": "Rust"}

Target = tuple[str, str, str]


def git(root: Path, *args: str) -> str:
    return subprocess.run(
        ["git", *args], cwd=root, check=True, capture_output=True, text=True
    ).stdout


def language_of(target: Target) -> str:
    return target[0].rsplit(":", 1)[1]


def of_language(targets: list[Target], language: str) -> list[Target]:
    return [target for target in targets if language_of(target) == language]


def exact_version(targets: list[Target], language: str) -> str:
    versions = {target[2] for target in of_language(targets, language)}
    return versions.pop() if versions else ""


def join_labels(labels: list[str]) -> str:
    if len(labels) < 3:
        return " and ".join(labels)
    return ", ".join(labels[:-1]) + " and " + labels[-1]


def describe(targets: list[Target]) -> str:
    return join_labels(
        [
            label
            for language, label in LANGUAGES.items()
            if of_language(targets, language)
        ]
    )


def load_targets(
    catalog: Path, languages: tuple[str, ...] = tuple(LANGUAGES)
) -> dict[str, Target]:
    packages = yaml.safe_load(catalog.read_text(encoding="utf-8"))["packages"]
    targets = {}
    for key, package in packages.items():
        if not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", key):
            raise ValueError(f"invalid package key: {key}")
        for language in languages:
            config = package["languages"].get(language)
            if config is None:
                continue
            directory = f"bindings/{key}/{language}"
            if config["sourceDirectory"] != directory:
                raise ValueError(f"{key}:{language}: unexpected source directory")
            version = str(config["languageVersion"])
            if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
                raise ValueError(
                    f"{key}:{language}: invalid {LANGUAGES[language]} version"
                )
            targets[f"{key}:{language}"] = (f"{key}:{language}", directory, version)
    return targets


def select_targets(
    catalog: Path, requested: str, languages: tuple[str, ...] = tuple(LANGUAGES)
) -> list[Target]:
    if requested.strip() == "all":
        requested = ""
    targets = load_targets(catalog, languages)
    keys = (
        [item.strip() for item in requested.split(",")]
        if requested.strip()
        else sorted(targets)
    )
    if not keys or len(set(keys)) != len(keys):
        raise ValueError("select at least one binding target without duplicates")
    enabled = join_labels([LANGUAGES[language] for language in languages])
    for key in keys:
        if key not in targets:
            raise ValueError(
                f"unknown or disabled target: {key}; this workflow enables {enabled} only"
            )
    selected = [targets[key] for key in sorted(keys)]
    for language in languages:
        versions = {target[2] for target in of_language(selected, language)}
        if len(versions) > 1:
            raise ValueError(
                f"one generation run requires one exact {LANGUAGES[language]} version"
            )
    return selected


def summarize(message: str) -> None:
    print(message)
    if path := os.environ.get("GITHUB_STEP_SUMMARY"):
        with Path(path).open("a", encoding="utf-8") as stream:
            stream.write(message + "\n\n")


def write_outputs(**outputs: str) -> None:
    if path := os.environ.get("GITHUB_OUTPUT"):
        with Path(path).open("a", encoding="utf-8") as stream:
            for name, value in outputs.items():
                stream.write(f"{name.replace('_', '-')}={value}\n")


def commit(root: Path, targets: list[Target], source_commit: str) -> bool:
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
        summarize(f"{describe(targets)} bindings are current; no commit was created.")
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
        f"chore(bindings): regenerate {describe(targets)} bindings",
        "-m",
        f"Generated and verified from {source_commit}.",
    )
    git(root, "push", "origin", "HEAD:refs/heads/main")
    generated_commit = git(root, "rev-parse", "HEAD").strip()
    summarize(
        f"Committed verified {describe(targets)} bindings to main: `{generated_commit}`."
    )
    return True


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["configure", "commit"])
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--targets", default="")
    parser.add_argument("--source-commit", default="")
    args = parser.parse_args()
    root = args.root.resolve()
    try:
        targets = select_targets(
            root / "tooling/extension-bindings/packages.yaml", args.targets
        )
        if args.command == "configure":
            write_outputs(
                targets=",".join(target[0] for target in targets),
                source_commit=git(root, "rev-parse", "HEAD").strip(),
            )
            summarize(
                f"Selected {describe(targets)} targets: "
                + ", ".join(target[0] for target in targets)
            )
        else:
            commit(root, targets, args.source_commit)
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        if isinstance(error, subprocess.CalledProcessError) and error.stderr:
            print(error.stderr)
        parser.exit(1, f"binding generation failed: {error}\n")


if __name__ == "__main__":
    main()

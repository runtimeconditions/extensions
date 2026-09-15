#!/usr/bin/env python3
"""Inventory historical authoritative Smithy model changes without inventing semantic approval."""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
from pathlib import Path
from typing import Any, Optional

from serialization import read_document, write_yaml


def git(root: Path, *arguments: str, binary: bool = False) -> Any:
    completed = subprocess.run(
        ["git", "-C", str(root), *arguments],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=not binary,
    )
    if completed.returncode != 0:
        error = completed.stderr.decode() if binary else completed.stderr
        raise ValueError(error.strip() or f"git {' '.join(arguments)} failed")
    return completed.stdout


def operation_names(model: dict[str, Any], service_shape: str) -> list[str]:
    service = model.get("shapes", {}).get(service_shape)
    if not service or service.get("type") != "service":
        raise ValueError(f"model does not contain service {service_shape}")
    return sorted(item["target"].rsplit("#", 1)[-1] for item in service.get("operations", []))


def fingerprint(names: list[str]) -> str:
    return hashlib.sha256(("\n".join(names) + "\n").encode()).hexdigest()


def render_report(manifest: dict[str, Any], observations: list[dict[str, Any]]) -> str:
    inventory_changes = sum(item["change"] == "operation-set-change" for item in observations)
    distinct = len({item["operationNamesSha256"] for item in observations})
    lines = [
        f"# Historical Smithy inventory: {manifest['id']}",
        "",
        f"The authoritative repository contains {len(observations)} commits that changed the selected service model, producing {distinct} distinct operation inventories and {inventory_changes} operation-set transitions after the first observed model.",
        "",
        "An operation-set transition is a required extension-semantic review point, not proof that every model-only change is automatically safe. Shape-level semantic classification is performed by the extension compiler against the reviewed Service Operations Semantic Bridge.",
        "",
        "| Date | Commit | Operations | Inventory change | Added | Removed | Fingerprint |",
        "| --- | --- | ---: | --- | --- | --- | --- |",
    ]
    for item in observations:
        added = ", ".join(item["added"]) or "none"
        removed = ", ".join(item["removed"]) or "none"
        lines.append(f"| {item['date'][:10]} | `{item['commit'][:12]}` | {item['operationCount']} | {item['change']} | {added} | {removed} | `{item['operationNamesSha256'][:12]}` |")
    lines.append("")
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--models-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    manifest = read_document(args.manifest)
    source = manifest["source"]
    relative_path = source["path"]
    commits = [item for item in git(args.models_root, "log", "--reverse", "--format=%H", "--", relative_path).splitlines() if item]
    observations = []
    previous: Optional[set[str]] = None
    for commit in commits:
        raw = git(args.models_root, "show", f"{commit}:{relative_path}", binary=True)
        model = json.loads(raw)
        names = operation_names(model, source["serviceShape"])
        current = set(names)
        if previous is None:
            change = "initial"
            added: list[str] = []
            removed: list[str] = []
        else:
            added = sorted(current - previous)
            removed = sorted(previous - current)
            change = "operation-set-change" if added or removed else "model-only-change"
        observations.append(
            {
                "commit": commit,
                "date": git(args.models_root, "show", "-s", "--format=%cI", commit).strip(),
                "modelSha256": hashlib.sha256(raw).hexdigest(),
                "operationCount": len(names),
                "operationNamesSha256": fingerprint(names),
                "change": change,
                "added": added,
                "removed": removed,
            }
        )
        previous = current
    args.output.mkdir(parents=True, exist_ok=True)
    write_yaml(args.output / "history.yaml", {"schemaVersion": 1, "experiment": manifest["id"], "observations": observations})
    (args.output / "history.md").write_text(render_report(manifest, observations), encoding="utf-8")
    print(f"historical model commits: {len(observations)}")
    print(f"distinct operation inventories: {len({item['operationNamesSha256'] for item in observations})}")
    print(f"report: {args.output / 'history.md'}")


if __name__ == "__main__":
    main()

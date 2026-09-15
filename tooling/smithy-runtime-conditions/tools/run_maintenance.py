#!/usr/bin/env python3
"""Run one reproducible Runtime Conditions extension-maintenance observation."""

from __future__ import annotations

import argparse
import hashlib
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from serialization import read_document, write_yaml


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git_output(root: Path, *arguments: str) -> str:
    completed = subprocess.run(
        ["git", "-C", str(root), *arguments],
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if completed.returncode != 0:
        raise ValueError(completed.stderr.strip() or f"git {' '.join(arguments)} failed")
    return completed.stdout.strip()


def source_revision(models_root: Path, relative_path: str) -> str:
    return git_output(models_root, "log", "-1", "--format=%H", "--", relative_path)


def operation_inventory(model: dict[str, Any], service_shape: str) -> list[str]:
    service = model.get("shapes", {}).get(service_shape)
    if not service or service.get("type") != "service":
        raise ValueError(f"source model does not contain service {service_shape}")
    return sorted(item["target"].rsplit("#", 1)[-1] for item in service.get("operations", []))


def operation_fingerprint(names: list[str]) -> str:
    return hashlib.sha256(("\n".join(names) + "\n").encode()).hexdigest()


def same_file(first: Path, second: Path) -> bool:
    return first.exists() and second.exists() and first.read_bytes() == second.read_bytes()


def semantic_coordinates(path: Path) -> tuple[Any, Any]:
    if not path.exists():
        return None, None
    mapping = read_document(path)
    metadata = mapping.get("metadata", {})
    extension = mapping.get("extension", {})
    return metadata.get("semanticSha256"), extension.get("semanticSha256")


def render_summary(result: dict[str, Any]) -> str:
    lines = [
        f"# Extension maintenance: {result['experiment']}",
        "",
        f"**Classification: `{result['classification']}`**",
        "",
        result["message"],
        "",
        "## Authoritative model",
        "",
        f"- Repository: `{result['source']['repository']}`",
        f"- Revision: `{result['source']['revision']}`",
        f"- Path: `{result['source']['path']}`",
        f"- SHA-256: `{result['source']['sha256']}`",
        f"- Operations: {result['source']['operationCount']}",
        f"- Operation-name SHA-256: `{result['source']['operationNamesSha256']}`",
        "",
        "## Generated artifacts",
        "",
        f"- Extension semantics match accepted release: {result['artifacts']['extensionSemanticsMatchAccepted']}",
        f"- Service-mapping semantics match accepted release: {result['artifacts']['serviceMappingSemanticsMatchAccepted']}",
        f"- Candidate bytes match accepted artifacts: {result['artifacts']['extensionMatchesAccepted'] and result['artifacts']['serviceMappingMatchesAccepted']}",
        f"- Compiler exit code: {result['compiler']['exitCode']}",
        "",
        "The review surface is the Service Operations Semantic Bridge and focused semantic report. Generated YAML is machine output.",
        "",
    ]
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--models-root", type=Path, required=True)
    parser.add_argument("--extensions-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--observation-only", action="store_true")
    args = parser.parse_args()

    manifest = read_document(args.manifest)
    models_root = args.models_root.resolve()
    extensions_root = args.extensions_root.resolve()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    source = manifest["source"]
    source_path = source["path"]
    model_path = models_root / source_path
    revision = source_revision(models_root, source_path)
    names = operation_inventory(read_document(model_path), source["serviceShape"])
    candidate_root = output / "candidate"
    extension_candidate = candidate_root / "runtimeconditions.extension.yaml"
    service_mapping_candidate = candidate_root / "runtimeconditions.service-mapping.yaml"
    compiler_review = output / "compiler-review.md"
    compiler = extensions_root / "tooling/smithy-runtime-conditions/tools/compile_extension.py"
    command = [
        sys.executable,
        str(compiler),
        "--model",
        str(model_path),
        "--bridge",
        str(extensions_root / manifest["semanticBridge"]),
    ]
    command.extend(
        [
            "--service-shape",
            source["serviceShape"],
            "--source-repository",
            source["repository"],
            "--source-revision",
            revision,
            "--source-path",
            source_path,
            "--extension-output",
            str(extension_candidate),
            "--service-mapping-output",
            str(service_mapping_candidate),
            "--review-output",
            str(compiler_review),
            "--baseline-service-mapping",
            str(extensions_root / manifest["accepted"]["serviceMapping"]),
        ]
    )
    completed = subprocess.run(command, check=False, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    accepted_extension = extensions_root / manifest["accepted"]["extension"]
    accepted_mapping = extensions_root / manifest["accepted"]["serviceMapping"]
    extension_matches = same_file(extension_candidate, accepted_extension)
    mapping_matches = same_file(service_mapping_candidate, accepted_mapping)
    candidate_mapping_digest, candidate_extension_digest = semantic_coordinates(service_mapping_candidate)
    accepted_mapping_digest, accepted_extension_digest = semantic_coordinates(accepted_mapping)
    mapping_semantics_match = candidate_mapping_digest == accepted_mapping_digest and candidate_mapping_digest is not None
    extension_semantics_match = candidate_extension_digest == accepted_extension_digest and candidate_extension_digest is not None
    if completed.returncode == 2:
        classification = "extension-review-required"
        message = "The authoritative operation inventory no longer matches the reviewed Service Operations Semantic Bridge. Extension stakeholders must review the focused model difference before publishing a new extension release."
    elif completed.returncode != 0:
        classification = "invalid"
        message = "The maintenance automation failed before it could classify authoritative API semantics safely."
    elif args.observation_only or (mapping_semantics_match and extension_semantics_match):
        classification = "automatic"
        message = "The authoritative model remains semantically compatible with the accepted immutable extension release. Provenance-only model changes do not require a new extension release."
    else:
        classification = "extension-review-required"
        message = "The authoritative model is covered by the semantic bridge, but its generated extension semantics differ from the accepted immutable release. Extension stakeholders must review and publish a new release before SDK mappings target the change."
    result = {
        "schemaVersion": 1,
        "experiment": manifest["id"],
        "completedAt": utc_now(),
        "classification": classification,
        "message": message,
        "source": {
            "repository": source["repository"],
            "revision": revision,
            "path": source_path,
            "sha256": sha256(model_path),
            "operationCount": len(names),
            "operationNamesSha256": operation_fingerprint(names),
        },
        "compiler": {
            "exitCode": completed.returncode,
            "stdout": completed.stdout,
            "stderr": completed.stderr,
            "review": str(compiler_review),
        },
        "artifacts": {
            "extensionMatchesAccepted": extension_matches,
            "serviceMappingMatchesAccepted": mapping_matches,
            "extensionSemanticsMatchAccepted": extension_semantics_match,
            "serviceMappingSemanticsMatchAccepted": mapping_semantics_match,
            "acceptedExtensionSemanticSha256": accepted_extension_digest,
            "candidateExtensionSemanticSha256": candidate_extension_digest,
            "acceptedServiceMappingSemanticSha256": accepted_mapping_digest,
            "candidateServiceMappingSemanticSha256": candidate_mapping_digest,
            "acceptedExtension": str(accepted_extension),
            "acceptedServiceMapping": str(accepted_mapping),
            "candidateExtension": str(extension_candidate),
            "candidateServiceMapping": str(service_mapping_candidate),
        },
    }
    write_yaml(output / "run.yaml", result)
    (output / "summary.md").write_text(render_summary(result), encoding="utf-8")
    print(f"classification: {classification}")
    print(f"summary: {output / 'summary.md'}")
    return 0 if classification == "automatic" else 2 if classification == "extension-review-required" else 1


if __name__ == "__main__":
    raise SystemExit(main())

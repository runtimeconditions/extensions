"""Build a static site tree so every catalog extension is reachable at its
own `metadata.id` URL.

Each extension's id is the URL it's meant to be resolved at, e.g.
https://runtimeconditions.io/extensions/aws-s3/0.1.0/runtimeconditions.extension.yaml.
This copies each extension.yaml to the output directory at the path portion
of that URL, so publishing the output directory as a static site (GitHub
Pages or otherwise) makes every id resolvable by itself - no separate
mapping to keep in sync.
"""

from __future__ import annotations

import argparse
import shutil
import sys
from pathlib import Path
from urllib.parse import urlparse

from serialization import read_document

REPO_ROOT = Path(__file__).resolve().parents[2]
CATALOG_ROOT = REPO_ROOT / "catalog"
EXPECTED_HOST = "runtimeconditions.io"


def build(output_dir: Path) -> int:
    extension_paths = sorted(CATALOG_ROOT.glob("*/*/releases/*/runtimeconditions.extension.yaml"))
    if not extension_paths:
        print("No extensions found under catalog/.", file=sys.stderr)
        return 1

    errors = []
    for extension_path in extension_paths:
        doc = read_document(extension_path)
        extension_id = doc.get("metadata", {}).get("id", "")
        parsed = urlparse(extension_id)
        relative = extension_path.relative_to(REPO_ROOT)

        if parsed.scheme != "https" or parsed.netloc != EXPECTED_HOST:
            errors.append(f"{relative}: metadata.id {extension_id!r} is not an https://{EXPECTED_HOST}/... URL")
            continue

        destination = output_dir / parsed.path.lstrip("/")
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(extension_path, destination)
        print(f"{relative} -> {parsed.path}")

    if errors:
        for error in errors:
            print(error, file=sys.stderr)
        print(f"\n{len(errors)} extension(s) have an id that can't be published this way.", file=sys.stderr)
        return 1

    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    return build(args.output)


if __name__ == "__main__":
    sys.exit(main())

"""Build a static site tree so every catalog extension is reachable at its
own `metadata.id` URL.

Each extension's id is the URL it's meant to be resolved at, e.g.
https://runtimeconditions.io/extensions/aws-s3/0.1.0/runtimeconditions.extension.yaml.
This copies each extension.yaml to the output directory at the path portion
of that URL, so publishing the output directory as a static site (GitHub
Pages or otherwise) makes every id resolvable by itself - no separate
mapping to keep in sync. Generated Go modules also receive discovery pages
at their public module coordinates from the package catalog.
"""

from __future__ import annotations

import argparse
import shutil
import sys
from pathlib import Path
from html import escape
from urllib.parse import urlparse

from go_bindings import GoBinding, load_bindings, validate_source
from serialization import read_document

REPO_ROOT = Path(__file__).resolve().parents[2]
EXPECTED_HOST = "runtimeconditions.io"


def build_go_pages(output_dir: Path, root: Path) -> None:
    """Only generated modules get discovery pages; dormant catalog targets wait."""
    bindings = load_bindings(root)
    generated = []
    for binding in bindings.values():
        if not (root / binding.directory / "go.mod").is_file():
            continue
        validate_source(root, binding)
        generated.append(binding)
    for binding in generated:
        for module in sorted({binding.prefix, binding.module}):
            destination = output_dir / module.split("/", 1)[1] / "index.html"
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_text(go_page(binding), encoding="utf-8")
            print(f"{binding.directory} -> /{module.split('/', 1)[1]}/")
    if generated:
        destination = output_dir / "x/index.html"
        destination.parent.mkdir(parents=True, exist_ok=True)
        links = "\n".join(
            f'<li><a href="/{escape(binding.module.split("/", 1)[1])}/">{escape(binding.module)}</a></li>'
            for binding in sorted(generated, key=lambda item: item.module)
        )
        destination.write_text(
            '<!doctype html><html lang="en"><head><meta charset="utf-8">'
            '<title>Runtime Conditions Go bindings</title></head><body>'
            '<h1>Runtime Conditions Go bindings</h1><ul>' + links + '</ul>'
            '<p>Requires Go 1.25 or later.</p></body></html>\n', encoding="utf-8"
        )


def go_page(binding: GoBinding) -> str:
    return f'''<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="go-import" content="{escape(binding.go_import, quote=True)}">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{escape(binding.module)} — Go bindings</title>
</head>
<body>
  <h1>{escape(binding.module)}</h1>
  <p>Runtime Conditions extension bindings for Go. Requires Go {escape(binding.go_version)} or later.</p>
  <h2>Install a released version</h2>
  <pre><code>go get {escape(binding.module)}@v{escape(binding.version)}</code></pre>
  <h2>Import</h2>
  <pre><code>import "{escape(binding.module)}"</code></pre>
  <p>The package name is <code>{escape(binding.name)}</code>; an import alias is optional.</p>
  <p>Versions are published with repository tags. The catalog version's tag is
     <code>{escape(binding.tag)}</code>.</p>
  <p><a href="{escape(binding.repository)}/tree/main/{escape(binding.directory)}">Generated source</a>
     · <a href="https://pkg.go.dev/{escape(binding.module)}">Go documentation</a>
     · <a href="/x/">All Go bindings</a></p>
</body>
</html>
'''


def build(output_dir: Path, root: Path = REPO_ROOT) -> int:
    catalog_root = root / "catalog"
    extension_paths = sorted(catalog_root.glob("*/*/releases/*/runtimeconditions.extension.yaml"))
    extension_paths += sorted(
        path
        for path in catalog_root.glob("*/*/*.yaml")
        if read_document(path).get("kind") == "RuntimeConditionsExtensionDefinition"
    )
    if not extension_paths:
        print("No extensions found under catalog/.", file=sys.stderr)
        return 1

    errors = []
    for extension_path in extension_paths:
        doc = read_document(extension_path)
        extension_id = doc.get("metadata", {}).get("id", "")
        parsed = urlparse(extension_id)
        relative = extension_path.relative_to(root)

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

    try:
        build_go_pages(output_dir, root)
    except (ValueError, KeyError, OSError) as error:
        print(f"Go binding discovery failed: {error}", file=sys.stderr)
        return 1
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    return build(args.output)


if __name__ == "__main__":
    sys.exit(main())

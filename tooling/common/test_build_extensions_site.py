"""Exercise static discovery alongside extension-definition publication."""

from __future__ import annotations

from html.parser import HTMLParser
from pathlib import Path
import tempfile
import unittest

import yaml

from build_extensions_site import build
from go_bindings import load_bindings


class MetaTags(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.imports = []

    def handle_starttag(self, tag, attrs) -> None:
        values = dict(attrs)
        if tag == "meta" and values.get("name") == "go-import":
            self.imports.append(values.get("content"))


class SiteTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory(prefix="binding-site-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name) / "extensions"
        self.output = Path(temporary.name) / "site"
        self.config = {
            "coordinate": "runtimeconditions.io/x/vendor/service", "name": "service",
            "version": "0.1.0", "sourceDirectory": "bindings/service/go",
            "languageVersion": "1.25.0", "publicationMode": "github-tag",
        }
        self.catalog = {"repositoryUrl": "https://github.com/example/extensions", "packages": {
            "service": {"languages": {"go": self.config}},
            "pending": {"languages": {"go": {**self.config, "coordinate": "runtimeconditions.io/x/vendor/pending",
                "name": "pending", "sourceDirectory": "bindings/pending/go"}}},
        }}
        self.write_catalog()
        self.definition = self.root / "catalog/vendor/service/releases/0.1.0/runtimeconditions.extension.yaml"
        self.definition.parent.mkdir(parents=True)
        self.definition.write_text(yaml.safe_dump({"kind": "RuntimeConditionsExtensionDefinition", "metadata": {
            "id": "https://runtimeconditions.io/extensions/vendor/service/0.1.0/runtimeconditions.extension.yaml"}}))
        self.write_source()

    def write_catalog(self) -> None:
        path = self.root / "tooling/extension-bindings/packages.yaml"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(yaml.safe_dump(self.catalog), encoding="utf-8")

    def write_source(self) -> None:
        source = self.root / self.config["sourceDirectory"]
        source.mkdir(parents=True, exist_ok=True)
        (source / "go.mod").write_text(f'module {self.config["coordinate"]}\n\ngo 1.25\n')
        (source / "bindings.go").write_text(f'package {self.config["name"]}\n')

    def test_site_contains_discovery_install_and_unaliased_import(self) -> None:
        self.assertEqual(build(self.output, self.root), 0)
        page = (self.output / "x/vendor/service/index.html").read_text()
        tags = MetaTags()
        tags.feed(page)
        self.assertEqual(tags.imports, [
            "runtimeconditions.io/x/vendor/service git https://github.com/example/extensions bindings/service/go"])
        self.assertIn('import "runtimeconditions.io/x/vendor/service"', page)
        self.assertIn("go get runtimeconditions.io/x/vendor/service@v0.1.0", page)
        self.assertFalse((self.output / "x/vendor/pending").exists())
        self.assertIn("runtimeconditions.io/x/vendor/service", (self.output / "x/index.html").read_text())
        published = self.output / "extensions/vendor/service/0.1.0/runtimeconditions.extension.yaml"
        self.assertEqual(published.read_bytes(), self.definition.read_bytes())

    def test_mismatched_generated_module_fails_site_build(self) -> None:
        (self.root / "bindings/service/go/go.mod").write_text("module github.com/old/module\n")
        self.assertEqual(build(self.output, self.root), 1)
        self.assertFalse((self.output / "x/vendor/service").exists())

    def test_mismatched_generated_package_fails_site_build(self) -> None:
        (self.root / "bindings/service/go/bindings.go").write_text("package vendorservice\n")
        self.assertEqual(build(self.output, self.root), 1)

    def test_major_versions_publish_prefix_and_versioned_discovery(self) -> None:
        self.config.update(coordinate="runtimeconditions.io/x/vendor/service/v10", version="10.1.0")
        self.write_catalog()
        self.write_source()
        self.assertEqual(build(self.output, self.root), 0)
        base = (self.output / "x/vendor/service/index.html").read_text()
        versioned = (self.output / "x/vendor/service/v10/index.html").read_text()
        self.assertEqual(base, versioned)
        self.assertIn('content="runtimeconditions.io/x/vendor/service git', base)

    def test_invalid_publication_coordinates_are_rejected(self) -> None:
        for changes in ({"coordinate": "runtimeconditions.io/x/vendor/../escape"},
                        {"coordinate": "other.example/x/vendor/service"},
                        {"sourceDirectory": "../outside"},
                        {"name": "renamed"}, {"languageVersion": "1.24.0"},
                        {"version": "2.0.0"}, {"version": "0.1.0-beta.01"}):
            original = self.config.copy()
            self.config.update(changes)
            self.write_catalog()
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                load_bindings(self.root)
            self.config.clear()
            self.config.update(original)

    def test_duplicate_discovery_prefix_is_rejected(self) -> None:
        self.catalog["packages"]["pending"]["languages"]["go"].update(
            coordinate=self.config["coordinate"], name=self.config["name"])
        self.write_catalog()
        with self.assertRaisesRegex(ValueError, "duplicate public"):
            load_bindings(self.root)


if __name__ == "__main__":
    unittest.main()

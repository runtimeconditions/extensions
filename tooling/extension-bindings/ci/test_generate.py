"""Exercise target selection and direct-main commits against local Git remotes."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import yaml
from generate import commit, git, select_targets


class GenerationTests(unittest.TestCase):
    def setUp(self) -> None:
        summary = patch.dict("os.environ", {"GITHUB_STEP_SUMMARY": ""})
        summary.start()
        self.addCleanup(summary.stop)
        self.temp = tempfile.TemporaryDirectory(prefix="binding-generation-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "checkout"
        self.root.mkdir()
        git(self.root, "init", "--initial-branch=main")
        git(self.root, "config", "user.name", "Test")
        git(self.root, "config", "user.email", "test@example.invalid")
        self.catalog = self.root / "tooling/extension-bindings/packages.yaml"
        self.catalog.parent.mkdir(parents=True)
        self.packages = {
            "owner": {
                "languages": {
                    "go": {
                        "sourceDirectory": "bindings/owner/go",
                        "languageVersion": "1.25.0",
                    },
                    "python": {
                        "sourceDirectory": "bindings/owner/python",
                        "languageVersion": "3.12.10",
                    },
                }
            },
            "addon": {
                "languages": {
                    "go": {
                        "sourceDirectory": "bindings/addon/go",
                        "languageVersion": "1.25.0",
                    },
                }
            },
        }
        self.write_catalog()
        self.write("bindings/owner/go/stale.go", "old generated source\n")
        self.write("bindings/owner/python/source.py", "unchanged Python source\n")
        git(self.root, "add", ".")
        git(self.root, "commit", "-m", "Initial inputs")
        self.remote = Path(self.temp.name) / "remote.git"
        git(self.root, "init", "--bare", str(self.remote))
        git(self.root, "remote", "add", "origin", str(self.remote))
        git(self.root, "push", "origin", "HEAD:refs/heads/main")
        self.source = git(self.root, "rev-parse", "HEAD").strip()
        self.targets = select_targets(self.catalog, "owner:go")

    def write(self, name: str, data: str) -> None:
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(data, encoding="utf-8")

    def write_catalog(self) -> None:
        self.catalog.write_text(
            yaml.safe_dump({"packages": self.packages}), encoding="utf-8"
        )

    def test_defaults_select_only_go_and_sort_targets(self) -> None:
        self.assertEqual(
            [target[0] for target in select_targets(self.catalog, "")],
            ["addon:go", "owner:go"],
        )
        self.assertEqual(
            select_targets(self.catalog, "all"), select_targets(self.catalog, "")
        )

    def test_manual_selection_rejects_disabled_unknown_empty_and_duplicate_targets(
        self,
    ) -> None:
        for value in ["owner:python", "missing:go", "owner:go,", "owner:go,owner:go"]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                select_targets(self.catalog, value)

    def test_selection_rejects_unexpected_directory_and_mixed_compilers(self) -> None:
        self.packages["owner"]["languages"]["go"]["sourceDirectory"] = "catalog"
        self.write_catalog()
        with self.assertRaisesRegex(ValueError, "source directory"):
            select_targets(self.catalog, "owner:go")
        self.packages["owner"]["languages"]["go"]["sourceDirectory"] = (
            "bindings/owner/go"
        )
        self.packages["owner"]["languages"]["go"]["languageVersion"] = "1.26.0"
        self.write_catalog()
        with self.assertRaisesRegex(ValueError, "one exact Go version"):
            select_targets(self.catalog, "")

    def test_commit_adds_removes_and_pushes_only_selected_generated_source(
        self,
    ) -> None:
        (self.root / "bindings/owner/go/stale.go").unlink()
        self.write("bindings/owner/go/new.go", "new generated source\n")
        self.assertTrue(commit(self.root, self.targets, self.source))
        head = git(self.root, "rev-parse", "HEAD").strip()
        self.assertEqual(
            git(self.root, "ls-remote", "origin", "refs/heads/main").split()[0], head
        )
        self.assertEqual(git(self.root, "rev-parse", "HEAD^").strip(), self.source)
        self.assertEqual(git(self.root, "status", "--porcelain"), "")
        self.assertEqual(
            git(self.root, "show", "HEAD:bindings/owner/python/source.py"),
            "unchanged Python source\n",
        )
        self.assertFalse(commit(self.root, self.targets, head))
        self.assertEqual(git(self.root, "rev-parse", "HEAD").strip(), head)

    def test_commit_rejects_input_or_unselected_output_changes(self) -> None:
        for path in [
            "catalog/input.yaml",
            "bindings/owner/python/new.py",
            "bindings/owner/go-other/file",
        ]:
            with self.subTest(path=path):
                self.write(path, "unexpected\n")
                with self.assertRaisesRegex(ValueError, "outside selected bindings"):
                    commit(self.root, self.targets, self.source)
                (self.root / path).unlink()
        self.assertEqual(git(self.root, "rev-parse", "HEAD").strip(), self.source)

    def test_commit_rejects_an_existing_index(self) -> None:
        self.write("bindings/owner/go/new.go", "generated\n")
        git(self.root, "add", "bindings/owner/go/new.go")
        with self.assertRaisesRegex(ValueError, "index must be empty"):
            commit(self.root, self.targets, self.source)

    def test_commit_rejects_moved_main_before_staging(self) -> None:
        other = Path(self.temp.name) / "other"
        git(self.root, "clone", "--branch", "main", str(self.remote), str(other))
        git(other, "config", "user.name", "Test")
        git(other, "config", "user.email", "test@example.invalid")
        (other / "new-input.yaml").write_text("new input\n", encoding="utf-8")
        git(other, "add", ".")
        git(other, "commit", "-m", "Concurrent input")
        git(other, "push", "origin", "HEAD:refs/heads/main")
        self.write("bindings/owner/go/new.go", "generated from older input\n")
        with self.assertRaisesRegex(ValueError, "main moved"):
            commit(self.root, self.targets, self.source)
        self.assertEqual(git(self.root, "diff", "--cached", "--name-only"), "")
        self.assertEqual(git(self.root, "rev-parse", "HEAD").strip(), self.source)


if __name__ == "__main__":
    unittest.main()

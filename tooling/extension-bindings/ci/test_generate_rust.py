"""Exercise the Rust generation script."""

from __future__ import annotations

from pathlib import Path
from unittest.mock import patch

from generation import select_targets
from testsupport import RepoTestCase, load_script, run_script

script = load_script("generate-rust.py")


class RustGenerationTests(RepoTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.packages["owner"]["languages"]["rust"] = {
            "sourceDirectory": "bindings/owner/rust",
            "languageVersion": "1.92.0",
        }
        self.write_catalog()

    def test_configure_reports_the_version_and_whether_rust_is_selected(self) -> None:
        output = self.root.parent / "github-output"
        for targets, version, has_rust in [
            ("owner:rust", "1.92.0", "true"),
            ("owner:go", "", "false"),
            ("owner:go,owner:rust", "1.92.0", "true"),
        ]:
            with self.subTest(targets=targets):
                values = run_script(
                    "generate-rust.py",
                    self.root,
                    output,
                    "configure",
                    "--targets",
                    targets,
                )
                self.assertEqual(values, {"rust-version": version, "has-rust": has_rust})

    def test_update_refuses_rust_targets_without_the_toolchain(self) -> None:
        targets = select_targets(self.catalog, "owner:rust")
        with patch.object(script.shutil, "which", return_value=None), self.assertRaisesRegex(
            ValueError, "cargo, rustc, rustfmt"
        ):
            script.update(self.root, targets, Path("rc"), Path("core.yaml"))

    def test_update_passes_only_rust_targets_to_the_generator(self) -> None:
        targets = select_targets(self.catalog, "")
        with patch.object(script.shutil, "which", return_value="/bin/tool"), patch.object(
            script.subprocess, "run"
        ) as run:
            script.update(self.root, targets, Path("rc"), Path("core.yaml"))
        arguments = run.call_args.args[0]
        self.assertEqual(
            [arguments[i + 1] for i, arg in enumerate(arguments) if arg == "--target"],
            ["owner:rust"],
        )

    def test_update_needs_no_tools_without_rust_targets(self) -> None:
        targets = select_targets(self.catalog, "owner:go")
        with patch.object(script.shutil, "which", return_value=None), patch.object(
            script.subprocess, "run"
        ) as run:
            script.update(self.root, targets, Path("rc"), Path("core.yaml"))
        run.assert_not_called()


if __name__ == "__main__":
    import unittest

    unittest.main()

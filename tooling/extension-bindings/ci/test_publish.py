"""Check publication ordering, preflight, and retries against local Git remotes."""

from __future__ import annotations

import hashlib
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import yaml

from publish import (SUFFIXES, asset_paths, check_discovery, checksum_text, git,
                     ordered_targets, preflight, promote)
from build_extensions_site import go_page


class FakeGitHub:
    def __init__(self) -> None:
        self.releases = {}
        self.bytes = {}
        self.uploads = 0
        self.fail_once = False

    def release(self, tag):
        return self.releases.get(tag)

    def download(self, asset):
        return self.bytes[asset["id"]]

    def create(self, binding, source):
        release = {"id": len(self.releases) + 1, "draft": True, "assets": [],
                   "html_url": "https://github.com/example/extensions/releases/tag/" + binding.tag,
                   "source": source}
        self.releases[binding.tag] = release
        return release

    def upload(self, release, name, path):
        if self.fail_once and name.endswith(".source.zip"):
            self.fail_once = False
            raise OSError("interrupted upload")
        self.uploads += 1
        asset = {"name": name, "id": self.uploads}
        self.bytes[asset["id"]] = path.read_bytes()
        release["assets"].append(asset)

    def finish(self, release):
        release["draft"] = False


class PublicationTests(unittest.TestCase):
    def setUp(self) -> None:
        summary = patch.dict("os.environ", {"GITHUB_STEP_SUMMARY": ""})
        summary.start()
        self.addCleanup(summary.stop)
        temporary = tempfile.TemporaryDirectory(prefix="binding-publication-test-")
        self.addCleanup(temporary.cleanup)
        self.work = Path(temporary.name)
        self.root = self.work / "checkout"
        self.root.mkdir()
        self.output = self.work / "artifacts"
        self.output.mkdir()
        self.catalog = {"repositoryUrl": "https://github.com/example/extensions", "packages": {}}
        for key in ("owner", "addon"):
            config = {"coordinate": f"runtimeconditions.io/x/vendor/{key}", "name": key,
                      "version": "0.1.0", "sourceDirectory": f"bindings/{key}/go",
                      "languageVersion": "1.25.0", "publicationMode": "github-tag"}
            self.catalog["packages"][key] = {"languages": {"go": config}}
            directory = self.root / config["sourceDirectory"]
            directory.mkdir(parents=True)
            module = f'module {config["coordinate"]}\n\ngo 1.25\n'
            if key == "addon":
                module += "\nrequire runtimeconditions.io/x/vendor/owner v0.1.0\n"
            (directory / "go.mod").write_text(module)
            (directory / "bindings.go").write_text(f"package {key}\ntype Declaration struct{{}}\n")
            release = {"package": {"coordinate": config["coordinate"], "name": key, "version": "v0.1.0"}}
            if key == "addon":
                release["packageDependencies"] = [{"coordinate": "runtimeconditions.io/x/vendor/owner", "testedVersion": "v0.1.0"}]
            (directory / "runtimeconditions.binding-release.yaml").write_text(yaml.safe_dump(release))
        self.write_catalog()
        git(self.root, "init", "--initial-branch=main")
        git(self.root, "config", "user.name", "Test")
        git(self.root, "config", "user.email", "test@example.invalid")
        self.commit()
        self.remote = self.work / "remote.git"
        git(self.root, "init", "--bare", str(self.remote))
        git(self.root, "remote", "add", "origin", str(self.remote))
        git(self.root, "push", "origin", "main")
        self.bindings = ordered_targets(self.root, "addon:go")
        self.github = FakeGitHub()
        discovery = patch("publish.check_discovery")
        discovery.start()
        self.addCleanup(discovery.stop)
        self.make_assets()

    def write_catalog(self):
        path = self.root / "tooling/extension-bindings/packages.yaml"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(yaml.safe_dump(self.catalog))

    def commit(self):
        git(self.root, "add", ".")
        git(self.root, "commit", "-m", "Test inputs")
        self.source = git(self.root, "rev-parse", "HEAD").strip()

    def make_assets(self):
        manifest = {}
        for binding in self.bindings:
            for suffix in SUFFIXES:
                path = self.output / (binding.stem + suffix)
                if suffix.endswith(".zip"):
                    prefix = binding.directory + "/" if suffix == ".source.zip" else binding.module + "@v" + binding.version + "/"
                    with zipfile.ZipFile(path, "w") as archive:
                        for source in sorted((self.root / binding.directory).iterdir()):
                            archive.writestr(prefix + source.name, source.read_bytes())
                else:
                    path.write_text("test resource\n")
                manifest[path.name] = hashlib.sha256(path.read_bytes()).hexdigest()
            assets = asset_paths(self.output, binding)
            assets["SHA256SUMS"].parent.mkdir(parents=True, exist_ok=True)
            assets["SHA256SUMS"].write_text(checksum_text(assets))
        (self.output / "runtimeconditions.file-manifest.yaml").write_text(yaml.safe_dump({"files": manifest}))
        (self.output / "publication.yaml").write_text(yaml.safe_dump({"sourceCommit": self.source, "targets": [binding.target for binding in self.bindings]}))
        (self.output / "release-plan.yaml").write_text(yaml.safe_dump({"releases": [
            {"target": binding.target, "declaredVersion": binding.version, "releaseTag": binding.tag}
            for binding in self.bindings]}))

    def test_publication_includes_dependencies_in_topological_order(self):
        self.assertEqual([binding.target for binding in self.bindings], ["owner:go", "addon:go"])

    def test_dry_run_never_pushes_tags_or_creates_releases(self):
        promote(self.root, self.bindings, self.output, self.github, True)
        self.assertNotIn("refs/tags/", git(self.root, "ls-remote", "origin"))
        self.assertEqual(self.github.releases, {})

    def test_publish_and_retry_reuse_identical_tags_and_assets(self):
        promote(self.root, self.bindings, self.output, self.github, False)
        refs = git(self.root, "ls-remote", "origin")
        for binding in self.bindings:
            self.assertIn(binding.tag, refs)
            self.assertFalse(self.github.release(binding.tag)["draft"])
        uploads = self.github.uploads
        promote(self.root, self.bindings, self.output, self.github, False)
        self.assertEqual(self.github.uploads, uploads)
        self.assertEqual(git(self.root, "ls-remote", "origin"), refs)

    def test_interrupted_upload_can_resume_without_rebuilding(self):
        self.github.fail_once = True
        with self.assertRaisesRegex(OSError, "interrupted upload"):
            promote(self.root, self.bindings, self.output, self.github, False)
        self.assertTrue(self.github.release(self.bindings[0].tag)["draft"])
        promote(self.root, self.bindings, self.output, self.github, False)
        self.assertEqual(self.github.uploads, len(self.bindings) * (len(SUFFIXES) + 1))
        self.assertTrue(all(not release["draft"] for release in self.github.releases.values()))

    def test_unchanged_module_tag_is_reusable_after_unrelated_commits(self):
        promote(self.root, self.bindings, self.output, self.github, False)
        refs = git(self.root, "ls-remote", "origin")
        (self.root / "unrelated.txt").write_text("new input\n")
        self.commit()
        git(self.root, "push", "origin", "main")
        self.make_assets()
        promote(self.root, self.bindings, self.output, self.github, False)
        self.assertEqual([line for line in refs.splitlines() if "refs/tags/" in line],
                         [line for line in git(self.root, "ls-remote", "origin").splitlines() if "refs/tags/" in line])

    def test_checksum_failure_stops_before_any_mutation(self):
        asset_paths(self.output, self.bindings[-1])["SHA256SUMS"].write_text("corrupt\n")
        with self.assertRaisesRegex(ValueError, "SHA256SUMS differs"):
            promote(self.root, self.bindings, self.output, self.github, False)
        self.assertNotIn("refs/tags/", git(self.root, "ls-remote", "origin"))
        self.assertEqual(self.github.releases, {})

    def test_tag_collision_stops_before_other_modules_are_published(self):
        binding = self.bindings[-1]
        git(self.root, "checkout", "-b", "wrong-version")
        (self.root / binding.directory / "bindings.go").write_text("package addon\ntype Changed struct{}\n")
        self.commit()
        git(self.root, "tag", binding.tag)
        git(self.root, "push", "origin", "refs/tags/" + binding.tag)
        git(self.root, "checkout", "main")
        refs = git(self.root, "ls-remote", "origin")
        with self.assertRaisesRegex(ValueError, "different module bytes"):
            promote(self.root, self.bindings, self.output, self.github, False)
        self.assertEqual(git(self.root, "ls-remote", "origin"), refs)
        self.assertEqual(self.github.releases, {})

    def test_existing_asset_collision_is_rejected_without_overwriting(self):
        promote(self.root, self.bindings, self.output, self.github, False)
        release = self.github.release(self.bindings[-1].tag)
        self.github.bytes[release["assets"][0]["id"]] = b"different release bytes"
        uploads = self.github.uploads
        with self.assertRaisesRegex(ValueError, "existing release asset differs"):
            promote(self.root, self.bindings, self.output, self.github, False)
        self.assertEqual(self.github.uploads, uploads)

    def test_missing_or_mismatched_discovery_is_rejected(self):
        binding = self.bindings[0]
        with patch("publish.urlopen", return_value=io.BytesIO(b"<html></html>")):
            with self.assertRaisesRegex(ValueError, "deploy the docs"):
                check_discovery(binding)
        with patch("publish.urlopen", return_value=io.BytesIO(go_page(binding).encode())):
            check_discovery(binding)

    def test_remote_main_movement_is_rejected(self):
        other = self.work / "other"
        git(self.root, "clone", "--branch", "main", str(self.remote), str(other))
        git(other, "config", "user.name", "Test")
        git(other, "config", "user.email", "test@example.invalid")
        (other / "new-input.txt").write_text("new head\n")
        git(other, "add", ".")
        git(other, "commit", "-m", "Main moved")
        git(other, "push", "origin", "main")
        with self.assertRaisesRegex(ValueError, "main moved"):
            preflight(self.root, self.bindings, self.output, self.github)

    def test_dependency_version_mismatch_is_rejected(self):
        self.catalog["packages"]["owner"]["languages"]["go"]["version"] = "0.1.1"
        self.write_catalog()
        with self.assertRaisesRegex(ValueError, "dependency is missing"):
            ordered_targets(self.root, "addon:go")


if __name__ == "__main__":
    unittest.main()

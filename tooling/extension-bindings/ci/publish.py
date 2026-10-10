"""Package, preflight, and publish committed Go bindings with immutable tags."""

from __future__ import annotations

import argparse
import hashlib
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from urllib.error import HTTPError
from urllib.parse import quote, urlencode
from urllib.request import HTTPRedirectHandler, Request, build_opener, urlopen
import zipfile

import yaml

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "common"))
from go_bindings import GoBinding, load_bindings, validate_source
from serialization import read_document
from generate import git, select_targets, summarize

SUFFIXES = (".module.zip", ".source.zip", ".runtimeconditions.binding-model.yaml",
            ".runtimeconditions.binding-release.yaml", ".runtimeconditions.bindings.yaml",
            ".runtimeconditions.file-manifest.yaml")


def ordered_targets(root: Path, requested: str) -> list[GoBinding]:
    """Include native dependencies so publishing env alone also releases common."""
    bindings = load_bindings(root)
    providers = {item.module: item for item in bindings.values()}
    state = {}
    result = []

    def visit(binding: GoBinding) -> None:
        if state.get(binding.target) == "done":
            return
        if state.get(binding.target) == "visiting":
            raise ValueError(f"package dependency cycle at {binding.target}")
        state[binding.target] = "visiting"
        validate_source(root, binding)
        release = read_document(root / binding.directory / "runtimeconditions.binding-release.yaml")
        identity = release["package"]
        if (identity["coordinate"], identity["name"], identity["version"]) != (binding.module, binding.name, "v" + binding.version):
            raise ValueError(f"{binding.target}: release identity differs from catalog; regenerate bindings")
        for dependency in release.get("packageDependencies", []):
            provider = providers.get(dependency["coordinate"])
            if provider is None or dependency["testedVersion"] != "v" + provider.version:
                raise ValueError(f"{binding.target}: dependency is missing or its catalog version differs")
            visit(provider)
        state[binding.target] = "done"
        result.append(binding)

    for target, _, _ in select_targets(root / "tooling/extension-bindings/packages.yaml", requested):
        visit(bindings[target])
    if len({binding.go_version for binding in result}) != 1:
        raise ValueError("publication requires one exact Go compiler for the dependency closure")
    return result


def clean_commit(root: Path) -> str:
    if git(root, "status", "--porcelain"):
        raise ValueError("publication requires a clean checkout of committed generated source")
    return git(root, "rev-parse", "HEAD").strip()


def asset_paths(output: Path, binding: GoBinding) -> dict[str, Path]:
    assets = {binding.stem + suffix: output / (binding.stem + suffix) for suffix in SUFFIXES}
    assets["SHA256SUMS"] = output / "checksums" / binding.key / "SHA256SUMS"
    return assets


def checksum_text(assets: dict[str, Path]) -> str:
    return "".join(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {name}\n"
                   for name, path in sorted(assets.items()) if name != "SHA256SUMS")


def build(root: Path, bindings: list[GoBinding], rc: Path, core: Path, output: Path) -> None:
    source = clean_commit(root)
    tooling = root / "tooling/extension-bindings"
    args = [str(rc.resolve()), "bindings", "package", "--packages", str(tooling / "packages.yaml"),
            "--toolchain-lock", str(tooling / "toolchain.lock.yaml"),
            "--core-schema", str(core.resolve())]
    for binding in bindings:
        args.extend(["--target", binding.target])
    subprocess.run([*args, "--output", str(output.resolve())], cwd=root, check=True)
    args[2] = "plan-release"
    plan = subprocess.run(args, cwd=root, check=True, capture_output=True, text=True)
    (output / "release-plan.yaml").write_text(plan.stdout, encoding="utf-8")
    for binding in bindings:
        assets = asset_paths(output, binding)
        assets["SHA256SUMS"].parent.mkdir(parents=True, exist_ok=True)
        assets["SHA256SUMS"].write_text(checksum_text(assets), encoding="utf-8")
    (output / "publication.yaml").write_text(yaml.safe_dump({
        "sourceCommit": source, "targets": [binding.target for binding in bindings],
    }, sort_keys=False), encoding="utf-8")
    summarize(f"Verified and packaged Go bindings from `{source}`: " + ", ".join(binding.target for binding in bindings))


class Discovery(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.imports = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        values = dict(attrs)
        if tag == "meta" and values.get("name") == "go-import":
            self.imports.append(values.get("content"))


def check_discovery(binding: GoBinding) -> None:
    with urlopen("https://" + binding.module + "?go-get=1", timeout=30) as response:
        html = response.read(1 << 20).decode("utf-8")
    discovery = Discovery()
    discovery.feed(html)
    if discovery.imports != [binding.go_import]:
        raise ValueError(f"{binding.module}: deploy the docs discovery page before publishing; expected {binding.go_import!r}")
    if binding.module != binding.prefix:
        # Go verifies metadata at the import prefix for major-version paths.
        prefix = GoBinding(binding.key, binding.prefix, binding.name, binding.version,
                           binding.directory, binding.go_version, binding.repository)
        check_discovery(prefix)


class AssetRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, url):
        redirected = super().redirect_request(request, response, code, message, headers, url)
        if redirected is not None:
            redirected.remove_header("Authorization")
        return redirected


class GitHub:
    def __init__(self, repository: str) -> None:
        self.repository = repository.removeprefix("https://github.com/")
        self.base = "https://api.github.com/repos/" + self.repository
        self.token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN", "")

    def request(self, url: str, method: str = "GET", data: bytes | None = None,
                content_type: str = "application/json") -> dict | None:
        headers = {"Accept": "application/vnd.github+json", "User-Agent": "runtimeconditions-go-bindings",
                   "Content-Type": content_type, "X-GitHub-Api-Version": "2022-11-28"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        try:
            with urlopen(Request(url, data=data, headers=headers, method=method), timeout=60) as response:
                return json.load(response)
        except HTTPError as error:
            if error.code == 404 and method == "GET":
                return None
            raise ValueError(f"GitHub {method} failed with HTTP {error.code}") from error

    def release(self, tag: str) -> dict | None:
        return self.request(self.base + "/releases/tags/" + quote(tag, safe=""))

    def download(self, asset: dict) -> bytes:
        # Draft assets require authentication. Drop it before following a CDN redirect.
        headers = {"Accept": "application/octet-stream", "User-Agent": "runtimeconditions-go-bindings",
                   "X-GitHub-Api-Version": "2022-11-28"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        request = Request(self.base + "/releases/assets/" + str(asset["id"]), headers=headers)
        with build_opener(AssetRedirect()).open(request, timeout=60) as response:
            data = response.read((128 << 20) + 1)
        if len(data) > 128 << 20:
            raise ValueError("release asset exceeds size limit")
        return data

    def create(self, binding: GoBinding, source: str) -> dict:
        body = {"tag_name": binding.tag, "target_commitish": source, "draft": True,
                "prerelease": "-" in binding.version,
                "name": f"{binding.module} v{binding.version}",
                "body": f"Install with `go get {binding.module}@v{binding.version}`.\n\n"
                        f"Generated source: `{binding.directory}` at `{source}`.\n"
                        "Requires Go 1.25 or later. Package archives and SHA256SUMS are attached."}
        return self.request(self.base + "/releases", "POST", json.dumps(body).encode())

    def upload(self, release: dict, name: str, path: Path) -> None:
        url = release["upload_url"].split("{", 1)[0] + "?" + urlencode({"name": name})
        self.request(url, "POST", path.read_bytes(), "application/octet-stream")

    def finish(self, release: dict) -> None:
        if release["draft"]:
            self.request(self.base + "/releases/" + str(release["id"]), "PATCH", b'{"draft":false}')


def verify_archives(root: Path, output: Path, binding: GoBinding) -> None:
    directory = root / binding.directory
    tree = {str(path.relative_to(directory)): path.read_bytes() for path in directory.rglob("*") if path.is_file()}
    for suffix, prefix in ((".source.zip", binding.directory + "/"),
                           (".module.zip", binding.module + "@v" + binding.version + "/")):
        with zipfile.ZipFile(output / (binding.stem + suffix)) as archive:
            expected = {prefix + name: data for name, data in tree.items()}
            if len(archive.infolist()) != len(expected) or set(archive.namelist()) != set(expected):
                raise ValueError(f"{binding.target}: {suffix} inventory differs from committed source")
            if any(archive.read(name) != data for name, data in expected.items()):
                raise ValueError(f"{binding.target}: {suffix} bytes differ from committed source")


def preflight(root: Path, bindings: list[GoBinding], output: Path, github: GitHub) -> tuple[str, dict, list[str]]:
    source = clean_commit(root)
    publication = read_document(output / "publication.yaml")
    if publication != {"sourceCommit": source, "targets": [binding.target for binding in bindings]}:
        raise ValueError("publication artifacts do not identify this exact checkout and selected targets")
    refs = dict((line.split()[1], line.split()[0]) for line in git(root, "ls-remote", "origin").splitlines())
    if refs.get("refs/heads/main") != source:
        raise ValueError("main moved after packaging; rerun publication on its current head")
    plan = read_document(output / "release-plan.yaml")
    planned = {entry["target"]: entry for entry in plan["releases"]}
    if set(planned) != {binding.target for binding in bindings}:
        raise ValueError("release plan does not cover the selected targets")
    manifest = read_document(output / "runtimeconditions.file-manifest.yaml")["files"]
    releases = {}
    new_tags = []
    for binding in bindings:
        entry = planned[binding.target]
        if entry["declaredVersion"] != binding.version or entry["releaseTag"] != binding.tag:
            raise ValueError(f"{binding.target}: release plan identity differs")
        assets = asset_paths(output, binding)
        for name, path in assets.items():
            if name != "SHA256SUMS" and manifest.get(name) != hashlib.sha256(path.read_bytes()).hexdigest():
                raise ValueError(f"{binding.target}: artifact digest differs for {name}")
        if assets["SHA256SUMS"].read_text(encoding="utf-8") != checksum_text(assets):
            raise ValueError(f"{binding.target}: SHA256SUMS differs")
        verify_archives(root, output, binding)
        check_discovery(binding)
        ref = "refs/tags/" + binding.tag
        if ref in refs:
            git(root, "fetch", "--no-tags", "origin", ref)
            tagged_commit = refs.get(ref + "^{}", refs[ref])
            published_tree = git(root, "rev-parse", tagged_commit + ":" + binding.directory).strip()
            if published_tree != git(root, "rev-parse", "HEAD:" + binding.directory).strip():
                raise ValueError(f"{binding.tag}: already names different module bytes; bump its catalog version")
        else:
            new_tags.append(binding.tag)
        release = github.release(binding.tag)
        if release and ref not in refs:
            raise ValueError(f"{binding.tag}: release exists without its tag")
        if release:
            existing = {asset["name"]: asset for asset in release["assets"]}
            if set(existing) - set(assets):
                raise ValueError(f"{binding.tag}: release has unexpected assets")
            for name, asset in existing.items():
                if github.download(asset) != assets[name].read_bytes():
                    raise ValueError(f"{binding.tag}: existing release asset differs: {name}")
        releases[binding.tag] = release
    return source, releases, new_tags


def promote(root: Path, bindings: list[GoBinding], output: Path, github: GitHub, dry_run: bool) -> None:
    source, releases, new_tags = preflight(root, bindings, output, github)
    if dry_run:
        summarize("Dry run passed: committed source, package versions, archives, live discovery pages, tags, and release assets verified. No publication was performed.")
        return
    if git(root, "ls-remote", "origin", "refs/heads/main").split()[0] != source:
        raise ValueError("main moved during preflight; rerun publication")
    for binding in bindings:
        if binding.tag in new_tags:
            git(root, "-c", "user.name=github-actions[bot]",
                "-c", "user.email=41898282+github-actions[bot]@users.noreply.github.com",
                "tag", "-a", binding.tag, source, "-m", f"Release {binding.module} v{binding.version}")
    if new_tags:
        # All new module versions become available together. Never move a tag.
        git(root, "push", "--atomic", "origin", *("refs/tags/" + tag for tag in new_tags))
    for binding in bindings:
        release = releases[binding.tag] or github.create(binding, source)
        existing = {asset["name"] for asset in release["assets"]}
        for name, path in asset_paths(output, binding).items():
            if name not in existing:
                github.upload(release, name, path)
        github.finish(release)
        summarize(f"Published `{binding.module}@v{binding.version}`: " + release["html_url"])


def smoke(bindings: list[GoBinding]) -> None:
    for proxy in ("direct", "https://proxy.golang.org,direct"):
        with tempfile.TemporaryDirectory(prefix="rc-go-install-") as temporary:
            work = Path(temporary)
            env = {**os.environ, "GOWORK": "off", "GOPROXY": proxy, "GOMODCACHE": str(work / "cache"),
                   "GOSUMDB": "sum.golang.org", "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "",
                   "GOINSECURE": "", "GIT_TERMINAL_PROMPT": "0", "GH_TOKEN": "", "GITHUB_TOKEN": ""}
            for binding in bindings:
                consumer = work / binding.key
                consumer.mkdir()
                (consumer / "go.mod").write_text("module consumer.invalid/bindings\n\ngo 1.25\n", encoding="utf-8")
                (consumer / "main.go").write_text(
                    f'package main\nimport "{binding.module}"\nvar _ {binding.name}.Declaration\nfunc main() {{}}\n', encoding="utf-8")
                subprocess.run(["go", "get", binding.module + "@v" + binding.version], cwd=consumer, env=env, check=True)
                subprocess.run(["go", "build", "./..."], cwd=consumer, env=env, check=True)
        summarize(f"Fresh consumer installation and unaliased imports passed with GOPROXY={proxy}.")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["configure", "build", "promote", "smoke"])
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--targets", default="")
    parser.add_argument("--rc", type=Path)
    parser.add_argument("--core-schema", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    try:
        root = args.root.resolve()
        bindings = ordered_targets(root, args.targets)
        if args.command == "configure":
            if path := os.environ.get("GITHUB_OUTPUT"):
                with Path(path).open("a", encoding="utf-8") as stream:
                    stream.write(f"go-version={bindings[0].go_version}\nsource-commit={clean_commit(root)}\n")
                    stream.write("targets=" + ",".join(binding.target for binding in bindings) + "\n")
            summarize("Publishing dependency closure: " + ", ".join(binding.target for binding in bindings))
        elif args.command == "build":
            if args.rc is None or args.core_schema is None or args.output is None:
                raise ValueError("build requires --rc, --core-schema, and --output")
            build(root, bindings, args.rc, args.core_schema, args.output.resolve())
        elif args.command == "promote":
            if args.output is None:
                raise ValueError("promote requires --output")
            promote(root, bindings, args.output.resolve(), GitHub(bindings[0].repository), args.dry_run)
        else:
            smoke(bindings)
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError, zipfile.BadZipFile) as error:
        if isinstance(error, subprocess.CalledProcessError) and error.stderr:
            print(error.stderr, file=sys.stderr)
        parser.exit(1, f"Go binding publication failed: {error}\n")


if __name__ == "__main__":
    main()

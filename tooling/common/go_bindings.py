"""Public Go module coordinates shared by site assembly and publication."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import re

from serialization import read_document

MAJOR_SUFFIX = r"/v(?:[2-9]|[1-9][0-9]+)$"
MODULE = re.compile(r"^runtimeconditions\.io/x/[a-z][a-z0-9-]*/[a-z][a-z0-9_]*(?:/v(?:[2-9]|[1-9][0-9]+))?$")
PRERELEASE = r"(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
VERSION = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-" + PRERELEASE + r"(?:\." + PRERELEASE + r")*)?$")
GO_KEYWORDS = set("break default func interface select case defer go map struct chan else goto package switch const fallthrough if range type continue for import return var".split())


@dataclass(frozen=True)
class GoBinding:
    key: str
    module: str
    name: str
    version: str
    directory: str
    go_version: str
    repository: str

    @property
    def target(self) -> str:
        return f"{self.key}:go"

    @property
    def tag(self) -> str:
        return f"{self.directory}/v{self.version}"

    @property
    def prefix(self) -> str:
        return re.sub(MAJOR_SUFFIX, "", self.module)

    @property
    def go_import(self) -> str:
        return f"{self.prefix} git {self.repository} {self.directory}"

    @property
    def stem(self) -> str:
        return f"{self.key}-go-{self.version}"


def load_bindings(root: Path) -> dict[str, GoBinding]:
    catalog = read_document(root / "tooling/extension-bindings/packages.yaml")
    repository = catalog["repositoryUrl"].rstrip("/")
    if not re.fullmatch(r"https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("Go bindings require an HTTPS GitHub repositoryUrl")
    result = {}
    prefixes = set()
    for key, package in catalog["packages"].items():
        config = package["languages"].get("go")
        if config is None:
            continue
        if not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", key):
            raise ValueError(f"invalid package key: {key}")
        binding = GoBinding(key, str(config["coordinate"]), str(config["name"]),
                            str(config["version"]), str(config["sourceDirectory"]),
                            str(config["languageVersion"]), repository)
        if not MODULE.fullmatch(binding.module):
            raise ValueError(f"{key}: expected a runtimeconditions.io/x/<provider>/<extension> module")
        if binding.directory != f"bindings/{key}/go":
            raise ValueError(f"{key}: unexpected Go sourceDirectory")
        if binding.name != binding.prefix.rsplit("/", 1)[1] or binding.name in GO_KEYWORDS:
            raise ValueError(f"{key}: Go package name must match the public path's final component")
        version = VERSION.fullmatch(binding.version)
        if version is None:
            raise ValueError(f"{key}: invalid Go module version")
        major = int(version[1])
        suffix = binding.module[len(binding.prefix):]
        if suffix != (f"/v{major}" if major >= 2 else ""):
            raise ValueError(f"{key}: module path must match its major version")
        if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", binding.go_version) or tuple(map(int, binding.go_version.split("."))) < (1, 25, 0):
            raise ValueError(f"{key}: subdirectory discovery requires Go 1.25 or later")
        if config["publicationMode"] != "github-tag":
            raise ValueError(f"{key}: Go publicationMode must be github-tag")
        if binding.prefix in prefixes:
            raise ValueError(f"duplicate public Go module path: {binding.prefix}")
        prefixes.add(binding.prefix)
        result[binding.target] = binding
    return result


def validate_source(root: Path, binding: GoBinding) -> None:
    directory = root / binding.directory
    if not directory.resolve().is_relative_to(root.resolve()):
        raise ValueError(f"{binding.target}: source directory escapes the repository")
    module = (directory / "go.mod").read_text(encoding="utf-8")
    if re.findall(r"^module\s+(\S+)\s*$", module, re.MULTILINE) != [binding.module]:
        raise ValueError(f"{binding.target}: go.mod module differs from the package catalog; regenerate bindings")
    source = (directory / "bindings.go").read_text(encoding="utf-8")
    if re.findall(r"^package\s+(\w+)\s*$", source, re.MULTILINE) != [binding.name]:
        raise ValueError(f"{binding.target}: generated package name differs from the package catalog")

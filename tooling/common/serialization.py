"""Runtime Conditions document serialization with YAML as the first-party format.

Canonical copy. Every extension's tools/serialization.py is a thin shim
that loads this module - see this repo's README for why. This is the
NATS extension's version of the file, which was the most recently
updated copy at dedup time.
"""

from __future__ import annotations

import json
import re
from copy import deepcopy
from pathlib import Path
from typing import Any

import yaml


class RuntimeConditionsLoader(yaml.SafeLoader):
    """YAML 1.2-oriented safe loader that keeps timestamps as strings."""


RuntimeConditionsLoader.yaml_implicit_resolvers = deepcopy(yaml.SafeLoader.yaml_implicit_resolvers)
for first_character, resolvers in list(RuntimeConditionsLoader.yaml_implicit_resolvers.items()):
    RuntimeConditionsLoader.yaml_implicit_resolvers[first_character] = [(tag, pattern) for tag, pattern in resolvers if tag not in {"tag:yaml.org,2002:bool", "tag:yaml.org,2002:timestamp"}]
RuntimeConditionsLoader.add_implicit_resolver("tag:yaml.org,2002:bool", re.compile(r"^(?:true|false)$", re.IGNORECASE), list("tTfF"))


def read_document(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as stream:
        value = json.load(stream) if path.suffix == ".json" else yaml.load(stream, Loader=RuntimeConditionsLoader)
    if not isinstance(value, dict):
        raise ValueError(f"{path}: expected a document object")
    return value


def write_yaml(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(yaml.safe_dump(value, sort_keys=False, allow_unicode=True, width=4096), encoding="utf-8")

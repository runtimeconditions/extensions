"""Shim: re-exports the canonical serialization helpers from tooling/common.

Kept as a same-named local module (not a package import) so this
directory's scripts can keep using `from serialization import ...` when
run directly as `python tools/<script>.py`. The actual implementation
lives in tooling/common/serialization.py - don't add logic here.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

_repo_root = Path(__file__).resolve().parents[3]
_shared_path = _repo_root / "tooling" / "common" / "serialization.py"
_spec = importlib.util.spec_from_file_location("_rc_shared_serialization", _shared_path)
_module = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = _module
_spec.loader.exec_module(_module)

RuntimeConditionsLoader = _module.RuntimeConditionsLoader
read_document = _module.read_document
write_yaml = _module.write_yaml

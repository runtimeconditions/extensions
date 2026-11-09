"""Configure and update verified Rust bindings for the Actions workflow."""

from __future__ import annotations

import argparse
import shutil
import subprocess
from pathlib import Path

from generation import (
    Target,
    exact_version,
    of_language,
    select_targets,
    summarize,
    write_outputs,
)

LANGUAGE = "rust"
TOOLS = ("cargo", "rustc", "rustfmt")


def require_tools() -> None:
    missing = [name for name in TOOLS if shutil.which(name) is None]
    if missing:
        raise ValueError(
            "Rust targets require the provisioned toolchain on PATH: missing "
            + ", ".join(missing)
        )


def update(root: Path, targets: list[Target], rc: Path, core: Path) -> None:
    selected = of_language(targets, LANGUAGE)
    if not selected:
        summarize("No Rust targets selected; nothing to update.")
        return
    require_tools()
    tooling = root / "tooling" / "extension-bindings"
    args = [
        str(rc.resolve()),
        "bindings",
        "update",
        "--packages",
        str(tooling / "packages.yaml"),
        "--toolchain-lock",
        str(tooling / "toolchain.lock.yaml"),
        "--core-schema",
        str(core.resolve()),
    ]
    for key, _, _ in selected:
        args.extend(["--target", key])
    subprocess.run(args, cwd=root, check=True)
    summarize(f"Verified and synchronized {len(selected)} Rust binding targets.")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["configure", "update"])
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--targets", default="")
    parser.add_argument("--rc", type=Path)
    parser.add_argument("--core-schema", type=Path)
    args = parser.parse_args()
    root = args.root.resolve()
    try:
        targets = select_targets(
            root / "tooling/extension-bindings/packages.yaml", args.targets
        )
        if args.command == "configure":
            version = exact_version(targets, LANGUAGE)
            write_outputs(rust_version=version, has_rust="true" if version else "false")
        else:
            if args.rc is None or args.core_schema is None:
                raise ValueError("update requires --rc and --core-schema")
            update(root, targets, args.rc, args.core_schema)
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"binding generation failed: {error}\n")


if __name__ == "__main__":
    main()

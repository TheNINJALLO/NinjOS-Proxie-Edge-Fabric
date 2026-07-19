#!/usr/bin/env python3
"""Generate the companion operator guide from the versioned template."""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path

COMPANION_ROOT = Path(__file__).resolve().parents[1]
TEMPLATE = COMPANION_ROOT / "docs" / "COMPLETE_SETUP.md.in"
OUTPUT = COMPANION_ROOT / "docs" / "COMPLETE_SETUP.md"
METADATA = COMPANION_ROOT / "release-metadata.json"


def cmake_value(pattern: str, text: str, label: str) -> str:
    match = re.search(pattern, text, flags=re.MULTILINE | re.DOTALL)
    if not match:
        raise SystemExit(f"Unable to determine {label} from companion/CMakeLists.txt")
    return match.group(1)


def render() -> str:
    metadata = json.loads(METADATA.read_text(encoding="utf-8"))
    cmake = (COMPANION_ROOT / "CMakeLists.txt").read_text(encoding="utf-8")
    plugin_version = cmake_value(
        r"project\s*\(\s*ninjos_proxie_companion\s+VERSION\s+([0-9.]+)",
        cmake,
        "plugin version",
    )
    endstone_version = cmake_value(
        r"set\s*\(\s*ENDSTONE_API_VERSION\s+\"([^\"]+)\"",
        cmake,
        "default Endstone version",
    )

    declared_plugin = str(metadata.get("companionVersion", "")).strip()
    declared_endstone = str(metadata.get("defaultEndstoneVersion", "")).strip()
    if declared_plugin and declared_plugin != plugin_version:
        raise SystemExit(
            f"release-metadata.json companionVersion={declared_plugin} does not match CMake {plugin_version}"
        )
    if declared_endstone and declared_endstone != endstone_version:
        raise SystemExit(
            f"release-metadata.json defaultEndstoneVersion={declared_endstone} does not match CMake {endstone_version}"
        )

    replacements = {
        "@EDGE_VERSION@": str(metadata["edgeFabricVersion"]),
        "@COMPANION_VERSION@": plugin_version,
        "@ENDSTONE_VERSION@": endstone_version,
        "@DASHBOARD_PORT@": str(metadata.get("dashboardDefaultPort", 25571)),
    }
    content = TEMPLATE.read_text(encoding="utf-8")
    for marker, value in replacements.items():
        content = content.replace(marker, value)
    unresolved = sorted(set(re.findall(r"@[A-Z0-9_]+@", content)))
    if unresolved:
        raise SystemExit(f"Unresolved documentation markers: {', '.join(unresolved)}")
    return content.rstrip() + "\n"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--root-copy",
        type=Path,
        help="Also write the generated guide to this repository-relative or absolute path.",
    )
    args = parser.parse_args()

    content = render()
    OUTPUT.write_text(content, encoding="utf-8")
    print(f"[companion-docs] generated {OUTPUT}")

    if args.root_copy:
        target = args.root_copy
        if not target.is_absolute():
            target = COMPANION_ROOT.parent / target
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
        print(f"[companion-docs] generated {target}")


if __name__ == "__main__":
    main()

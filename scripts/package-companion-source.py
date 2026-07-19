#!/usr/bin/env python3
"""Create the dashboard-downloadable Endstone companion source archive."""

from __future__ import annotations

import json
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
COMPANION = ROOT / "companion"
METADATA = json.loads((COMPANION / "release-metadata.json").read_text(encoding="utf-8"))
VERSION = str(METADATA["companionVersion"])
OUTPUT = ROOT / f"NinjOS-Endstone-Companion-v{VERSION}-GitHub-Clean.zip"
SKIP_PARTS = {".git", "build", "dist", "__pycache__", ".cache"}
SKIP_SUFFIXES = {".pyc", ".pyo", ".so", ".o", ".a"}

with zipfile.ZipFile(OUTPUT, "w", zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(COMPANION.rglob("*")):
        if not path.is_file():
            continue
        rel = path.relative_to(COMPANION)
        if any(part in SKIP_PARTS for part in rel.parts):
            continue
        if path.suffix in SKIP_SUFFIXES:
            continue
        archive.write(path, rel.as_posix())

print(f"[companion-package] created {OUTPUT}")

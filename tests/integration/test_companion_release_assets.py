#!/usr/bin/env python3
from __future__ import annotations

import json
import subprocess
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def main() -> None:
    metadata = json.loads((ROOT / "companion" / "release-metadata.json").read_text(encoding="utf-8"))
    companion_version = metadata["companionVersion"]
    edge_version = metadata["edgeFabricVersion"]

    subprocess.run(
        [
            "python3",
            str(ROOT / "companion" / "scripts" / "generate-documentation.py"),
            "--root-copy",
            "docs/COMPANION.md",
        ],
        check=True,
        cwd=ROOT,
    )
    subprocess.run(["python3", str(ROOT / "scripts" / "package-companion-source.py")], check=True, cwd=ROOT)

    root_guide = (ROOT / "docs" / "COMPANION.md").read_text(encoding="utf-8")
    companion_guide = (ROOT / "companion" / "docs" / "COMPLETE_SETUP.md").read_text(encoding="utf-8")
    assert root_guide == companion_guide
    assert f"Edge Fabric release: {edge_version}" in root_guide
    assert f"Companion release: {companion_version}" in root_guide
    assert "@EDGE_VERSION@" not in root_guide
    assert "## 16. Multi-server deployment rules" in root_guide
    assert "## 18. Common errors" in root_guide
    assert "## 22. Final verification checklist" in root_guide

    archive = ROOT / f"NinjOS-Endstone-Companion-v{companion_version}-GitHub-Clean.zip"
    assert archive.is_file()
    with zipfile.ZipFile(archive) as bundle:
        names = set(bundle.namelist())
        required = {
            ".github/workflows/build-companion.yml",
            "README.md",
            "CMakeLists.txt",
            "release-metadata.json",
            "scripts/generate-documentation.py",
            "docs/COMPLETE_SETUP.md.in",
            "docs/COMPLETE_SETUP.md",
            "docs/INSTALL.md",
            "companion.properties.example",
            "src/plugin.cpp",
        }
        missing = sorted(required - names)
        assert not missing, missing
        workflow = bundle.read(".github/workflows/build-companion.yml").decode("utf-8")
        assert "Generate operator documentation" in workflow
        assert 'COMPANION-HOWTO.md' in workflow

    package_script = (ROOT / "scripts" / "package-release.sh").read_text(encoding="utf-8")
    assert "ENDSTONE-COMPANION-HOWTO.md" in package_script

    print("companion-release-assets-v7.3.4: PASS")


if __name__ == "__main__":
    main()

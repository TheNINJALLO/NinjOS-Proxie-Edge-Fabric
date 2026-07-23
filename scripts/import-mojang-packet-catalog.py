#!/usr/bin/env python3
"""Build the inspector's factual packet ID/name catalog from Mojang docs."""

from __future__ import annotations

import argparse
import json
import subprocess
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path, help="Checkout of Mojang/bedrock-protocol-docs")
    parser.add_argument("--output", type=Path, default=Path("dashboard/packet-names.json"))
    args = parser.parse_args()

    json_dir = args.source / "json"
    if not json_dir.is_dir():
        raise SystemExit(f"Missing Mojang JSON directory: {json_dir}")

    packets: dict[str, str] = {}
    versions: set[str] = set()
    protocols: set[int] = set()
    for source in sorted(json_dir.glob("*Packet.json")):
        document = json.loads(source.read_text(encoding="utf-8"))
        packet_id = document.get("$metaProperties", {}).get("[cereal:packet]")
        title = str(document.get("title", "")).strip()
        if not isinstance(packet_id, int) or not title.endswith("Packet"):
            continue
        key = str(packet_id)
        if key in packets:
            raise SystemExit(f"Duplicate Mojang packet ID {packet_id}")
        packets[key] = title.removesuffix("Packet")
        if document.get("x-minecraft-version"):
            versions.add(str(document["x-minecraft-version"]))
        if isinstance(document.get("x-protocol-version"), int):
            protocols.add(document["x-protocol-version"])

    if len(packets) < 100:
        raise SystemExit(f"Only {len(packets)} packet definitions were found")
    commit = subprocess.run(
        ["git", "-C", str(args.source), "rev-parse", "HEAD"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    catalog = {
        "source": "Mojang/bedrock-protocol-docs",
        "sourceUrl": "https://github.com/Mojang/bedrock-protocol-docs",
        "sourceCommit": commit,
        "minecraftVersions": sorted(versions),
        "protocolVersions": sorted(protocols),
        "packetCount": len(packets),
        "packets": dict(sorted(packets.items(), key=lambda item: int(item[0]))),
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(catalog, indent=2) + "\n", encoding="utf-8")
    print(f"[packet-catalog] wrote {len(packets)} Mojang packet names to {args.output}")


if __name__ == "__main__":
    main()

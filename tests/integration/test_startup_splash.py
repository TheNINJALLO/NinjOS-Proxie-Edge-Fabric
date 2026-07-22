#!/usr/bin/env python3
from __future__ import annotations

import os
import shutil
import signal
import subprocess
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="ninjos-splash-") as temp_name:
        temp = Path(temp_name)
        (temp / "config").mkdir()

        for name in ("NinjOSEdge", "NinjOSDashboard"):
            shutil.copy2(ROOT / "prebuilt" / "linux-x86_64" / name, temp / name)
        shutil.copy2(ROOT / "start-runtime.sh", temp / "start-runtime.sh")
        shutil.copy2(ROOT / "config" / "edge-fabric.example.ini", temp / "edge-fabric.example.ini")

        config = (ROOT / "config" / "edge-fabric.example.ini").read_text(encoding="utf-8")
        replacements = {
            "185.83.152.144": "127.0.0.1",
            "25566,25571,25572-25581": "40366,40371,40372-40381",
            "primary_allocation_port = 25566": "primary_allocation_port = 40366",
            "port = 25571": "port = 40371",
            "port_start = 25572": "port_start = 40372",
            "port_end = 25581": "port_end = 40381",
            "backend_port = 25565": "backend_port = 40365",
            "public_port = 25566": "public_port = 40366",
            "backend_port = 19431": "backend_port = 40331",
            "public_port = 25571": "public_port = 40371",
        }
        for old, new in replacements.items():
            config = config.replace(old, new)
        config_path = temp / "config" / "edge-fabric.ini"
        config_path.write_text(config, encoding="utf-8")
        config_path.chmod(0o600)

        environment = os.environ.copy()
        environment.update(
            {
                "NINJOS_ROOT_DIR": str(temp),
                "COMPANION_SHARED_SECRET": "test-companion-secret-123456789",
            }
        )
        process = subprocess.Popen(
            [str(temp / "start-runtime.sh")],
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            start_new_session=True,
        )
        try:
            output, _ = process.communicate(timeout=4)
        except subprocess.TimeoutExpired as error:
            output = error.stdout or ""
            if isinstance(output, bytes):
                output = output.decode("utf-8", errors="replace")
            os.killpg(process.pid, signal.SIGTERM)
            try:
                tail, _ = process.communicate(timeout=3)
                output += tail or ""
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                tail, _ = process.communicate()
                output += tail or ""
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=3)

        required = [
            " _   _ ___ _   _     _        ___  ____",
            r"|_| \_|___|_| \_|\___/        \___/|____/",
            "P R O X I E",
            "Verified Identity Gateway",
            "Version         : v7.3.5",
            "FIRST-RUN OWNER SETUP REQUIRED",
            "Setup code:",
            "Gateway Mode    : Universal dual-mode Bedrock edge",
            "Player Identity : Transparent native auth or signed full-proxy identity",
            "Engine          : Ninj-OS Edge Datagram Engine",
            "Implementation  : Ninj-OS protocol-agnostic transport core",
            "Reference       : ProxyPass by SculkCatalystMC",
            "Reference URL   : github.com/SculkCatalystMC/ProxyPass",
            "[Ninj-OS Proxie] Startup settings applied",
            "[Ninj-OS Proxie] Gateway ready",
            "Launching transparent, protocol-agnostic UDP relay",
        ]
        missing = [item for item in required if item not in output]
        if missing:
            raise AssertionError(f"Splash output missing {missing}. Output:\n{output}")
        if " _   _ _       _        ___  ____" in output:
            raise AssertionError(f"Legacy NINI-OS splash returned. Output:\n{output}")

        print("startup-splash-v7.3.5: PASS")


if __name__ == "__main__":
    main()

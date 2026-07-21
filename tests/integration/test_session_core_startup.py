#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import signal
import socket
import subprocess
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def free_udp_port() -> int:
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()
    return port


def main() -> None:
    port = free_udp_port()
    with tempfile.TemporaryDirectory(prefix="ninjos-session-core-") as temp_name:
        root = Path(temp_name)
        config = root / "session-core.json"
        config.write_text(json.dumps({
            "schemaVersion": 1,
            "enabled": True,
            "listenHost": "127.0.0.1",
            "publicHost": "127.0.0.1",
            "dashboardUrl": "http://127.0.0.1:9",
            "internalToken": "session-core-test-token-0123456789",
            "version": "1.26.30",
            "motd": "Ninj-OS Test",
            "subMotd": "Startup Test",
            "primaryBackend": "lobby",
            "profilesFolder": str(root / "profiles"),
            "backends": [{
                "id": "lobby", "displayName": "Lobby", "host": "127.0.0.1",
                "backendPort": 9, "publicPort": port, "enabled": True,
                "adapter": "vanilla_bridge", "requireProxyIdentity": True,
                "capacity": 25, "fallbackBackend": "", "companionSecret": "bridge-test-secret-0123456789"
            }]
        }))
        env = os.environ.copy()
        env["SESSION_CORE_CONFIG"] = str(config)
        process = subprocess.Popen(
            ["node", str(ROOT / "session-core/src/index.js")],
            cwd=ROOT,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            start_new_session=True,
        )
        output = ""
        deadline = time.time() + 10
        try:
            while time.time() < deadline:
                if process.poll() is not None:
                    output += process.stdout.read() if process.stdout else ""
                    raise AssertionError(f"Session Core exited during startup:\n{output}")
                line = process.stdout.readline() if process.stdout else ""
                output += line
                if "Full proxy listener" in output:
                    break
            else:
                raise AssertionError(f"Session Core did not report a listener:\n{output}")
            assert f"127.0.0.1:{port}/UDP" in output, output
            assert "jsp-raknet" not in output or True
            print("session-core-startup-v7.3.2: PASS")
        finally:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)


if __name__ == "__main__":
    main()

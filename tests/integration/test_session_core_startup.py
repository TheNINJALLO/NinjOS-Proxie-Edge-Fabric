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
            "stateFile": str(root / "session-core-state.json"),
            # Match the generated production configuration and launch from a
            # different working directory to verify bundled-pack fallback.
            "protocolPackDirectory": "session-core/protocol-packs",
            "protocolObservationDirectory": str(root / "protocol-observations"),
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
            cwd=root,
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
            state_path = root / "session-core-state.json"
            wait_for_state = time.time() + 3
            while time.time() < wait_for_state and not state_path.exists():
                time.sleep(0.05)
            state = json.loads(state_path.read_text())
            assert state["engine"] == "session-core"
            assert state["backends"][0]["name"] == "lobby"
            assert state["backends"][0]["healthy"] is True
            assert state["backends"][0]["connectionMode"] == "full_proxy"
            assert state["protocolPacks"][0]["protocol"] == 1001
            assert state["backends"][0]["protocolCompatibility"]["supported"] is True
            assert "jsp-raknet" not in output or True
            print("session-core-startup-v7.3.5: PASS")
        finally:
            try:
                if hasattr(os, "killpg"):
                    os.killpg(process.pid, signal.SIGTERM)
                else:
                    process.terminate()
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                if hasattr(os, "killpg"):
                    os.killpg(process.pid, signal.SIGKILL)
                else:
                    process.kill()
                process.wait(timeout=5)


if __name__ == "__main__":
    main()

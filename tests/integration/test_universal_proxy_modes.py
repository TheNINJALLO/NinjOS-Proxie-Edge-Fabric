#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BINARY = ROOT / "prebuilt/linux-x86_64/NinjOSDashboard"
PORT = 38771
OWNER_TOKEN = "owner-universal-mode-test-token"
SESSION_TOKEN = "session-core-internal-test-token-0123456789"
BRIDGE_SECRET = "lobby-bridge-secret-0123456789"


def http(path: str, method: str = "GET", body: dict | None = None, headers: dict | None = None):
    payload = None if body is None else json.dumps(body).encode()
    request_headers = dict(headers or {})
    if payload is not None:
        request_headers["Content-Type"] = "application/json"
    req = urllib.request.Request(f"http://127.0.0.1:{PORT}{path}", data=payload, method=method, headers=request_headers)
    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            return response.status, json.loads(response.read() or b"{}")
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read() or b"{}")


def wait_ready():
    deadline = time.time() + 12
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{PORT}/health/live", timeout=0.4) as response:
                if response.status == 200:
                    return
        except Exception:
            time.sleep(0.1)
    raise RuntimeError("dashboard did not start")


def main():
    with tempfile.TemporaryDirectory(prefix="ninjos-universal-") as temp_name:
        root = Path(temp_name)
        runtime = root / "runtime"
        runtime.mkdir()
        config = root / "edge-fabric.ini"
        text = (ROOT / "config/edge-fabric.example.ini").read_text().replace("185.83.152.144", "127.0.0.1")
        text = text.replace("enabled = false\nfallback = true\nprotection_profile = default\ncompanion_secret = env:COMPANION_LOBBY_SECRET", "enabled = true\nfallback = true\nprotection_profile = default\ncompanion_secret = env:COMPANION_LOBBY_SECRET")
        config.write_text(text)
        gateway = root / "gateway.conf"
        env = os.environ.copy()
        env.update({
            "RUNTIME_DIR": str(runtime),
            "EDGE_CONFIG_FILE": str(config),
            "GATEWAY_CONFIG_FILE": str(gateway),
            "DASHBOARD_PORT": str(PORT),
            "DASHBOARD_TOKEN": OWNER_TOKEN,
            "NINJOS_ENABLE_LEGACY_OWNER_TOKEN": "1",
            "SESSION_CORE_TOKEN": SESSION_TOKEN,
            "COMPANION_KINGDOM_SECRET": "kingdom-secret-0123456789",
            "COMPANION_ZOO_SECRET": "zoo-secret-0123456789",
            "COMPANION_LOBBY_SECRET": BRIDGE_SECRET,
            "COMPANION_SHARED_SECRET": "default-secret-0123456789",
        })
        subprocess.run([str(BINARY), "--prepare-config", str(config), str(runtime), str(gateway)], env=env, check=True)
        session_config = json.loads((runtime / "session-core.json").read_text())
        assert session_config["protocolCaptureEnabled"] is True
        assert session_config["protocolCaptureMode"] == "metadata"
        assert session_config["protocolCapturePacketIds"] == "30,77"
        assert session_config["protocolCaptureMaxPacketBytes"] == 65536
        assert session_config["protocolCaptureDecodeFailures"] is True
        assert session_config["protocolPackDirectory"] == "session-core/protocol-packs"
        assert [b["id"] for b in session_config["backends"]] == ["lobby"], session_config
        assert session_config["backends"][0]["adapter"] == "vanilla_bridge"
        assert "kingdom" in gateway.read_text() and "zoo" in gateway.read_text()
        assert "lobby" not in gateway.read_text()
        gateway_topology = runtime / "gateway-topology.properties"
        assert f"topology_file={gateway_topology}" in gateway.read_text()
        assert "kingdom" in gateway_topology.read_text() and "zoo" in gateway_topology.read_text()
        assert "lobby" not in gateway_topology.read_text()

        process = subprocess.Popen([str(BINARY)], env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        try:
            wait_ready()
            grant_request = {
                "sessionId": "proxy-session-one",
                "serverId": "lobby",
                "username": "TestPlayer",
                "xuid": "2530000000000001",
                "uuid": "00000000-0000-0000-0000-000000000001",
                "originalIp": "127.0.0.1",
                "expiresAt": int(time.time() * 1000) + 30000,
            }
            status, grant = http(
                "/api/session-core/v1/grants", "POST", grant_request,
                {"Authorization": f"Bearer {SESSION_TOKEN}"},
            )
            assert status == 201 and grant["serverId"] == "lobby", grant
            assert grant["role"] == "member"

            consume_headers = {"X-NinjOS-Server": "lobby", "X-NinjOS-Bridge-Token": BRIDGE_SECRET}
            status, consumed = http(
                "/api/bridge/v1/join/consume", "POST",
                {"serverId": "lobby", "username": "TestPlayer", "sessionId": "proxy-session-one"},
                consume_headers,
            )
            assert status == 200 and consumed["xuid"] == "2530000000000001", consumed
            status, second = http(
                "/api/bridge/v1/join/consume", "POST",
                {"serverId": "lobby", "username": "TestPlayer", "sessionId": "proxy-session-one"},
                consume_headers,
            )
            assert status == 404, second
            print("universal proxy modes and identity bridge test passed")
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill(); process.wait(timeout=5)


if __name__ == "__main__":
    main()

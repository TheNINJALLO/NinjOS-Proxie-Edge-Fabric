#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import hmac
import json
import os
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BINARY = ROOT / "prebuilt" / "linux-x86_64" / "NinjOSDashboard"
PORT = 36771
OWNER_TOKEN = "owner-config-companion-test-token"
COMPANION_SECRET = "kingdom-companion-secret-persistence-0123456789"


def request(path: str, method: str = "GET", body: dict | None = None, token: str = OWNER_TOKEN):
    payload = None if body is None else json.dumps(body).encode()
    headers = {}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if payload is not None:
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(
        f"http://127.0.0.1:{PORT}{path}", data=payload, method=method, headers=headers
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            raw = response.read()
            return response.status, json.loads(raw or b"{}")
    except urllib.error.HTTPError as error:
        raw = error.read()
        data = json.loads(raw or b"{}")
        return error.code, data


def wait_http(timeout: float = 12.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{PORT}/health/live", timeout=0.4) as response:
                if response.status == 200:
                    return
        except Exception:
            time.sleep(0.1)
    raise RuntimeError("dashboard did not become ready")


def signed_ingest(server_header: str, server_payload: str, secret: str):
    now = int(time.time() * 1000)
    body = json.dumps(
        {
            "serverId": server_payload,
            "records": [
                {
                    "type": "metrics",
                    "timestamp": now,
                    "companionVersion": "3.7.0",
                    "capabilitySchema": 1,
                    "capabilities": ["metrics", "presence", "transfer"],
                    "currentTps": 19.75,
                    "currentMspt": 17.5,
                    "onlinePlayers": 0,
                    "uploadFailures": 0,
                    "queueDepth": 0,
                }
            ],
        },
        separators=(",", ":"),
    ).encode()
    timestamp = str(now)
    signature = hmac.new(secret.encode(), timestamp.encode() + b"\n" + body, hashlib.sha256).hexdigest()
    req = urllib.request.Request(
        f"http://127.0.0.1:{PORT}/ingest",
        data=body,
        method="POST",
        headers={
            "Content-Type": "application/json",
            "X-NinjOS-Timestamp": timestamp,
            "X-NinjOS-Signature": signature,
            "X-NinjOS-Server": server_header,
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            return response.status, json.loads(response.read() or b"{}")
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read() or b"{}")


def main() -> None:
    if not BINARY.is_file():
        raise FileNotFoundError(BINARY)

    with tempfile.TemporaryDirectory(prefix="ninjos-config-companion-") as temp_name:
        root = Path(temp_name)
        runtime = root / "runtime"
        runtime.mkdir()
        config = root / "edge-fabric.ini"
        text = (ROOT / "config" / "edge-fabric.example.ini").read_text()
        text = text.replace("185.83.152.144", "127.0.0.1")
        config.write_text(text)
        gateway_config = root / "gateway.conf"

        environment = os.environ.copy()
        environment.update(
            {
                "RUNTIME_DIR": str(runtime),
                "EDGE_CONFIG_FILE": str(config),
                "GATEWAY_CONFIG_FILE": str(gateway_config),
                "DASHBOARD_PORT": str(PORT),
                "DASHBOARD_TOKEN": OWNER_TOKEN,
                "NINJOS_ENABLE_LEGACY_OWNER_TOKEN": "1",
            }
        )
        for name in (
            "COMPANION_SHARED_SECRET",
            "COMPANION_KINGDOM_SECRET",
            "COMPANION_ZOO_SECRET",
        ):
            environment.pop(name, None)

        process = subprocess.Popen(
            [str(BINARY)],
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
        try:
            wait_http()

            # Empty environment-backed secrets must be rejected instead of appearing saved.
            status, result = request(
                "/api/secrets",
                "PUT",
                {
                    "id": "backend.kingdom.companion_secret",
                    "mode": "environment",
                    "environmentVariable": "COMPANION_KINGDOM_SECRET",
                },
            )
            assert status == 400, result
            assert "is empty" in result.get("error", ""), result

            status, result = request(
                "/api/secrets",
                "PUT",
                {
                    "id": "backend.kingdom.companion_secret",
                    "mode": "dashboard",
                    "value": "eleven-char",
                },
            )
            assert status == 400, result
            assert "at least 12 characters" in result.get("error", ""), result

            status, result = request(
                "/api/secrets",
                "PUT",
                {
                    "id": "backend.kingdom.companion_secret",
                    "mode": "dashboard",
                    "value": "123456789012",
                },
            )
            assert status == 202, result

            # Dashboard-managed save must survive a read-back and expose a matching revision.
            status, saved = request(
                "/api/secrets",
                "PUT",
                {
                    "id": "backend.kingdom.companion_secret",
                    "mode": "dashboard",
                    "value": COMPANION_SECRET,
                },
            )
            assert status == 202 and saved.get("saved") is True, saved
            revision = saved.get("revision")
            assert revision and len(revision) == 64, saved

            status, unified = request("/api/unified-config")
            assert status == 200 and unified.get("revision") == revision, unified
            assert COMPANION_SECRET not in unified.get("content", "")
            assert "companion_secret = [REDACTED]" in unified.get("content", "")
            assert COMPANION_SECRET in config.read_text()

            save_status = json.loads((runtime / "config-save-status.json").read_text())
            assert save_status["revision"] == revision, save_status

            # The advanced editor cannot replace a Vault-managed credential.
            tampered_content = unified["content"].replace(
                "companion_secret = [REDACTED]",
                "companion_secret = should-not-replace-vault-secret",
            )
            status, result = request(
                "/api/unified-config", "PUT", {"content": tampered_content}
            )
            assert status == 202, result
            assert COMPANION_SECRET in config.read_text()
            assert "should-not-replace-vault-secret" not in config.read_text()

            # Generated properties and manager fingerprint must use the same effective secret.
            raw_request = urllib.request.Request(
                f"http://127.0.0.1:{PORT}/api/companion-download?type=properties&backend=kingdom",
                headers={"Authorization": f"Bearer {OWNER_TOKEN}"},
            )
            with urllib.request.urlopen(raw_request, timeout=5) as response:
                properties = response.read().decode()
            assert f"shared_secret={COMPANION_SECRET}" in properties
            assert "server_id=kingdom" in properties
            fingerprint = hashlib.sha256(COMPANION_SECRET.encode()).hexdigest()[:12].upper()
            assert f"Effective secret fingerprint: {fingerprint}" in properties

            status, manager = request("/api/companion-manager")
            assert status == 200, manager
            kingdom = next(item for item in manager["backends"] if item["id"] == "kingdom")
            assert kingdom["configured"] is True
            assert kingdom["secretFingerprint"] == fingerprint
            assert kingdom["connection"]["connected"] is False

            # Mixed-case header/payload IDs must normalize to the configured backend ID.
            status, accepted = signed_ingest("KingDom", "KINGDOM", COMPANION_SECRET)
            assert status == 200 and accepted.get("accepted") == 1, accepted
            assert accepted.get("serverId") == "kingdom", accepted
            assert accepted.get("secretFingerprint") == fingerprint, accepted

            status, manager = request("/api/companion-manager")
            kingdom = next(item for item in manager["backends"] if item["id"] == "kingdom")
            connection = kingdom["connection"]
            assert connection["connected"] is True, connection
            assert float(connection["currentTps"]) == 19.75, connection
            assert float(connection["currentMspt"]) == 17.5, connection

            # Wrong secrets must remain rejected.
            status, rejected = signed_ingest("kingdom", "kingdom", "wrong-secret-value-0123456789")
            assert status == 401, rejected

            print("config and companion persistence test passed")
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            if process.stdout:
                output = process.stdout.read()
                if process.returncode not in (0, -15):
                    print(output)


if __name__ == "__main__":
    main()

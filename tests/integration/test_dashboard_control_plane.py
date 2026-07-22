#!/usr/bin/env python3
from __future__ import annotations

import base64
import hashlib
import hmac
import io
import json
import os
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

BINARY = Path(os.environ.get("NINJOS_DASHBOARD_BINARY", ROOT / "prebuilt/linux-x86_64/NinjOSDashboard")).resolve()
PORT = 34265
OWNER_TOKEN = "owner-token-v65"
TOTP_SECRET = "JBSWY3DPEHPK3PXP"
COMPANION_SECRET = "companion-secret-v65"


def totp(secret: str) -> str:
    key = base64.b32decode(secret)
    counter = int(time.time() // 30).to_bytes(8, "big")
    digest = hmac.new(key, counter, hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    number = (
        ((digest[offset] & 0x7F) << 24)
        | (digest[offset + 1] << 16)
        | (digest[offset + 2] << 8)
        | digest[offset + 3]
    )
    return f"{number % 1_000_000:06d}"


def request(path: str, method: str = "GET", body=None, token: str = "", headers=None):
    payload = None
    request_headers = dict(headers or {})
    if body is not None:
        payload = json.dumps(body).encode()
        request_headers["Content-Type"] = "application/json"
    if token:
        request_headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(f"http://127.0.0.1:{PORT}{path}", data=payload, method=method, headers=request_headers)
    with urllib.request.urlopen(req, timeout=4) as response:
        raw = response.read()
        if response.headers.get("Content-Type", "").startswith("application/json"):
            return response.status, json.loads(raw)
        return response.status, raw


def upload_artifact(path: str, token: str, filename: str, data: bytes):
    boundary = "----NinjOSEdgeFabricBoundary"
    body = bytearray()
    body.extend(f"--{boundary}\r\n".encode())
    body.extend(
        f'Content-Disposition: form-data; name="artifact"; filename="{filename}"\r\n'.encode()
    )
    body.extend(b"Content-Type: application/octet-stream\r\n\r\n")
    body.extend(data)
    body.extend(f"\r\n--{boundary}--\r\n".encode())
    request_object = urllib.request.Request(
        f"http://127.0.0.1:{PORT}{path}",
        data=bytes(body),
        method="POST",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": f"multipart/form-data; boundary={boundary}",
        },
    )
    with urllib.request.urlopen(request_object, timeout=5) as response:
        return response.status, json.loads(response.read())


def signed_ingest(server: str, records: list[dict], secret: str = COMPANION_SECRET):
    body = json.dumps({"serverId": server, "records": records}, separators=(",", ":")).encode()
    timestamp = str(int(time.time() * 1000))
    signature = hmac.new(
        secret.encode(),
        timestamp.encode() + b"\n" + body,
        hashlib.sha256,
    ).hexdigest()
    req = urllib.request.Request(
        f"http://127.0.0.1:{PORT}/ingest",
        data=body,
        method="POST",
        headers={
            "Content-Type": "application/json",
            "X-NinjOS-Timestamp": timestamp,
            "X-NinjOS-Signature": signature,
            "X-NinjOS-Server": server,
        },
    )
    with urllib.request.urlopen(req, timeout=4) as response:
        return json.loads(response.read())


def wait_for(predicate, timeout=8):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            last = predicate()
            if last:
                return last
        except Exception as error:
            last = error
        time.sleep(0.2)
    raise AssertionError(f"condition not met, last={last}")


def main() -> None:
    if not BINARY.is_file():
        raise FileNotFoundError(BINARY)

    with tempfile.TemporaryDirectory(prefix="ninjos-dashboard-control-") as temp_name:
        runtime = Path(temp_name)
        config_path = runtime / "edge-fabric.ini"
        config_path.write_text((ROOT / "config/edge-fabric.example.ini").read_text())
        gateway_state = {
            "timestamp": int(time.time() * 1000),
            "activeSessions": 2,
            "trackedIps": 2,
            "routingMode": "primary",
            "incident": {"active": False, "packetsPerSecond": 25, "dropRatio": 0.01},
            "counters": {"droppedPackets": 1, "rateLimited": 1, "temporaryBans": 0},
            "firewall": {"topRisk": [], "activeBans": 0},
            "backends": [
                {"name": "kingdom", "healthy": True, "enabled": True, "host": "127.0.0.1", "port": 25565},
            ],
        }
        (runtime / "gateway-state.json").write_text(json.dumps(gateway_state))
        (runtime / "session-core-state.json").write_text(json.dumps({
            "timestamp": int(time.time() * 1000),
            "engine": "session-core",
            "backends": [{
                "name": "zoo", "healthy": True, "enabled": True,
                "connectionMode": "full_proxy", "activeSessions": 0,
            }],
        }))
        (runtime / "companion-state-zoo.json").write_text(json.dumps({
            "timestamp": int(time.time() * 1000),
            "serverId": "zoo",
            "metrics": {"uploadFailures": 9, "queueDepth": 0},
        }))
        (runtime / "sessions.json").write_text("[]")
        (runtime / "health-actions.properties").write_text("enabled=false\n")
        protocol_dir = runtime / "protocol-observations" / "1001"
        protocol_dir.mkdir(parents=True)
        (protocol_dir / "zoo.jsonl").write_text(json.dumps({
            "timestamp": int(time.time() * 1000),
            "recordId": "fixture-protocol-record",
            "type": "protocol_packet",
            "layer": "protocol",
            "protocol": 1001,
            "backend": "zoo",
            "direction": "clientbound",
            "packetId": 77,
            "packetName": "fixture_packet",
            "bytes": 3,
            "action": "forward",
            "captureTiers": ["metadata", "decoded", "wire", "round_trip"],
            "decoded": {"safe": True},
            "wire": {"encoding": "hex", "data": "4d0102", "capturedBytes": 3, "originalBytes": 3, "truncated": False},
            "roundTrip": {"attempted": True, "exact": True, "mismatchOffset": -1},
        }) + "\n")

        environment = os.environ.copy()
        environment.update(
            {
                "RUNTIME_DIR": str(runtime),
                "EDGE_CONFIG_FILE": str(config_path),
                "GATEWAY_CONFIG_FILE": str(runtime / "gateway.conf"),
                "DASHBOARD_PORT": str(PORT),
                "DASHBOARD_TOKEN": OWNER_TOKEN,
                "NINJOS_ENABLE_LEGACY_OWNER_TOKEN": "1",
                "DASHBOARD_TOTP_SECRET": TOTP_SECRET,
                "COMPANION_SHARED_SECRET": COMPANION_SECRET,
                "TRANSFER_PUBLIC_HOST": "127.0.0.1",
                "TRANSFER_PORT_START": "35272",
                "TRANSFER_PORT_END": "35281",
                "TRANSFER_REQUIRE_SOURCE_IP": "0",
                "METRIC_SAMPLE_SECONDS": "1",
                "METRIC_RETENTION_DAYS": "1",
                "DASHBOARD_SESSION_MINUTES": "60",
            }
        )
        process = subprocess.Popen([str(BINARY)], env=environment, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        try:
            wait_for(lambda: request("/health/live")[0] == 200)

            try:
                request("/api/login", "POST", {"username": "owner", "token": OWNER_TOKEN})
                raise AssertionError("TOTP-free login unexpectedly succeeded")
            except urllib.error.HTTPError as error:
                assert error.code == 401

            _, login = request(
                "/api/login",
                "POST",
                {"username": "owner", "token": OWNER_TOKEN, "totp": totp(TOTP_SECRET)},
            )
            session = login["token"]

            _, protocol_packets = request(
                "/api/packets?layer=protocol&tier=wire&packetId=77&details=1", token=session
            )
            assert protocol_packets["count"] == 1, protocol_packets
            assert protocol_packets["records"][0]["decoded"]["safe"] is True
            assert protocol_packets["tiers"]["round_trip"] == 1

            _, state = request("/api/state", token=session)
            assert state["version"] == "7.3.4"
            assert state["management"]["sqliteLedger"] is True
            zoo_health = next(item for item in state["gateway"]["backends"] if item["name"] == "zoo")
            assert zoo_health["healthy"] is True
            zoo_companion_state = next(item for item in state["companions"] if item["serverId"] == "zoo")
            assert zoo_companion_state["health"] == "healthy"

            _, managed_settings = request("/api/settings", token=session)
            assert any(item["id"] == "companion.capture_mode" for item in managed_settings["settings"])
            status, managed_result = request(
                "/api/settings",
                "PUT",
                {"values": {"companion.capture_mode": "selected", "companion.payload_limit": "1024"}},
                session,
            )
            assert status == 202 and managed_result["updated"] == 2

            backend_secret = "zoo-dashboard-managed-secret-0123456789"
            status, secret_result = request(
                "/api/secrets",
                "PUT",
                {
                    "id": "backend.zoo.companion_secret",
                    "mode": "dashboard",
                    "value": backend_secret,
                },
                session,
            )
            assert status == 202
            assert secret_result["secret"]["configured"] is True
            assert backend_secret not in json.dumps(secret_result)

            _, secrets = request("/api/secrets", token=session)
            zoo_secret = next(item for item in secrets["secrets"] if item["id"] == "backend.zoo.companion_secret")
            assert zoo_secret["mode"] == "dashboard"
            assert zoo_secret["configured"] is True
            assert zoo_secret["fingerprint"]
            assert backend_secret not in json.dumps(secrets)

            _, unified_config = request("/api/unified-config", token=session)
            assert "companion_secret = [REDACTED]" in unified_config["content"]
            assert backend_secret not in unified_config["content"]

            _, companion_manager = request("/api/companion-manager", token=session)
            zoo_companion = next(item for item in companion_manager["backends"] if item["id"] == "zoo")
            assert zoo_companion["configured"] is True
            assert "shared_secret=[CONFIGURED]" in zoo_companion["preview"]
            assert backend_secret not in zoo_companion["preview"]

            status, companion_properties = request(
                "/api/companion-download?type=properties&backend=zoo", token=session
            )
            assert status == 200
            assert f"shared_secret={backend_secret}".encode() in companion_properties
            assert b"server_id=zoo" in companion_properties
            assert b"capture_mode=selected" in companion_properties
            assert b"payload_limit=1024" in companion_properties

            fake_elf = bytearray(128)
            fake_elf[0:4] = b"\x7fELF"
            fake_elf[4] = 2
            fake_elf[5] = 1
            fake_elf[18:20] = (62).to_bytes(2, "little")
            status, artifact_status = upload_artifact(
                "/api/companion-manager", session, "ninjos_proxie_companion.so", bytes(fake_elf)
            )
            assert status == 201 and artifact_status["compiledAvailable"] is True

            status, install_package = request(
                "/api/companion-download?type=package&backend=zoo", token=session
            )
            assert status == 200
            install_zip = zipfile.ZipFile(io.BytesIO(install_package))
            assert "plugins/ninjos_proxie_companion.so" in install_zip.namelist()
            generated_properties = install_zip.read(
                "plugins/ninjos_proxie_companion/companion.properties"
            )
            assert f"shared_secret={backend_secret}".encode() in generated_properties
            assert b"server_id=zoo" in generated_properties

            profile_content = (
                "default.max_datagram_size=2048\n"
                "default.max_packets_per_second_per_ip=6000\n"
                "default.max_handshakes_per_minute=30\n"
                "default.max_sessions_per_ip=4\n"
                "default.allow_new_sessions_during_incident=false\n"
                "zoo.max_packets_per_second_per_ip=4000\n"
                "zoo.max_handshakes_per_minute=20\n"
                "zoo.max_sessions_per_ip=3\n"
            )
            status, applied = request(
                "/api/protection-profiles",
                "POST",
                {"content": profile_content},
                session,
            )
            assert status == 202 and applied["queued"] is True

            status, ticket_response = request(
                "/api/transfers",
                "POST",
                {
                    "destination": "zoo",
                    "sourceIp": "127.0.0.4",
                    "xuid": "2535000000000001",
                    "playerName": "TheN1NJ4LL0",
                    "sourceServer": "kingdom",
                },
                session,
            )
            assert status == 201
            ticket = ticket_response["ticket"]
            ticket_id = ticket["ticketId"]

            def transaction_state(expected):
                _, payload = request("/api/transfer-transactions?limit=20", token=session)
                for row in payload["transactions"]:
                    if row["ticket_id"] == ticket_id and row["state"] == expected:
                        return row
                return None

            assert transaction_state("ticketed")
            with (runtime / "events.jsonl").open("a") as handle:
                handle.write(json.dumps({
                    "timestamp": int(time.time() * 1000),
                    "type": "transfer.ticket_consumed",
                    "message": f"Transfer ticket consumed ticket={ticket_id} player=TheN1NJ4LL0 destination=zoo",
                }) + "\n")
            wait_for(lambda: transaction_state("proxy_connected"))

            now = int(time.time() * 1000)
            accepted = signed_ingest("zoo", [{
                "type": "metrics",
                "timestamp": now,
                "companionVersion": "3.5.0",
                "capabilitySchema": 1,
                "capabilities": ["metrics", "presence", "transfer", "arrival_confirmation"],
                "currentTps": 20.0,
                "currentMspt": 10.0,
                "onlinePlayers": 1,
                "players": [{"playerName": "TheN1NJ4LL0", "xuid": "2535000000000001"}],
            }], backend_secret)
            assert accepted["accepted"] >= 1
            wait_for(lambda: transaction_state("arrived"))

            _, profiles = request("/api/profiles", token=session)
            assert any(row["xuid"] == "2535000000000001" and row["current_server"] == "zoo" for row in profiles["profiles"])

            _, capabilities = request("/api/capabilities", token=session)
            assert capabilities["fleet"][0]["compatible"] is True

            time.sleep(1.5)
            _, history = request("/api/history?name=gateway.active_sessions&minutes=5", token=session)
            assert len(history["samples"]) >= 1

            status, support = request("/api/support-bundle", "POST", {}, session)
            assert status == 200
            archive = zipfile.ZipFile(io.BytesIO(support))
            assert "gateway-state.json" in archive.namelist()
            joined = b"\n".join(archive.read(name) for name in archive.namelist())
            assert OWNER_TOKEN.encode() not in joined
            assert COMPANION_SECRET.encode() not in joined
            assert backend_secret.encode() not in joined

            db_path = runtime / "edge-fabric.db"
            assert db_path.is_file()
            with sqlite3.connect(db_path) as database:
                count = database.execute("SELECT COUNT(*) FROM transfer_transactions").fetchone()[0]
                assert count >= 1
                audit_count = database.execute("SELECT COUNT(*) FROM audit_log").fetchone()[0]
                assert audit_count >= 1

            print("dashboard-v7.3.4: PASS")
        finally:
            process.terminate()
            try:
                process.wait(timeout=4)
            except subprocess.TimeoutExpired:
                process.kill()


if __name__ == "__main__":
    main()

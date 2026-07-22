#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import shutil
import signal
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def wait_http(port: int, timeout: float = 12.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{port}/health/live", timeout=0.4) as response:
                if response.status == 200:
                    return
        except Exception:
            time.sleep(0.1)
    raise RuntimeError("dashboard did not become ready")


def call_api(port: int, token: str, path: str, method: str = "GET", body: dict | None = None):
    payload = None if body is None else json.dumps(body).encode()
    headers = {"Authorization": f"Bearer {token}"}
    if payload is not None:
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(
        f"http://127.0.0.1:{port}{path}",
        data=payload,
        method=method,
        headers=headers,
    )
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, json.loads(response.read() or b"{}")
    except urllib.error.HTTPError as error:
        raise RuntimeError(error.read().decode()) from error




def dashboard_child_pid(supervisor_pid: int) -> int:
    output = subprocess.check_output(["pgrep", "-P", str(supervisor_pid), "NinjOSDashboard"], text=True).strip().splitlines()
    if not output:
        raise RuntimeError("dashboard child process was not found")
    return int(output[0])


def session_core_child_pid(supervisor_pid: int) -> int:
    output = subprocess.check_output(["pgrep", "-P", str(supervisor_pid), "node"], text=True).strip().splitlines()
    if not output:
        raise RuntimeError("Session Core child process was not found")
    return int(output[0])

def udp_backend(port: int, prefix: bytes, stop: threading.Event) -> None:
    server = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    server.bind(("127.0.0.1", port))
    server.settimeout(0.2)
    try:
        while not stop.is_set():
            try:
                payload, address = server.recvfrom(4096)
            except socket.timeout:
                continue
            server.sendto(prefix + payload, address)
    finally:
        server.close()


def udp_request(port: int, payload: bytes, timeout: float = 0.5) -> bytes:
    client = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    client.bind(("127.0.0.5", 0))
    client.settimeout(timeout)
    try:
        client.sendto(payload, ("127.0.0.1", port))
        return client.recvfrom(4096)[0]
    finally:
        client.close()


def wait_udp(port: int, expected: bytes, timeout: float = 10.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            if udp_request(port, b"probe") == expected:
                return
        except Exception:
            time.sleep(0.15)
    raise RuntimeError(f"UDP route {port} did not become ready")


def build_test_config(destination: Path) -> None:
    text = (ROOT / "config" / "edge-fabric.example.ini").read_text()
    text = text.replace(
        "public_port = 19134\nenabled = false\nfallback = true",
        "public_port = 36274\nenabled = true\nfallback = true",
    )
    replacements = [
        ("185.83.152.144", "127.0.0.1"),
        ("25566", "36266"),
        ("25571", "36271"),
        ("25572", "36272"),
        ("25581", "36281"),
        ("25565", "36165"),
        ("19431", "36131"),
    ]
    for old, new in replacements:
        text = text.replace(old, new)
    text = text.replace("port = 36271\npublic_host", "port = 36471\npublic_host")
    text = text.replace(
        "managed_public_udp_ports = 36266,36271,36272-36281",
        "managed_public_udp_ports = 36266,36271,36272-36281,36282",
    )
    destination.write_text(text)


def main() -> None:
    temporary_root = Path(tempfile.mkdtemp(prefix="ninjos-v675-integration-"))
    stop = threading.Event()
    process: subprocess.Popen | None = None

    try:
        for name in ("NinjOSEdge", "NinjOSDashboard"):
            source = ROOT / "prebuilt" / "linux-x86_64" / name
            if not source.is_file():
                raise FileNotFoundError(f"build the release binaries before running tests: {source}")
            shutil.copy2(source, temporary_root / name)
            os.chmod(temporary_root / name, 0o755)
        shutil.copy2(ROOT / "start-runtime.sh", temporary_root / "start-runtime.sh")
        os.chmod(temporary_root / "start-runtime.sh", 0o755)
        shutil.copy2(ROOT / "config" / "edge-fabric.example.ini", temporary_root / "edge-fabric.example.ini")
        os.symlink(ROOT / "session-core", temporary_root / "session-core", target_is_directory=True)

        config_dir = temporary_root / "config"
        config_dir.mkdir()
        build_test_config(config_dir / "edge-fabric.ini")

        for port, prefix in ((36165, b"K:"), (36131, b"Z:"), (36182, b"R:"), (36183, b"C:")):
            threading.Thread(target=udp_backend, args=(port, prefix, stop), daemon=True).start()

        environment = os.environ.copy()
        environment.update(
            {
                "NINJOS_ROOT_DIR": str(temporary_root),
                "DASHBOARD_TOKEN": "owner-v675-test-1",
                "NINJOS_ENABLE_LEGACY_OWNER_TOKEN": "1",
                "COMPANION_SHARED_SECRET": "companion-v675-test",
                "COMPANION_REDSTONE_SECRET": "redstone-companion-secret-0123456789",
            }
        )
        process = subprocess.Popen(
            [str(temporary_root / "start-runtime.sh")],
            cwd=temporary_root,
            env=environment,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )

        wait_http(36471)
        wait_udp(36266, b"K:probe")
        dashboard_pid_before = dashboard_child_pid(process.pid)
        session_core_pid_before = session_core_child_pid(process.pid)

        _, registry = call_api(36471, "owner-v675-test-1", "/api/backend-registry")
        assert {backend["id"] for backend in registry["topology"]["backends"]} == {"kingdom", "zoo", "lobby"}

        backend = {
            "id": "redstone",
            "displayName": "Redstone",
            "host": "127.0.0.1",
            "backendPort": 36182,
            "publicPort": 36272,
            "fallback": False,
            "profile": "redstone",
            "enabled": True,
            "companionSecretEnv": "COMPANION_REDSTONE_SECRET",
        }
        status, response = call_api(36471, "owner-v675-test-1", "/api/backend-registry", "POST", backend)
        assert status == 201 and response["saved"] is True

        status, response = call_api(
            36471,
            "owner-v675-test-1",
            "/api/secrets",
            "PUT",
            {
                "id": "backend.redstone.companion_secret",
                "mode": "environment",
                "environmentVariable": "COMPANION_REDSTONE_SECRET",
            },
        )
        assert status == 202 and response["saved"] is True

        time.sleep(1)
        wait_http(36471)
        wait_udp(36272, b"R:probe")
        assert dashboard_child_pid(process.pid) == dashboard_pid_before, "backend save restarted the dashboard"
        session_core_pid_after = session_core_child_pid(process.pid)
        assert session_core_pid_after != session_core_pid_before, "backend save did not restart Session Core"

        _, registry = call_api(36471, "owner-v675-test-1", "/api/backend-registry")
        assert 36272 not in registry["availableTransferPorts"]
        assert 36273 in registry["availableTransferPorts"]

        deadline = time.time() + 8
        while time.time() < deadline:
            try:
                _, state = call_api(36471, "owner-v675-test-1", "/api/state")
                backend_names = [item.get("name") for item in state.get("gateway", {}).get("backends", [])]
                if "zoo" in backend_names:
                    break
            except Exception:
                pass
            time.sleep(0.2)
        else:
            raise RuntimeError("gateway state did not repopulate after topology restart")

        status, transfer = call_api(
            36471,
            "owner-v675-test-1",
            "/api/transfers",
            "POST",
            {
                "destination": "zoo",
                "sourceIp": "127.0.0.5",
                "xuid": "2535000000000675",
                "playerName": "PoolTest",
                "sourceServer": "kingdom",
            },
        )
        assert status == 201
        assert transfer["ticket"]["port"] == 36273, transfer

        _, unified = call_api(36471, "owner-v675-test-1", "/api/unified-config")
        edited_content = unified["content"].replace(
            "display_name = Redstone\n",
            "display_name = Redstone Config Save\n",
            1,
        )
        status, saved_config = call_api(
            36471,
            "owner-v675-test-1",
            "/api/unified-config",
            "PUT",
            {"content": edited_content},
        )
        assert status == 202 and saved_config["saved"] is True
        assert saved_config["restartScope"] == "gateway"
        time.sleep(1)
        assert dashboard_child_pid(process.pid) == dashboard_pid_before
        _, registry = call_api(36471, "owner-v675-test-1", "/api/backend-registry")
        raw_saved = next(item for item in registry["topology"]["backends"] if item["id"] == "redstone")
        assert raw_saved["displayName"] == "Redstone Config Save"

        updated = dict(backend)
        updated["displayName"] = "Redstone Network"
        updated["fallback"] = True
        status, response = call_api(
            36471,
            "owner-v675-test-1",
            "/api/backend-registry?id=redstone",
            "PUT",
            updated,
        )
        assert status == 200 and response["saved"] is True
        _, registry = call_api(36471, "owner-v675-test-1", "/api/backend-registry")
        redstone = next(item for item in registry["topology"]["backends"] if item["id"] == "redstone")
        assert redstone["displayName"] == "Redstone Network"
        assert redstone["fallback"] is True
        assert dashboard_child_pid(process.pid) == dashboard_pid_before

        renamed = dict(updated)
        renamed["id"] = "redstone-net"
        status, response = call_api(
            36471,
            "owner-v675-test-1",
            "/api/backend-registry?id=redstone",
            "PUT",
            renamed,
        )
        assert status == 200 and response["saved"] is True
        _, registry = call_api(36471, "owner-v675-test-1", "/api/backend-registry")
        assert "redstone-net" in [item["id"] for item in registry["topology"]["backends"]]
        assert "redstone" not in [item["id"] for item in registry["topology"]["backends"]]

        creative = {
            "id": "creative",
            "displayName": "Creative",
            "host": "127.0.0.1",
            "backendPort": 36183,
            "publicPort": 0,
            "fallback": False,
            "profile": "default",
            "enabled": True,
            "companionSecretEnv": "",
        }
        status, response = call_api(36471, "owner-v675-test-1", "/api/backend-registry", "POST", creative)
        assert status == 201 and response["saved"] is True
        _, registry = call_api(36471, "owner-v675-test-1", "/api/backend-registry")
        assert len(registry["topology"]["backends"]) == 5
        assert dashboard_child_pid(process.pid) == dashboard_pid_before

        _, unified = call_api(36471, "owner-v675-test-1", "/api/unified-config")
        assert "[backend.redstone-net]" in unified["content"]
        assert "[backend.creative]" in unified["content"]

        _, sources = call_api(36471, "owner-v675-test-1", "/api/security-sources")
        assert any(
            item.get("environmentVariable") == "COMPANION_REDSTONE_SECRET"
            for item in sources["sources"]
        )

        status, _ = call_api(
            36471,
            "owner-v675-test-1",
            "/api/backend-registry?id=redstone-net",
            "DELETE",
        )
        assert status == 200

        print("unified-management-integration: PASS")
    finally:
        stop.set()
        if process is not None:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
        shutil.rmtree(temporary_root, ignore_errors=True)


if __name__ == "__main__":
    main()

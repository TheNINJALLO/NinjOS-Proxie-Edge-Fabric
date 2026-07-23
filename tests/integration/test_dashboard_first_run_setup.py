#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import signal
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PORT = 40671


def request(path: str, method: str = "GET", body: dict | None = None, token: str = ""):
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(f"http://127.0.0.1:{PORT}{path}", data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            payload = response.read().decode()
            return response.status, json.loads(payload) if payload else {}
    except urllib.error.HTTPError as error:
        payload = error.read().decode()
        try:
            parsed = json.loads(payload)
        except json.JSONDecodeError:
            parsed = {"error": payload}
        return error.code, parsed


def wait_ready(process: subprocess.Popen[str]) -> None:
    for _ in range(80):
        if process.poll() is not None:
            raise AssertionError("Dashboard exited during startup")
        try:
            status, _ = request("/api/setup/status")
            if status == 200:
                return
        except Exception:
            pass
        time.sleep(0.1)
    raise AssertionError("Dashboard did not become ready")


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="ninjos-first-run-") as temp_name:
        temp = Path(temp_name)
        runtime = temp / "runtime"
        config = temp / "config"
        runtime.mkdir()
        config.mkdir()
        config_path = config / "edge-fabric.ini"
        config_path.write_text((ROOT / "config" / "edge-fabric.example.ini").read_text().replace("port = 25571", f"port = {PORT}"), encoding="utf-8")

        environment = os.environ.copy()
        environment.update({
            "RUNTIME_DIR": str(runtime),
            "EDGE_CONFIG_FILE": str(config_path),
            "GATEWAY_CONFIG_FILE": str(temp / "gateway.conf"),
            "DASHBOARD_PORT": str(PORT),
            "DASHBOARD_PUBLIC_HOST": "127.0.0.1",
            "COMPANION_SHARED_SECRET": "first-run-companion-secret-12345",
            "DASHBOARD_RECOVERY_TOKEN": "recovery-password-123456789",
        })
        environment.pop("DASHBOARD_TOKEN", None)
        process = subprocess.Popen(
            [str(ROOT / "prebuilt" / "linux-x86_64" / "NinjOSDashboard")],
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            start_new_session=True,
        )
        try:
            wait_ready(process)
            status, setup = request("/api/setup/status")
            assert status == 200 and setup["setupRequired"] is True
            setup_file = runtime / "FIRST_RUN_SETUP.txt"
            content = setup_file.read_text(encoding="utf-8")
            code = next(line.split(": ", 1)[1] for line in content.splitlines() if line.startswith("Setup code:"))

            status, failed = request("/api/login", "POST", {"username": "owner", "password": "anything"})
            assert status == 409 and failed.get("setupRequired") is True

            status, failed = request("/api/setup", "POST", {
                "setupCode": "wrong-code-123456789",
                "username": "NinjOwner",
                "password": "A-strong-password-1234",
            })
            assert status == 401

            status, created = request("/api/setup", "POST", {
                "setupCode": code,
                "username": "NinjOwner",
                "password": "A-strong-password-1234",
            })
            assert status == 201, created
            assert created["principal"]["username"] == "NinjOwner"
            assert not setup_file.exists()
            first_session = created["token"]

            status, who = request("/api/whoami", token=first_session)
            assert status == 200 and who["username"] == "NinjOwner"

            status, failed = request("/api/login", "POST", {"username": "NinjOwner", "password": "bad-password"})
            assert status == 401
            status, login = request("/api/login", "POST", {"username": "NinjOwner", "password": "A-strong-password-1234"})
            assert status == 200, login

            status, changed = request("/api/account", "PUT", {
                "currentPassword": "A-strong-password-1234",
                "username": "KingdomOwner",
                "password": "A-new-strong-password-5678",
            }, login["token"])
            assert status == 200, changed

            status, old_login = request("/api/login", "POST", {"username": "NinjOwner", "password": "A-strong-password-1234"})
            assert status == 401
            status, new_login = request("/api/login", "POST", {"username": "KingdomOwner", "password": "A-new-strong-password-5678"})
            assert status == 200, new_login

            status, recovery = request("/api/login", "POST", {"username": "recovery", "password": "recovery-password-123456789"})
            assert status == 200 and recovery.get("recovery") is True
            status, recovered = request("/api/account", "PUT", {
                "currentPassword": "",
                "username": "RecoveredOwner",
                "password": "Recovered-password-9012",
            }, recovery["token"])
            assert status == 200, recovered
            status, final_login = request("/api/login", "POST", {"username": "RecoveredOwner", "password": "Recovered-password-9012"})
            assert status == 200, final_login

            owner_token = final_login["token"]
            status, team_account = request("/api/users", "POST", {
                "username": "NightOperator",
                "password": "Operator-password-3456",
                "role": "operator",
            }, owner_token)
            assert status == 201, team_account
            assert team_account["role"] == "operator" and team_account["enabled"] is True
            assert "passwordHash" not in team_account and "tokenHash" not in team_account

            status, team = request("/api/users", token=owner_token)
            assert status == 200, team
            listed = next(user for user in team["users"] if user["username"] == "NightOperator")
            assert listed["authentication"] == "password" and listed["managed"] is False

            status, operator_login = request("/api/login", "POST", {
                "username": "NightOperator",
                "password": "Operator-password-3456",
            })
            assert status == 200 and operator_login["principal"]["role"] == "operator"
            status, denied = request("/api/users", token=operator_login["token"])
            assert status == 403, denied

            status, disabled = request("/api/users", "PUT", {
                "username": "NightOperator",
                "role": "viewer",
                "enabled": False,
                "password": "Viewer-password-7890",
            }, owner_token)
            assert status == 200 and disabled["enabled"] is False
            status, revoked = request("/api/whoami", token=operator_login["token"])
            assert status == 401, revoked
            status, disabled_login = request("/api/login", "POST", {
                "username": "NightOperator",
                "password": "Viewer-password-7890",
            })
            assert status == 401, disabled_login

            status, enabled = request("/api/users", "PUT", {
                "username": "NightOperator",
                "role": "viewer",
                "enabled": True,
            }, owner_token)
            assert status == 200 and enabled["role"] == "viewer"
            status, viewer_login = request("/api/login", "POST", {
                "username": "NightOperator",
                "password": "Viewer-password-7890",
            })
            assert status == 200 and viewer_login["principal"]["role"] == "viewer"

            status, removed = request("/api/users", "DELETE", {
                "username": "NightOperator",
            }, owner_token)
            assert status == 200 and removed["deleted"] is True
            status, removed_login = request("/api/login", "POST", {
                "username": "NightOperator",
                "password": "Viewer-password-7890",
            })
            assert status == 401, removed_login

            users = json.loads((runtime / "dashboard-users.json").read_text(encoding="utf-8"))
            owner = next(user for user in users["users"] if user["role"] == "owner")
            assert "passwordHash" in owner and "passwordSalt" in owner
            assert "Recovered-password-9012" not in json.dumps(users)
            assert users["setupComplete"] is True
            print("dashboard-first-run-setup-v7.3.10: PASS")
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)


if __name__ == "__main__":
    main()

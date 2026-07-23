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
PORT = 40871
OWNER_TOKEN = "multi-server-owner-token-123456789"


def request(path: str):
    req = urllib.request.Request(
        f"http://127.0.0.1:{PORT}{path}",
        headers={"Authorization": f"Bearer {OWNER_TOKEN}"},
    )
    with urllib.request.urlopen(req, timeout=10) as response:
        return response.status, json.loads(response.read().decode())


def wait_ready(process: subprocess.Popen[str]) -> None:
    for _ in range(100):
        if process.poll() is not None:
            output = process.stdout.read() if process.stdout else ""
            raise AssertionError(f"Dashboard exited during startup:\n{output}")
        try:
            status, _ = request("/api/state")
            if status == 200:
                return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.1)
    raise AssertionError("Dashboard did not become ready")


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="ninjos-multi-endstone-") as temp_name:
        temp = Path(temp_name)
        runtime = temp / "runtime"
        runtime.mkdir()
        config_path = temp / "edge-fabric.ini"
        config_path.write_text(
            (ROOT / "config" / "edge-fabric.example.ini")
            .read_text(encoding="utf-8")
            .replace("port = 25571", f"port = {PORT}", 1),
            encoding="utf-8",
        )
        now = int(time.time() * 1000)
        (runtime / "gateway-state.json").write_text(
            json.dumps(
                {
                    "timestamp": now,
                    "backends": [
                        {
                            "name": "kingdom",
                            "healthy": True,
                            "enabled": True,
                            "latencyMs": 12.5,
                            "activeSessions": 3,
                        },
                        {
                            "name": "zoo",
                            "healthy": True,
                            "enabled": True,
                            "latencyMs": 18.75,
                            "activeSessions": 0,
                        },
                    ],
                }
            ),
            encoding="utf-8",
        )
        (runtime / "sessions.json").write_text("[]", encoding="utf-8")
        (runtime / "companion-state-kingdom.json").write_text(
            json.dumps(
                {
                    "serverId": "kingdom",
                    "timestamp": now,
                    "metrics": {
                        "currentTps": 19.95,
                        "averageTps": 19.87,
                        "currentMspt": 32.4,
                        "averageMspt": 34.1,
                        "onlinePlayers": 7,
                        "maxPlayers": 50,
                        "queueDepth": 2,
                        "uploadFailures": 0,
                        "companionVersion": "3.6.1",
                    },
                }
            ),
            encoding="utf-8",
        )

        environment = os.environ.copy()
        environment.update(
            {
                "RUNTIME_DIR": str(runtime),
                "EDGE_CONFIG_FILE": str(config_path),
                "GATEWAY_CONFIG_FILE": str(temp / "gateway.conf"),
                "DASHBOARD_PORT": str(PORT),
                "DASHBOARD_TOKEN": OWNER_TOKEN,
                "NINJOS_ENABLE_LEGACY_OWNER_TOKEN": "1",
                "COMPANION_SHARED_SECRET": "default-companion-secret-123456",
                "COMPANION_KINGDOM_SECRET": "kingdom-companion-secret-123456",
                "COMPANION_ZOO_SECRET": "zoo-companion-secret-123456789",
            }
        )
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
            status, state = request("/api/state")
            assert status == 200
            companions = state.get("companions", [])
            assert len(companions) == 3, companions
            by_id = {item["serverId"]: item for item in companions}
            assert set(by_id) == {"kingdom", "zoo", "lobby"}

            kingdom = by_id["kingdom"]
            assert kingdom["displayName"] == "The Kingdom"
            assert kingdom["connected"] is True
            assert kingdom["reportStatus"] == "live"
            assert kingdom["metrics"]["currentTps"] == 19.95
            assert kingdom["activeSessions"] == 3
            assert kingdom["secretConfigured"] is True

            zoo = by_id["zoo"]
            assert zoo["displayName"] == "Zoo"
            assert zoo["connected"] is False
            assert zoo["reportStatus"] == "never"
            assert zoo["metrics"] == {}
            assert zoo["secretConfigured"] is True

            lobby = by_id["lobby"]
            assert lobby["displayName"] == "Main Lobby"
            assert lobby["backendEnabled"] is False
            assert lobby["connected"] is False
            assert lobby["reportStatus"] == "never"

            app = (ROOT / "dashboard" / "public" / "app.js").read_text(encoding="utf-8")
            html = (ROOT / "dashboard" / "public" / "index.html").read_text(encoding="utf-8")
            styles = (ROOT / "dashboard" / "public" / "styles.css").read_text(encoding="utf-8")
            assert "renderEndstonePerformance(companions)" in app
            assert "endstone-server-card" in app and "endstone-server-grid" in app
            assert 'addEventListener("change", () => saveProfileRole' in app
            assert "if (state.profileRoleSaving.size) return" in app
            assert 'id="endstoneSummary"' in html
            assert ".endstone-server-grid" in styles

            print("multi-server-endstone-performance-v7.3.11: PASS")
        finally:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import signal
import socket
import subprocess
import tempfile
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

BINARY = Path(os.environ.get("NINJOS_DASHBOARD_BINARY", ROOT / "prebuilt/linux-x86_64/NinjOSDashboard")).resolve()
PORT = 0


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as server:
        server.bind(("127.0.0.1", 0))
        return int(server.getsockname()[1])


def live() -> bool:
    try:
        with urllib.request.urlopen(f'http://127.0.0.1:{PORT}/health/live', timeout=1) as response:
            return response.status == 200
    except Exception:
        return False


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
    raise AssertionError(f'condition not met: {last}')


def command_present(path: Path, server: str, enabled: str) -> bool:
    if not path.exists():
        return False
    return any(f'|backend|{server}|{enabled}' in line for line in path.read_text().splitlines())


def write_state(path: Path, healthy: bool) -> None:
    path.write_text(json.dumps({
        'timestamp': int(time.time() * 1000),
        'activeSessions': 0,
        'trackedIps': 0,
        'counters': {},
        'firewall': {'topRisk': []},
        'backends': [
            {'name': 'kingdom', 'healthy': True, 'enabled': True},
            {'name': 'zoo', 'healthy': healthy, 'enabled': True},
        ],
    }))


def main() -> None:
    global PORT
    PORT = free_port()
    with tempfile.TemporaryDirectory(prefix='ninjos-health-actions-') as temp_name:
        runtime = Path(temp_name)
        config_path = runtime / "edge-fabric.ini"
        config_path.write_text((ROOT / "config/edge-fabric.example.ini").read_text())
        state = runtime / 'gateway-state.json'
        write_state(state, False)
        (runtime / 'sessions.json').write_text('[]')
        (runtime / 'health-actions.properties').write_text(
            'enabled=true\n'
            'require_companion=false\n'
            'degraded_after_seconds=1\n'
            'recovery_seconds=1\n'
            'minimum_tps=12\n'
            'maximum_mspt=80\n'
        )

        env = os.environ.copy()
        env.update({
            'RUNTIME_DIR': str(runtime),
            'EDGE_CONFIG_FILE': str(config_path),
            'DASHBOARD_PORT': str(PORT),
            'DASHBOARD_TOKEN': 'health-owner',
            'NINJOS_ENABLE_LEGACY_OWNER_TOKEN': '1',
            'COMPANION_SHARED_SECRET': 'health-secret',
        })
        process = subprocess.Popen([str(BINARY)], env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, start_new_session=True)
        try:
            wait_for(live)
            commands = runtime / 'commands.log'
            wait_for(lambda: command_present(commands, 'zoo', 'false'), timeout=20)
            write_state(state, True)
            wait_for(lambda: command_present(commands, 'zoo', 'true'), timeout=16)
            alerts = runtime / 'edge-fabric.db'
            assert alerts.exists()
            print('health-actions-v7.3.7: PASS')
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=4)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=4)


if __name__ == '__main__':
    main()

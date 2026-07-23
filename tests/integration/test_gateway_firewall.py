#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import signal
import socket
import subprocess
import tempfile
import threading
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

BINARY = Path(os.environ.get("NINJOS_EDGE_BINARY", ROOT / "prebuilt/linux-x86_64/NinjOSEdge")).resolve()


def echo_backend(host: str, port: int, prefix: bytes, stop: threading.Event) -> None:
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind((host, port))
    sock.settimeout(0.2)
    try:
        while not stop.is_set():
            try:
                data, address = sock.recvfrom(4096)
            except socket.timeout:
                continue
            sock.sendto(prefix + data, address)
    finally:
        sock.close()


def request(client: socket.socket, port: int, payload: bytes, timeout: float = 1.0) -> bytes | None:
    client.settimeout(timeout)
    client.sendto(payload, ("127.0.0.1", port))
    try:
        return client.recvfrom(4096)[0]
    except socket.timeout:
        return None


def wait_state(path: Path, predicate, timeout: float = 6.0) -> dict:
    deadline = time.time() + timeout
    last = {}
    while time.time() < deadline:
        try:
            last = json.loads(path.read_text())
            if predicate(last):
                return last
        except Exception:
            pass
        time.sleep(0.1)
    raise AssertionError(f"state predicate not met; last={last}")


def main() -> None:
    if not BINARY.is_file():
        raise FileNotFoundError(BINARY)

    with tempfile.TemporaryDirectory(prefix="ninjos-v65-gateway-") as temp_name:
        root = Path(temp_name)
        runtime = root / "runtime"
        runtime.mkdir()
        profiles = runtime / "protection-profiles.properties"
        profiles.write_text(
            "\n".join(
                [
                    "default.max_datagram_size=2048",
                    "default.max_packets_per_second_per_ip=100",
                    "default.max_handshakes_per_minute=100",
                    "default.max_sessions_per_ip=4",
                    "default.allow_new_sessions_during_incident=false",
                    "kingdom.max_packets_per_second_per_ip=100",
                    "kingdom.max_handshakes_per_minute=100",
                    "kingdom.max_sessions_per_ip=4",
                    "zoo.max_packets_per_second_per_ip=2",
                    "zoo.max_handshakes_per_minute=100",
                    "zoo.max_sessions_per_ip=2",
                ]
            )
            + "\n"
        )

        config = root / "gateway.conf"
        config.write_text(
            f"""listen_port=32166
runtime_dir={runtime}
transfer_ticket_file={runtime / 'transfer-tickets.tsv'}
command_file={runtime / 'commands.log'}
backends=kingdom|127.0.0.1|32165|false;zoo|127.0.0.1|32131|false
static_routes=32166|kingdom;32171|zoo
primary_backend=kingdom
routing_mode=primary
transfer_enabled=true
transfer_port_start=32172
transfer_port_end=32173
transfer_reserved_ports=32172
idle_timeout_seconds=30
handshake_timeout_seconds=10
cleanup_interval_seconds=1
max_sessions=100
max_sessions_per_ip=10
firewall_enabled=true
adaptive_firewall_enabled=true
risk_decay_per_minute=5
risk_warning_threshold=1000
risk_ban_threshold=2000
progressive_ban_seconds=1,2
firewall_allowlist_file={runtime / 'allow.txt'}
firewall_denylist_file={runtime / 'deny.txt'}
firewall_bans_file={runtime / 'bans.tsv'}
protection_profiles_file={profiles}
max_datagram_size=2048
max_packets_per_second_per_ip=100
global_packets_per_second=100000
max_handshakes_per_minute=100
ping_cache_enabled=false
health_enabled=false
incident_mode_enabled=true
incident_trigger_packets_per_second=3
incident_trigger_drop_ratio=1.0
incident_min_packets_per_second=1
incident_recovery_seconds=2
incident_rate_divisor=2
incident_handshake_divisor=2
packet_capture_enabled=false
capture_outgoing=false
socket_receive_buffer=1048576
socket_send_buffer=1048576
stats_interval_seconds=60
state_interval_ms=100
command_poll_ms=100
live_config_file={runtime / 'live-config.properties'}
live_config_reload_ms=100
"""
        )

        stop = threading.Event()
        threads = [
            threading.Thread(target=echo_backend, args=("127.0.0.1", 32165, b"K:", stop), daemon=True),
            threading.Thread(target=echo_backend, args=("127.0.0.1", 32131, b"Z:", stop), daemon=True),
        ]
        for thread in threads:
            thread.start()

        # Simulate a Full Proxy listener owning a port inside the temporary
        # transfer pool. The transparent gateway must leave it reserved.
        reserved_listener = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        reserved_listener.bind(("0.0.0.0", 32172))

        process = subprocess.Popen(
            [str(BINARY), "--config", str(config)],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
        try:
            time.sleep(0.6)
            kingdom = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            kingdom.bind(("127.0.0.2", 0))
            zoo = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            zoo.bind(("127.0.0.3", 0))
            newcomer = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            newcomer.bind(("127.0.0.4", 0))

            assert request(kingdom, 32166, b"hello") == b"K:hello"
            assert request(zoo, 32171, b"one") == b"Z:one"
            assert request(zoo, 32171, b"two") == b"Z:two"
            assert request(zoo, 32171, b"three", 0.25) is None

            for index in range(12):
                reply = request(kingdom, 32166, f"load-{index}".encode(), 0.5)
                assert reply == b"K:" + f"load-{index}".encode()

            state_path = runtime / "gateway-state.json"
            active = wait_state(state_path, lambda value: value.get("incident", {}).get("active") is True)
            assert active["backends"][0]["protectionProfile"] == "kingdom"
            assert active["backends"][1]["protectionProfile"] == "zoo"
            assert active["counters"]["rateLimited"] >= 1

            assert request(newcomer, 32166, b"new-during-incident", 0.35) is None
            assert request(kingdom, 32166, b"existing-stays") == b"K:existing-stays"

            recovered = wait_state(state_path, lambda value: value.get("incident", {}).get("active") is False, timeout=6)
            assert recovered["counters"]["incidentEntries"] >= 1
            assert recovered["counters"]["incidentExits"] >= 1
            assert request(newcomer, 32166, b"new-after-recovery") == b"K:new-after-recovery"

            print("gateway-v7.3.13: PASS")
        finally:
            process.send_signal(signal.SIGTERM)
            try:
                process.wait(timeout=4)
            except subprocess.TimeoutExpired:
                process.kill()
            reserved_listener.close()
            stop.set()


if __name__ == "__main__":
    main()

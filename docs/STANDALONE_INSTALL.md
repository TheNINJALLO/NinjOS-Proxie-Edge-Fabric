# Standalone Linux installation

This guide installs Ninj-OS Proxie Edge Fabric on a normal Linux VPS, dedicated
server, home server, or virtual machine without Pterodactyl.

## Supported host

- 64-bit x86 Linux
- Linux kernel 3.2 or newer
- Bash, `tar`, `sha256sum`, `awk`, `coreutils`, and systemd for the managed-service method
- Public or LAN UDP ports for every configured route and transfer port
- One TCP port for the dashboard

The supplied gateway and dashboard executables are statically linked. Go, CMake,
and a C++ compiler are only needed when building from source.

## Recommended automated installation

1. Download the runtime archive from the GitHub Release:

   ```text
   NinjOS-Proxie-Edge-Fabric-v7.3.6-Runtime.tar.gz
   ```

2. Download or clone the source repository, then run the installer as root:

   ```bash
   sudo ./scripts/install-standalone.sh \
     ./NinjOS-Proxie-Edge-Fabric-v7.3.6-Runtime.tar.gz
   ```

3. The installer will:

   - verify the SHA-256 sidecar when it is beside the archive;
   - stage and validate the runtime before installation;
   - install release files in `/opt/ninjos-proxie`;
   - preserve `/opt/ninjos-proxie/config` and `/opt/ninjos-proxie/runtime`;
   - create the restricted `ninjos` service account;
   - generate a private companion secret and a single-use dashboard setup code;
   - create `/etc/ninjos-proxie.env` with mode `0600`;
   - install and start `ninjos-proxie.service`.

4. The installer prints the single-use setup code. It can also be read before
   setup completes with:

   ```bash
   sudo cat /opt/ninjos-proxie/runtime/FIRST_RUN_SETUP.txt
   ```

5. Open the dashboard:

   ```text
   http://SERVER-IP:25571
   ```

   Enter the setup code and choose the permanent owner username and password.
   The setup file is deleted automatically after success.

6. Open **Configuration & Secrets** and replace the bundled example public
   address, backend addresses, public UDP routes, and transfer pool with values
   assigned to your server.

## Service commands

```bash
sudo systemctl status ninjos-proxie
sudo systemctl restart ninjos-proxie
sudo systemctl stop ninjos-proxie
sudo systemctl start ninjos-proxie
sudo journalctl -u ninjos-proxie -f
```

The dashboard log and persistent database are also stored beneath:

```text
/opt/ninjos-proxie/runtime/
```

## Manual installation without systemd

Use this method on distributions without systemd, inside another process manager,
or during testing.

```bash
mkdir -p "$HOME/ninjos-proxie"
tar -xzf NinjOS-Proxie-Edge-Fabric-v7.3.6-Runtime.tar.gz \
  -C "$HOME/ninjos-proxie"
cd "$HOME/ninjos-proxie"
chmod +x NinjOSEdge NinjOSDashboard start-runtime.sh ninjos-dashboard-account.sh

export NINJOS_ROOT_DIR="$PWD"
export COMPANION_SHARED_SECRET='replace-with-a-different-long-random-secret'
./start-runtime.sh
```

Press `Ctrl+C` to stop it. For unattended hosting, use systemd, Docker Compose, or
another supervisor instead of leaving the process in an SSH session.

## Running under another supervisor

The command that must remain running is:

```bash
/opt/ninjos-proxie/start-runtime.sh
```

Set these environment values in the supervisor:

```text
NINJOS_ROOT_DIR=/opt/ninjos-proxie
DASHBOARD_SETUP_CODE=<optional fixed single-use setup code; normally empty>
DASHBOARD_RECOVERY_TOKEN=<temporary emergency password-reset override; normally empty>
COMPANION_SHARED_SECRET=<private companion secret>
```

Optional secrets referenced by `config/edge-fabric.ini` may also be supplied in the
same environment file. The process exits when the gateway exits normally and uses
special internal exit codes to reload only the necessary component after dashboard
configuration changes.

## Directory layout

```text
/opt/ninjos-proxie/
├── NinjOSEdge
├── NinjOSDashboard
├── start-runtime.sh
├── ninjos-dashboard-account.sh
├── config/
│   └── edge-fabric.ini
├── runtime/
│   ├── edge-fabric.db
│   ├── dashboard.log
│   ├── FIRST_RUN_SETUP.txt  # exists only until owner setup succeeds
│   └── generated/
└── docs/
```

Back up `config/` and `runtime/`. They contain the live configuration, database,
audit history, generated files, and other persistent state.

## Networking

Open every configured public UDP route, every UDP transfer-pool port, and the TCP
dashboard port. Do not expose backend Bedrock ports publicly unless players or
other services genuinely need direct access.

See [Firewall and networking](FIREWALL_NETWORKING.md) for UFW, firewalld, router,
and NAT examples.


## Recovering or resetting dashboard access

The preferred recovery method does not delete the account:

1. Set `DASHBOARD_RECOVERY_TOKEN` in `/etc/ninjos-proxie.env` to a private value
   containing at least 16 characters.
2. Restart the service.
3. Sign in with username `recovery` and the recovery value as the password.
4. Open **Team & Access > Owner Account** and choose a replacement
   username and password.
5. Clear `DASHBOARD_RECOVERY_TOKEN` and restart again.

For a complete first-run reset from the shell:

```bash
sudo /opt/ninjos-proxie/ninjos-dashboard-account.sh status
sudo /opt/ninjos-proxie/ninjos-dashboard-account.sh reset-setup
sudo systemctl restart ninjos-proxie
sudo cat /opt/ninjos-proxie/runtime/FIRST_RUN_SETUP.txt
```

The reset command backs up the old account file before removing it.

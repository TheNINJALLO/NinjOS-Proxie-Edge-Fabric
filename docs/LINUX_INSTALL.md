# Linux installation

Supported deployment: 64-bit Linux with systemd. The gateway and dashboard are static x86_64 binaries. Full Proxy Mode additionally uses Node.js 22; the installer uses a compatible system Node or installs the pinned portable runtime.

## Automated installation

Place these together:

```text
install-standalone.sh
NinjOS-Proxie-Edge-Fabric-v7.3.16-Runtime.tar.gz
```

Run:

```bash
chmod +x install-standalone.sh
sudo ./install-standalone.sh ./NinjOS-Proxie-Edge-Fabric-v7.3.16-Runtime.tar.gz
```

The installer:

1. Verifies the runtime SHA-256 sidecar when present.
2. Rejects an archive containing persistent `config/` or `runtime/` data.
3. Creates the restricted `ninjos` account.
4. Installs to `/opt/ninjos-proxie`.
5. Preserves existing configuration and database files.
6. Creates `/etc/ninjos-proxie.env` with generated secrets.
7. Installs Node.js 22.23.1 under `/opt/ninjos-proxie/node-runtime` only when a compatible system Node is unavailable.
8. Creates and starts `ninjos-proxie.service`.

## First login

Read the setup code:

```bash
sudo cat /opt/ninjos-proxie/runtime/FIRST_RUN_SETUP.txt
```

Open:

```text
http://SERVER-IP:25571
```

Choose the owner username and password.

## Service commands

```bash
sudo systemctl status ninjos-proxie
sudo systemctl restart ninjos-proxie
sudo systemctl stop ninjos-proxie
sudo journalctl -u ninjos-proxie -f
```

## Configuration

Canonical file:

```text
/opt/ninjos-proxie/config/edge-fabric.ini
```

Private environment values:

```text
/etc/ninjos-proxie.env
```

Use the dashboard for normal edits. Every successful save is read back and assigned a SHA-256 configuration revision.

## Firewall

Open only public listeners and the dashboard. Example with UFW:

```bash
sudo ufw allow 19132/udp
sudo ufw allow 19133/udp
sudo ufw allow from YOUR_ADMIN_IP to any port 25571 proto tcp
```

Do not open private `online-mode=false` backend ports. Restrict them to localhost or the private proxy address.

## Full Proxy requirements

The runtime includes `session-core/node_modules`, so no npm download is required after installation. The installer provides Node.js 22 when needed. Transparent Auth routes do not require the Session Core to be active.

## Manual test launch

```bash
cd /opt/ninjos-proxie
sudo -u ninjos env $(sudo cat /etc/ninjos-proxie.env | xargs) ./start-runtime.sh
```

Use systemd for production. The manual command is intended only for diagnosis.

## Upgrade

Run the new installer against the new runtime archive. It stops the service, stages the release, keeps persistent data, updates executable files, and starts the service again.

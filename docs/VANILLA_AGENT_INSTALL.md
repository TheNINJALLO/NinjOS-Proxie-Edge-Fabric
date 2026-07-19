# Vanilla Host Agent installation

The host agent complements the Vanilla Bridge. It reports process status and synchronizes proxy operators into BDS `permissions.json`.

## Linux

```bash
sudo mkdir -p /opt/ninjos-vanilla-agent
sudo cp ninjos-vanilla-agent /opt/ninjos-vanilla-agent/
sudo cp agent.json /etc/ninjos-vanilla-agent.json
sudo cp ninjos-vanilla-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ninjos-vanilla-agent
```

## Windows

Copy `ninjos-vanilla-agent.exe` and `agent.json` into a protected folder. Run it from Task Scheduler or a service wrapper:

```powershell
.\ninjos-vanilla-agent.exe --config .\agent.json
```

## Test one cycle

```text
ninjos-vanilla-agent --config agent.json --once
```

## Configuration

`serverId` and `sharedSecret` must match the dashboard backend. `permissionsFile` points to the BDS `permissions.json`. The agent preserves non-operator rows and replaces the synchronized operator set with XUIDs returned by the dashboard.

Stop BDS before manually editing the same permissions file. The agent uses an atomic temporary-file replacement, but BDS may still reload its own in-memory copy only after a permission reload or restart.

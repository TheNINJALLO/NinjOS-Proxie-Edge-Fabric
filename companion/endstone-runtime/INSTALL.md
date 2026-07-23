# Ninj-OS Endstone identity runtime

This CPython 3.14 wheel is a version-locked Endstone 0.11.6 runtime with the Ninj-OS native
identity hook. It must be installed on Full Proxy Endstone backends that need
the Microsoft XUID exposed through Endstone's normal `Player::getXuid()` API.

Stop Endstone, back up the environment, then install the wheel from this folder:

```bash
python -m pip install --force-reinstall ./endstone-*-cp314-cp314-*.whl
```

Set this environment variable in the Endstone server's startup configuration:

```text
NINJOS_TRUST_PROXY_IDENTITY=1
```

Keep the backend UDP port private and reachable only from Edge Fabric. The hook
accepts proxy identity claims only when the native XUID is empty; Companion
then consumes the corresponding one-use signed dashboard grant and kicks the
player if verification fails.

Remove `NINJOS_TRUST_PROXY_IDENTITY` when returning the backend to Transparent
Auth or exposing it directly. Do not use this wheel with a different Endstone
or Bedrock server version.

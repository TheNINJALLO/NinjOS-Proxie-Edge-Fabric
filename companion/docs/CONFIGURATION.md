# Configuration

The plugin reads:

```text
plugins/ninjos_proxie_companion/companion.properties
```

Required connection values:

```properties
dashboard_host=proxy.example.net
dashboard_port=25571
shared_secret=<backend companion shared secret>
server_id=kingdom
```

The shared secret must match the effective secret for `server_id` in the Edge
Fabric dashboard. It is separate from dashboard login credentials.

After editing the file, run:

```text
/npm reload
/npm status
/npm probe
```

Compare the 12-character fingerprint in `/npm status` with the expected backend
fingerprint shown by the dashboard.

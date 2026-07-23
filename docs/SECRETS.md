# Unified Secret Vault

The Secret Vault is the only dashboard area that edits credentials. Backend
topology, Managed Settings, and the advanced INI editor may show a redacted source
or status, but they cannot replace a Vault-managed value. This prevents a routine
server or configuration edit from silently rotating a companion key.

Every non-empty secret must contain at least 12 characters. The same server-side
rule applies to dashboard-managed values, populated environment references, and
advanced configuration validation. Generated values remain longer and stronger
than the minimum.

## Dashboard owner password

The owner username and password are not Secret Vault entries. They are created in
the first-run setup wizard and changed through **Team & Access > Owner Account**.

The password is stored only as a salted PBKDF2-HMAC-SHA256 verifier in:

```text
runtime/dashboard-users.json
```

The plaintext password is never written to disk or returned by an API.

## Companion secrets are separate

A companion shared secret is not the dashboard owner password, dashboard session,
operator token, viewer token, or metrics token. Each Endstone backend must use the
secret assigned to that backend, or inherit the default companion secret when its
per-backend value is empty.

The generated `companion.properties` file is the safest way to keep the backend ID,
dashboard address, port, and secret together.

## Secret Vault values

The Secret Vault manages service credentials such as:

```text
Operator, viewer, and metrics tokens
Owner TOTP secret
Session Core internal token
Default companion shared secret
Per-backend companion secrets
Discord webhook URL
Discord bot token
```

`DASHBOARD_TOKEN` may still appear on upgraded v7.0.x systems as a legacy owner
credential. Fresh v7.3.9 installations do not use it for owner setup.

## Storage modes

### Dashboard-managed

The value is stored in:

```text
config/edge-fabric.ini
```

The file is mode `0600`. Values are redacted from configuration APIs and support
bundles. Every successful write is read back from disk, assigned a SHA-256
configuration revision, and recorded in:

```text
runtime/config-save-status.json
```

The browser immediately reloads the configuration and verifies that the returned
revision matches the saved revision.

### Environment reference

The INI file may store an environment reference such as:

```ini
metrics_token = env:METRICS_TOKEN
```

The actual value remains in Pterodactyl Startup, the systemd environment file, or
Docker `.env`.

The dashboard rejects environment mode when the selected variable is empty in the
currently running service. Set the variable first, restart the service so it is
present in the process environment, then select environment mode. Populated
variables are subject to the same 12-character minimum and type-specific checks as
dashboard-managed values.

### Inherit default

A backend companion may leave `companion_secret` empty and inherit the default
companion key.

## Rotation behavior

Secret changes regenerate the runtime configuration and schedule only the required
service restart. Changing the owner TOTP secret invalidates dashboard sessions.
Changing the owner username or password from **Team & Access > Owner Account** also invalidates
every other browser session.

After rotating a companion secret, download and install a fresh companion package
for that backend. The old package will immediately receive `401 Unauthorized`.

## Fingerprints

The dashboard displays the first twelve hexadecimal characters of a secret's
SHA-256 hash. Matching fingerprints confirm that two systems use the same value
without revealing it.

Compare the dashboard fingerprint with `/npm status` on the Endstone backend.

## Recommendations

- Use a different companion secret for every backend.
- Use at least 32 random bytes for companion and service tokens.
- Use a unique password for the dashboard owner.
- Use HTTPS or a trusted private network when accessing the dashboard.
- Download configured companion files only on a trusted device because they
  contain the backend secret.

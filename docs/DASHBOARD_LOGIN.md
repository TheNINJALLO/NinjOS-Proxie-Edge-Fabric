# Dashboard owner setup, sign-in, and recovery

## First-run owner setup

A fresh Ninj-OS Proxie v7.3.10 installation does not ship with a shared username,
password, or permanent dashboard token. The dashboard creates a single-use setup
code and blocks every management API until an owner account is created.

The setup code appears in the server console and is stored temporarily at:

```text
runtime/FIRST_RUN_SETUP.txt
```

Open the dashboard and complete the **Create the Dashboard Owner** form:

1. Enter the single-use setup code.
2. Choose an owner username containing 3 to 32 letters, numbers, periods,
   underscores, or hyphens.
3. Choose and confirm a password containing at least 12 characters.
4. Select **Create Owner & Sign In**.

After successful setup, Ninj-OS Proxie hashes the password with a unique random
salt, creates the owner account, signs the browser in, and deletes
`runtime/FIRST_RUN_SETUP.txt`. The plaintext password is never written to disk.

## Normal sign-in

Use the username and password selected during first-run setup. When TOTP is
enabled, also enter the current six-digit code.

Background dashboard polling begins only after authentication succeeds. Failed
sign-ins do not erase the username, password, or TOTP fields.

## Team accounts and roles

The owner can open **Team & Access** and give each administrator or staff member
an individual password-based account. Accounts can be edited, disabled, have
their password reset, or be removed without changing the owner's credentials.
Disabling, editing, or deleting an account immediately ends its active sessions.

Choose the narrowest role that fits the person's work:

| Role | Intended access |
| --- | --- |
| Admin | Backends, firewall policy, configuration, secrets, audit history, and daily operations |
| Operator | Daily controls and incident response without configuration or secret access |
| Viewer | Read-only network status, players, traffic, transfers, and event history |

Only the owner can manage team accounts. The owner account itself cannot be
disabled or deleted from this page. Accounts supplied through startup environment
variables are identified as **Managed externally** and must be changed in the host
or Pterodactyl Startup settings.

## Change the owner username or password

Sign in as the owner, open **Team & Access**, and use **Owner Account**.
Enter the current password, the desired username, and the new password twice.
Changing the owner login invalidates every other browser session.

## Emergency recovery

Set `DASHBOARD_RECOVERY_TOKEN` to a private value containing at least 16
characters and restart Ninj-OS Proxie. Sign in with:

```text
Username: recovery
Password: <DASHBOARD_RECOVERY_TOKEN>
```

Open **Team & Access > Owner Account**, choose a replacement username
and password, and save. The current-password field is not required while using the
recovery session.

After recovery:

1. Clear `DASHBOARD_RECOVERY_TOKEN` from Pterodactyl, the systemd environment
   file, or Docker `.env`.
2. Restart Ninj-OS Proxie.
3. Sign in with the replacement owner username and password.

The recovery value is never copied into the permanent owner account.

## Reset first-run setup from a shell

Shell-access installations include:

```bash
./ninjos-dashboard-account.sh status
./ninjos-dashboard-account.sh reset-setup
```

`reset-setup` backs up the current account file and removes the active owner
account. Restart the service to generate a new setup code. Prefer the recovery
variable when possible because it does not remove the current account.

## Legacy v7.0.x token accounts

Upgrades preserve existing token-based owner accounts so users are not locked out.
After signing in with the old token, use **Owner Account** to replace it with a
username and password. `DASHBOARD_TOKEN` remains only as an upgrade-compatibility
setting and is not used for fresh v7.3.10 owner setup.

# Ninj-OS Proxie v7.3.4 dashboard login fix

## Problem

The dashboard started background API polling before authentication. Every
unauthorized response reopened the sign-in dialog, and the dialog-opening
function cleared the password and TOTP fields. Because some views refreshed
every 1.5 seconds, the form appeared to refresh and erase itself while typing.

## Corrected behavior

- No dashboard API polling starts before authentication succeeds.
- Opening an already visible sign-in dialog does not clear its fields.
- A stale-session or HTTP 401 response stops all polling immediately.
- A 401 reopens the sign-in dialog without erasing partially entered values.
- Login credentials are captured before the network request starts.
- A failed login leaves the password and TOTP code available for correction.
- A successful login clears the sensitive fields and starts normal polling.
- An explicit **Sign Out / Forget** action still clears the fields.

## Install

1. Back up `config/` and `runtime/`.
2. Import `egg-ninjos-proxie-edge-fabric-v7.3.4.json`.
3. Assign it to the existing proxy server.
4. Use **Reinstall**.
5. Upload `NinjOS-Proxie-Edge-Fabric-v7.3.4-Runtime.tar.gz` to the server root.
6. Start the proxy and open the dashboard.

## Verification

Leave the sign-in window open and type slowly into the password field.
The value must remain in place indefinitely. A failed sign-in should display
an error without clearing the password or TOTP field.

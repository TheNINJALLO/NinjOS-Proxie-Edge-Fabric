# Protocol Weave

Protocol Weave is the data-driven compatibility layer in the Full Proxy Session
Core. It keeps reviewed Bedrock protocol knowledge separate from the proxy
engine so hotfix aliases and small packet changes can be introduced without
waiting for a complete upstream codec release.

## What the first implementation does

- Loads validated packs from `session-core/protocol-packs/`.
- Maps a client protocol to the pinned Session Core codec only when a reviewed
  pack declares it native or wire-compatible.
- Observes every parsed application packet in both directions.
- Records packet names and field layouts as JSON Lines under
  `runtime/protocol-observations/<protocol>/<backend>.jsonl`.
- Optionally stores redacted parsed payloads for controlled research.
- Applies limited declarative field operations before forwarding.
- Publishes active pack and observation counts in Session Core health state and
  the dashboard backend registry.
- Rejects unknown protocols through the normal Bedrock version response.

## Configuration

```ini
[session_core]
version = 1.26.30
advertised_version = 1.26.33
protocol_capture_enabled = true
protocol_capture_mode = metadata
protocol_observation_max_bytes = 10485760
protocol_capture_packet_ids = 30,77
protocol_capture_max_packet_bytes = 65536
protocol_capture_decode_failures = true
```

The inspection tiers are:

- `metadata`: packet identity, direction, size, field inventory, action, and
  timing. This is the production default.
- `decoded`: metadata plus the parsed object after credential, identity,
  address, device, certificate, and skin fields are redacted.
- `wire`: metadata plus post-decryption, post-decompression application bytes
  for the IDs in `protocol_capture_packet_ids`.
- `full`: safe decoded values, selected wire samples, decode failures, and
  decode/re-encode comparisons together.

Wire data is hexadecimal, bounded by `protocol_capture_max_packet_bytes`, and
hashed with SHA-256. Round-trip inspection parses a selected packet, re-encodes
it with the active codec, and reports the first mismatching byte as well as both
hashes and lengths. It never changes forwarding behavior.

Packet IDs 1, 3, and 4 and packets classified as login, handshake, network
settings, resource-pack, identity, or cache-blob traffic never receive decoded
or wire capture, even if an operator adds their ID to the allowlist. A failure
record keeps its error and metadata but withholds sensitive bytes.

Observation files rotate to one `.1` file at the configured size. They
remain local and are never uploaded automatically.

### Passive login-packet forwarding

`CraftingData` (`0x34`) and `VoxelShapes` (`0xD1`) can change layout within a
Bedrock hotfix while retaining the same network protocol number. v7.3.13 reads
only their packet header, records a `lossless_passthrough` metadata observation,
and forwards the original application bytes. These packets are never decoded,
rewritten, dropped, or written to a large failure dump on the live login path.

## Packet Inspector

The dashboard combines transport/RakNet metadata, gameplay records uploaded by
companions, and Protocol Weave observations produced by Full Proxy sessions.
Use the layer and capture-tier filters to isolate decoded, wire, round-trip, or
failure records. Opening a row separates the summary, redacted decoded fields,
post-translation fields, hexadecimal/ASCII wire view, round-trip results, and
original inspection record.

### Official packet names and block-action diagnostics

The packet catalog is generated from Mojang's official
`bedrock-protocol-docs` JSON metadata. The inspector reports the source commit,
documented Minecraft and protocol versions, and packet count. An unknown failure
label is replaced when its numeric ID exists in the catalog. The generated file
contains factual IDs and names only, not Mojang's packet schemas.

Regenerate it from a local checkout with:

```bash
python3 scripts/import-mojang-packet-catalog.py /path/to/bedrock-protocol-docs
```

`PlayerAuthInput` (ID 144) receives a safe metadata summary containing its input
flags, block-action count, block-action names, and item-interaction flags. If a
break attempt reports no block actions, inspect the client input permissions and
game mode sent by the backend. If actions are present, inspect backend protection,
spawn protection, game mode, and permission-plugin decisions.

## Adding a future protocol

1. Copy the pack-format example into a new numeric directory.
2. Set the new protocol and its observed Minecraft version.
3. Leave it unsupported until a controlled test establishes wire compatibility.
4. If compatible, declare `wireCompatibleWith` as the pinned codec protocol and
   start with `mode: observe`.
5. Compare the resulting field inventories and failure dumps in staging.
6. Add only the required declarative translators and automated fixtures.
7. Promote the pack through the normal review, build, and release process.

Supported operations are deliberately narrow:

- `rename_field`
- `add_default`
- `drop_field`

Complex structures, changed framing, encryption changes, registries, and
packets the current codec cannot deserialize require reviewed codec source.
Protocol Weave does not guess field meanings or learn protocol semantics from
production players.

Future translation work follows the same safety properties used by mature
version bridges: a separate handler for each packet and direction, explicit
defaults or drops for one-sided fields, cancellation when no honest mapping
exists, and byte-level round-trip fixtures for every supported boundary. A
captured field inventory is research input; it is never proof that two packet
schemas are compatible.

## Security boundary

Do not enable redacted payload capture casually. Redaction is defense in depth,
not a guarantee that an unknown future field is non-sensitive. Use a staging
server and test accounts, restrict filesystem access, inspect captures before
sharing them, and delete research captures when they are no longer required.

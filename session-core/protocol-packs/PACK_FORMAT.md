# Ninj-OS Protocol Pack format

Protocol packs describe a reviewed compatibility relationship. They do not infer
unknown packet semantics. A future protocol may reuse an older codec only when
`wireCompatibleWith` is explicitly declared after controlled testing.

Required fields are `schemaVersion`, `name`, `protocol`, `minecraftVersions`,
`codecVersion`, `mode`, and `translators`. Supported modes are `native`, `alias`,
`translated`, and `observe`.

Declarative translator operations are intentionally limited to `rename_field`,
`add_default`, and `drop_field`. Anything more complex requires reviewed source
code and a normal release.

Example wire-compatible discovery pack:

```json
{
  "schemaVersion": 1,
  "name": "Bedrock future test build",
  "protocol": 1005,
  "minecraftVersions": ["1.26.40"],
  "codecVersion": "1.26.30",
  "baseProtocol": 1001,
  "wireCompatibleWith": 1001,
  "mode": "observe",
  "translators": []
}
```

Do not add `wireCompatibleWith` merely to bypass a version error. If the new
protocol changed framing or any packet used during login, the pinned codec can
still disconnect or misread the session.

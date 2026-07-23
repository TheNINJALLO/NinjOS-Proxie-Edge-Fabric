# GitHub repository and release setup

## Upload the repository

1. Extract `NinjOS-Proxie-Edge-Fabric-v7.3.12-GitHub-Repository.zip`.
2. Create an empty GitHub repository.
3. Upload the extracted contents, including `.github`.
4. Use `main` as the default branch.

The GitHub ZIP excludes generated `node_modules`, build directories, caches, runtime databases, and private configuration. The release runtime includes the pinned production dependencies for offline installation.

## Workflows

- `build-and-test.yml`: builds gateway, dashboard, Session Core, vanilla bridge, and Linux/Windows host agents, then runs integration tests.
- `build-companion.yml`: builds Endstone Companion v3.6.1 against a selected Endstone release and packages the `.so` with the generated guide.
- `codeql.yml`: analyzes C++ and Go source.
- `release.yml`: builds, tests, packages, and publishes tagged releases.

## Create a release

```bash
git tag v7.3.12
git push origin v7.3.12
```

The public release contains exactly these seven supported downloads:

```text
egg-ninjos-proxie-edge-fabric-v7.3.12.json
install-standalone.sh
install-windows.ps1
NinjOS-Endstone-Companion-v3.6.1-Endstone-0.11.6-Linux-x86_64.zip
NinjOS-Proxie-Edge-Fabric-v7.3.12-Runtime.tar.gz
NinjOS-Proxie-Edge-Fabric-v7.3.12-Source.zip
NinjOS-Vanilla-Bridge-v7.3.12.mcpack
```

## Manual packaging

```bash
./scripts/package-release.sh ./dist
```

Review checksums, documentation links, attribution, and the public-text scan before publishing.

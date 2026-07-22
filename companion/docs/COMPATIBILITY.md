# Compatibility

## Required information

Before compiling, identify:

```text
Endstone version
Minecraft/BDS version
Server operating system
Server architecture
```

This repository builds Linux x86_64 `.so` files for Pterodactyl-hosted Endstone
servers.

## Current default

```text
Plugin version: 3.6.1
Default Endstone build target: 0.11.6
C++ standard: C++20
Compiler: Clang/LLVM 18
Standard library: libc++
```

The workflow accepts another Endstone release through its manual input.

## Compatibility policy

- Prefer exact Endstone patch-version builds.
- Do not assume a plugin compiled for one native API/ABI release will load on
  another.
- Keep separate release artifacts when different backend servers run different
  Endstone versions.
- The same `.so` may be reused on Kingdom and Zoo only when both servers use the
  same Endstone release, Linux architecture, and compatible runtime.

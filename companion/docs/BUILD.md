# Build instructions

## GitHub Actions

The normal build method is:

```text
Actions → Build Endstone Companion → Run workflow
```

Enter the exact Endstone release used by the backend.

The workflow first regenerates `docs/COMPLETE_SETUP.md` from the versioned
template. The compiled install package includes the guide as
`COMPANION-HOWTO.md`.

The workflow:

1. Uses Ubuntu 22.04.
2. Installs Clang/LLVM 18, libc++, CMake, and Ninja.
3. Fetches the selected Endstone release.
4. Compiles the C++20 plugin.
5. Renames the result to `ninjos_proxie_companion.so`.
6. Creates an install-ready ZIP.
7. Uploads the ZIP as a workflow artifact.
8. Publishes the ZIP to GitHub Releases when the workflow runs from a tag.

## Local Linux build

```bash
sudo apt-get update
sudo apt-get install -y \
  clang-18 \
  cmake \
  git \
  libc++-18-dev \
  libc++abi-18-dev \
  ninja-build

cmake \
  -S . \
  -B build \
  -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DENDSTONE_API_VERSION=0.11.6 \
  -DCMAKE_C_COMPILER=clang-18 \
  -DCMAKE_CXX_COMPILER=clang++-18 \
  -DCMAKE_CXX_FLAGS="-stdlib=libc++" \
  -DCMAKE_SHARED_LINKER_FLAGS="-stdlib=libc++"

cmake --build build --parallel 2
```

Find the result:

```bash
find build -type f -name '*ninjos_proxie_companion*.so'
```

## Exact-version rule

Compile against the exact Endstone release used by the Minecraft backend.
A native library built for another API/ABI version may fail to load even when
the source code compiles successfully.

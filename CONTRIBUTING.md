# Contributing

Contributions should be focused, testable, and documented.

## Development environment

Use a Linux x86_64 environment with:

- CMake 3.20 or newer
- A C++20 compiler
- Go 1.22 or newer
- Python 3
- Bash

Windows contributors may use WSL2.

## Before opening a pull request

```bash
./scripts/build.sh
./scripts/test.sh
```

Update documentation when behavior, configuration, deployment, or user-facing screens change.

## Pull request expectations

- Explain the problem and the chosen solution.
- Keep unrelated formatting changes out of functional patches.
- Add or update tests for behavior changes.
- Do not commit private server addresses, credentials, tokens, database files, logs, or runtime captures.
- Preserve the project license, notices, and acknowledgements.

## Reporting bugs

Include:

- Ninj-OS Proxie version
- Deployment platform
- Relevant configuration with secrets removed
- Exact error output
- Steps to reproduce
- Whether the problem occurs before or after dashboard setup

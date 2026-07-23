# Implementation provenance

Ninj-OS Proxie Edge Fabric v7.3.11 uses the Ninj-OS transport and management
architecture while retaining clear credit for the project that helped inform its
design.

The project was developed with reference to and inspired by ProxyPass by
SculkCatalystMC. That acknowledgement is intentionally included in the console,
release notice, documentation, and dashboard version metadata.

The current Ninj-OS gateway is a protocol-agnostic UDP edge engine built directly
on Linux sockets and epoll. The dashboard is a separate Go control plane backed by
SQLite, and the Endstone companion is packaged as a Ninj-OS integration. The
release does not dynamically load an upstream ProxyPass binary or ship a complete
unmodified upstream source tree as its runtime.

This statement describes the contents and development provenance of this release.
It is not legal advice and does not override the AGPL-3.0 license or any applicable
copyright notice.

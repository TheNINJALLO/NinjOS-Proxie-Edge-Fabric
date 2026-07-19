# Shared backend and transfer port pool

The managed UDP allocations may be used by either permanent backend routes or temporary transfer tickets.

For example, with `transfer.port_start = 25572` and `transfer.port_end = 25581`:

- Assigning backend `redstone` to public port `25572` makes `25572` a permanent route.
- The transfer broker then allocates tickets from `25573-25581`.
- Deleting or moving the Redstone route returns `25572` to temporary ticket availability.
- A public port may belong to only one permanent backend at a time.
- Every port must still be assigned to the proxy container in Pterodactyl.

The dashboard displays the remaining temporary ports after every backend save.

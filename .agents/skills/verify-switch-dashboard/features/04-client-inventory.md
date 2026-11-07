# F04 — Discovered Client Inventory & CSV Import

## Preconditions
- Authenticated session with MAC FDB entries learned from the mock Horaco switch.

## Steps & Assertions
1. **Discovered Client Resolution**:
   - `GET /api/clients` returns discovered L2 clients learned from `/mac.cgi`.
2. **Inline Device Metadata Update**:
   - `POST /api/clients/update_host` with `{"mac": "00:E0:4C:68:01:AA", "host": "pve-hypervisor-01", "note": "Primary Proxmox Node"}` returns `200 OK`.
   - `POST /api/clients/update_type` with `{"mac": "00:E0:4C:68:01:AA", "type": "server"}` returns `200 OK`.
   - Subsequent `GET /api/clients` reflects `host: "pve-hypervisor-01"`, `note: "Primary Proxmox Node"`, and `device_type: "server"`.
3. **Bulk CSV Import**:
   - `POST /api/clients/import_csv` (`multipart/form-data` with `clients.csv`) imports records and returns `{"status": "ok", "imported": 1}`.
   - Subsequent `GET /api/clients` includes the imported MAC record (`AA:BB:CC:11:22:33` -> `nas-storage`).

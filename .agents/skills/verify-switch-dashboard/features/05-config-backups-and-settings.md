# F05 — Switch Config Backups & Device Templates

## Preconditions
- Authenticated session with Mock Horaco Switch reachable at `127.0.0.1:<mock_port>`.

## Steps & Assertions
1. **Device Templates Inspection**:
   - `GET /api/templates` returns `200 OK` with loaded YAML templates (`RTL8372_4p_2sfp.yaml`, `RTL8372_5p_1sfp.yaml`, `RTL8373_8p_1sfp.yaml`, etc.).
2. **Switch Configuration Backup Extraction (`.bin`)**:
   - `POST /api/switches/<ip>/backup` authenticates against the switch, downloads `/config_back.cgi?cmd=conf_backup`, and writes `<DASHBOARD_DATA_DIR>/backup/switch_cfg_<ip>_<timestamp>.bin`.
3. **Backup Listing & Binary Download**:
   - `GET /api/backups` lists the newly created `.bin` file with its exact byte size.
   - `GET /api/backups/<filename>/download` returns `200 OK` with the exact binary payload.
4. **Backup Deletion**:
   - `DELETE /api/backups/<filename>` with `X-CSRF-Token` removes the file and returns `200 OK`.

# Switch Dashboard — Feature Verification Index

This directory contains modular verification specifications for **Switch Dashboard**. All features are automatically executed by `scripts/verify_app.py` (`make verify`).

## Feature Specifications

| ID | Feature | File | Core Invariants Verified |
|---|---|---|---|
| `F01` | Authentication, CSRF & AES-256-GCM Vault | [`01-auth-and-vault.md`](01-auth-and-vault.md) | First-run setup, `bcrypt` login, `HttpOnly` session cookie, `X-CSRF-Token` validation, `enc:v1:` ciphertext in `config.json` |
| `F02` | 1U Switch Faceplate, Port Telemetry & SFP+ DDMI | [`02-switch-faceplate-and-ddmi.md`](02-switch-faceplate-and-ddmi.md) | Horaco CGI login/scrape, 6 ports (`2.5G` RJ45 + `10G` SFP+), TX/RX counters, port note updates, `/api/ddmi` optical telemetry |
| `F03` | L2 Topology Map & Layout Persistence | [`03-l2-topology-map.md`](03-l2-topology-map.md) | Switch + client L2 graph generation, uplink deduction, `/api/topology/layout` coordinate persistence |
| `F04` | Discovered Client Inventory & CSV Import | [`04-client-inventory.md`](04-client-inventory.md) | OUI vendor resolution, `/api/devices/update` inline edits, `/api/devices/import` CSV bulk import |
| `F05` | Switch Config Backups & Device Templates | [`05-config-backups-and-settings.md`](05-config-backups-and-settings.md) | `/api/backup/run` `.bin` extraction, `/api/backup/download/` binary verification, `/api/device-templates` YAML inspection |

# F01 — Authentication, CSRF Protection & AES-256-GCM Vault

## Preconditions
- Fresh isolated `DASHBOARD_DATA_DIR` with no `config.json` or `.master.key`.
- `./switch-dashboard` running on an ephemeral local port.

## Steps & Assertions
1. **Unauthenticated Preflight**:
   - `GET /api/auth/status` returns `200 OK` with `setup_required: true`, `authenticated: false`.
   - `GET /api/switches` without a session cookie returns `401 Unauthorized`.
2. **Initial Administrator Setup**:
   - `POST /api/auth/setup` with `{"username": "admin", "password": "StrongAdminPassword!2026"}` returns `200 OK`.
   - Response sets `switch_dashboard_session` cookie (`HttpOnly`, `SameSite=Strict`) and returns a non-empty `csrf_token`.
   - `.master.key` is created in `DASHBOARD_DATA_DIR` with `0600` permissions.
3. **CSRF Enforcement**:
   - `POST /api/config` without `X-CSRF-Token` header returns `403 Forbidden`.
4. **AES-256-GCM Switch Password Encryption at Rest**:
   - `POST /api/config` with `X-CSRF-Token` and a switch password `"SuperSecretSwitchPassword!99"` returns `200 OK`.
   - Reading `GET /api/config` redacts plaintext passwords (`has_password: true`, `password: ""`).
   - Inspecting `<DASHBOARD_DATA_DIR>/config.json` on disk confirms `switches[0].password` starts with `"enc:v1:"` and does not contain `"SuperSecretSwitchPassword!99"`.

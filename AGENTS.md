# Project rules

## Scope and design system

- Read `docs/design-system.md` before changing any frontend route, component, or stylesheet. It is the single source of truth for visual tokens, typography, contrast ratios, component states, and Directives `D1`–`D8`.
- Never brand the application as `"UniFi"` or `"Ubiquiti"`. The product is **Switch Dashboard** (L2 operations and telemetry console for Horaco, KeepLink, and RTLPlayground switches).
- Do not use Tailwind CSS utility classes in `.svelte` files unless Tailwind is explicitly installed and approved. All components must use semantic classes and CSS custom properties defined in `frontend/src/app.css`.
- Every `<svg>` icon must include explicit `width` and `height` HTML attributes so icons never expand to full viewport width.

## Security and data lifecycle

- Zero external telemetry and zero external CDN requests (`Content-Security-Policy` enforces `'self'`).
- Switch credentials stored in `config.json` must remain encrypted at rest with **AES-256-GCM** (`enc:v1:...`) via `internal/auth/crypto.go` (`.master.key` with `0600` permissions or `DASHBOARD_MASTER_KEY`). Never return plaintext switch passwords in API responses.
- Administrator passwords use `bcrypt` (cost 12). State-changing HTTP endpoints require a valid `HttpOnly` + `SameSite=Strict` session cookie, `X-CSRF-Token`, `Origin`/`Referer` validation, and `Content-Type: application/json` (except multipart CSV upload).
- Do not reintroduce legacy vulnerable modules (SSH/OVS `shell=True`, SOAP TR-064, or root ARP/port scanners).

## Verification and quality gate

- Read `.agents/skills/verify-switch-dashboard/SKILL.md` to launch an isolated instance, seed a local mock switch, drive user journeys, and verify UI/API contracts.
- Run `make build`, `make test`, `make vet`, and `make verify` (which executes `scripts/verify_app.py` against a fresh temporary `DASHBOARD_DATA_DIR` and mock switch server) before considering any change complete.
- Never run verification against production `config.json` or real network switches unless explicitly requested by the user.

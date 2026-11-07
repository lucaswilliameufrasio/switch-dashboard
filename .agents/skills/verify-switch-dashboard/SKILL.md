---
name: verify-switch-dashboard
description: Run end-to-end verification of Switch Dashboard against an isolated Go binary instance, an ephemeral mock Horaco switch CGI server, and the Precision Rack Telemetry design system contract. Use after UI, backend, poller, or configuration changes, and before declaring any task complete.
---

# Verify Switch Dashboard (`verify-switch-dashboard`)

End-to-end verification workflow for **Switch Dashboard** (`horaco-switch-dashboard`). This skill launches the real compiled binary (`./switch-dashboard`) in an isolated temporary directory alongside an ephemeral mock Horaco RTL8372 switch CGI server, drives all core user journeys and APIs, audits the compiled SvelteKit bundle against `docs/design-system.md` (`D1`–`D8`), and captures structured evidence.

---

## Quick Run

Run the automated end-to-end verification suite with a single command:

```bash
make verify
# or directly after building:
python3 scripts/verify_app.py
```

---

## 1. Launch

1. **Build Frontend & Static Binary**:
   ```bash
   mise exec -- npm --prefix frontend run build
   CGO_ENABLED=0 mise exec -- go build -trimpath -ldflags="-s -w" -o switch-dashboard ./cmd/server
   ```
2. **Start Isolated Mock Horaco Switch CGI Server**:
   - Bound to `127.0.0.1:<ephemeral_mock_port>`.
   - Emulates native Horaco / RTLPlayground CGI endpoints:
     - `POST /login.cgi` (validates MD5 response token or sets session cookie)
     - `GET /info.cgi` (device model, MAC, firmware, IP/netmask/gateway)
     - `GET /port.cgi` (6 ports: `Port 1`–`Port 4` 2.5G RJ45 + `Port 5`–`Port 6` 10G SFP+)
     - `GET /port.cgi?page=stats` (64-bit TX/RX packet, byte, and error counters)
     - `GET /mac.cgi?page=static` (L2 MAC FDB entries mapped across ports and VLANs)
     - `GET /sfp.cgi?page=ddmi` (SFP+ optical diagnostics: temperature, voltage, bias mA, TX/RX power dBm)
     - `GET /config_back.cgi?cmd=conf_backup` (1024-byte binary switch configuration payload)
     - `POST /reboot.cgi` (switch warm reboot trigger)
3. **Start Isolated `./switch-dashboard` Instance**:
   - Create an isolated temporary directory (`mktemp -d /tmp/switch-dashboard-verify-XXXXXX`).
   - Copy `device-templates/`, `device_types.yaml`, and `mac_vendors.txt` into the temporary data directory.
   - Launch `./switch-dashboard` with:
     ```bash
     DASHBOARD_HOST=127.0.0.1 \
     DASHBOARD_PORT=<ephemeral_app_port> \
     DASHBOARD_DATA_DIR=<temp_dir> \
     ./switch-dashboard
     ```
   - Track the server PID (`app_proc.pid`) and mock switch PID/thread exclusively — never use broad `pkill` or `killall`.

---

## 2. Preflight

Before driving user journeys, verify:
- `GET http://127.0.0.1:<ephemeral_app_port>/api/auth/status` returns HTTP `200` with `{"setup_required": true, "authenticated": false}`.
- `GET http://127.0.0.1:<ephemeral_app_port>/` returns HTTP `200` with the embedded SvelteKit HTML shell (`<title>Switch Dashboard</title>`, inline dark canvas first-paint CSS `#090c10`).
- **Design System Contract Audit (`docs/design-system.md` Directives `D1`–`D8`)**:
  - `D1` (Brand Integrity): Zero occurrences of `"UniFi"` in `frontend/src/`, `cmd/`, or `internal/`.
  - `D2` (No Uncompiled Utility Classes): Every class attribute in `frontend/src/routes/**/*.svelte` is backed by a rule in `frontend/src/app.css` or component `<style>` block; zero Tailwind utility tokens (`grid-cols-*`, `bg-[#...]`, `w-6`, `h-6`).
  - `D3` (SVG Explicit Sizing): Every `<svg>` element in `frontend/src/routes/**/*.svelte` declares explicit numeric `width` and `height` attributes (or `viewBox` on canvas `<svg class="topology-svg">` / `<svg class="chart-svg">`).
  - `D4` (Zero Raw Hex Colors in Route Components): Zero raw `#rrggbb` hex literals inside `frontend/src/routes/**/*.svelte` markup or script blocks.
  - `D8` (WCAG AA Contrast): Computed luminance contrast ratios in `frontend/src/app.css` meet `>= 4.5:1` for text tokens (`--text-primary`, `--text-secondary`, `--text-muted`) and `>= 3.0:1` for semantic status badges (`--status-10g`, `--status-2g5`, `--status-1g`, `--status-100m`, `--status-danger`).

---

## 3. Drive (Feature Catalogue)

See [`features/README.md`](features/README.md) for the complete index of feature verification specifications:
1. [`features/01-auth-and-vault.md`](features/01-auth-and-vault.md) — First-run setup, session authentication, CSRF enforcement, and AES-256-GCM password encryption at rest.
2. [`features/02-switch-faceplate-and-ddmi.md`](features/02-switch-faceplate-and-ddmi.md) — Live Horaco CGI polling, 1U switch faceplate telemetry, port note persistence, bandwidth history, and SFP+ DDMI optical diagnostics.
3. [`features/03-l2-topology-map.md`](features/03-l2-topology-map.md) — Passive L2 MAC FDB discovery, topology graph nodes/edges, and manual node coordinate persistence.
4. [`features/04-client-inventory.md`](features/04-client-inventory.md) — Discovered client table, inline hostname/note/device-type editing, and bulk CSV import.
5. [`features/05-config-backups-and-settings.md`](features/05-config-backups-and-settings.md) — Binary `.bin` switch configuration extraction, download, deletion, and YAML device template inspection.

---

## 4. Evidence

Every verification run writes machine-readable and human-readable evidence to `/tmp/switch-dashboard-verify-evidence/report.json`:
- Timestamp, binary SHA-256, and Go/Node toolchain versions.
- Design system Directive (`D1`–`D8`) audit results and computed WCAG contrast table.
- Step-by-step HTTP status codes, payload assertions, and at-rest `config.json` ciphertext proof (`enc:v1:...`).
- Headless browser screenshot status:
  - When Playwright Chromium + host X11/ATK shared libraries (`libatk-1.0.so.0`, `libgbm.so.1`, etc.) are available, `scripts/verify_app.py` captures viewport screenshots across Desktop (`1440x900`) and Mobile (`390x844`) viewports into `/tmp/switch-dashboard-verify-evidence/screenshots/`.
  - If host shared libraries are missing on a headless server, the script records the exact dynamic linker diagnostic (`libatk-1.0.so.0: cannot open shared object file`) in `report.json` under `browser_visual_check` instead of faking visual sign-off.

---

## 5. Isolation & Cleanup

- Never run verification against the user's production `config.json`, `.master.key`, or port `8080`.
- Always pass `DASHBOARD_DATA_DIR=<temp_dir>` and an ephemeral free TCP port.
- In a `try ... finally` block, terminate only the spawned `app_proc` (`app_proc.terminate()` -> `app_proc.wait()`) and shut down the in-process `ThreadingHTTPServer` mock switch, then remove the temporary `DASHBOARD_DATA_DIR`.

---

## 6. Failure Handling

- If any assertion in `scripts/verify_app.py` fails, the script prints the last 50 lines of `./switch-dashboard` stdout/stderr, writes the failure details to `/tmp/switch-dashboard-verify-evidence/report.json`, cleans up all child processes, and exits with code `1`.
- Do not retry blindly: inspect the failing step, fix the root cause in Go or SvelteKit, rebuild (`make build`), and re-run `make verify`.

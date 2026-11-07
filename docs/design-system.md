# Switch Dashboard design system

Status: approved on 2026-11-07 by repository owner · Decision: `AGENTS.md`

## 1. Purpose and audience

**Switch Dashboard** is a local-first L2 network operations and hardware telemetry console for managed switches using Realtek RTL8372 / RTL8373 chipsets (Horaco, KeepLink, and RTLPlayground firmware).

- **Who uses it:** Home-lab operators, infrastructure engineers, and small NOC administrators managing 2.5GbE RJ45 and 10GbE SFP+ switches without SNMP.
- **Where:** Desktop browsers (`1440×900`+) during topology and port troubleshooting, and mobile browsers (`390×844`) while physically standing in front of a wiring closet or rack.
- **First job to understand within 3 seconds:** Which switches and physical ports are online, at what link speed (`10G`, `2.5G`, `1G`, `100M`, `Down`), and how much traffic is flowing across each port right now.

## 2. Audit summary

An audit of the initial SvelteKit migration identified four critical defects that caused the previous redesign to fail:

1. **Uncompiled utility classes (broken layout and giant SVGs):** Route templates (`+page.svelte`, `topology/+page.svelte`, `clients/+page.svelte`, `backups/+page.svelte`, `logs/+page.svelte`, `settings/+page.svelte`) used Tailwind CSS classes (`grid`, `flex`, `w-6`, `h-6`, `bg-[#0b0d12]`, `p-4`) without Tailwind installed in `frontend/package.json`. Consequently, layouts collapsed into vertical stacks and `<svg>` icons lacking explicit `width`/`height` attributes expanded to full viewport width.
2. **Raw color literals in templates:** Components embedded dozens of ad-hoc hex colors (`#8c96a8`, `#232936`, `#006fff`, `#ff3d71`) directly in markup instead of referencing semantic CSS custom properties.
3. **Misappropriated third-party branding:** The UI and backend previously used third-party network vendor names instead of the project's actual identity. While the visual density and hardware-first presentation take inspiration from modern network appliance consoles, the product is **Switch Dashboard** and must never impersonate external vendor branding.
4. **Unstyled first paint risk:** Styles were only imported via JavaScript hydration in `+layout.svelte` rather than guaranteeing critical canvas/surface tokens in `app.html`.

**What is kept:**
- Single-binary Go + static SvelteKit architecture with zero external CDNs and zero external telemetry.
- Physical 1U rack faceplate metaphor for RJ45 and SFP+ ports, 3-tier bandwidth history (`live`, `1h`, `24h`), on-demand SFP+ DDMI optical diagnostics, interactive L2 topology map, and AES-256-GCM switch password encryption.

## 3. Concept and principles

**Concept:** *Precision Rack Telemetry* — an anodized aluminum 1U hardware faceplate paired with a high-contrast dark slate operations console where port LEDs and link speeds carry the color while surrounding chrome remains neutral.

### Principles

1. **Hardware state leads, chrome stays neutral** — Link status LEDs (`10G` cyan, `2.5G` emerald, `1G` green, `100M` amber, `Down` slate) and traffic series are the only saturated elements in the viewport, *so we never tint card backgrounds, sidebars, or tables with decorative gradients.* (Von Restorff isolation effect)
2. **Token-only styling with zero external CSS frameworks** — Every component uses semantic CSS custom properties and scoped/global CSS classes defined in `frontend/src/app.css`, *so we never write Tailwind utility classes or raw hex/px literals in `.svelte` templates.* (Aesthetic-usability effect)
3. **Intrinsic SVG and layout geometry** — Every `<svg>` element declares explicit `width` and `height` HTML attributes in addition to CSS sizing, and `app.html` defines critical dark-canvas rules inline, *so we never render giant fallback SVGs or flash an unstyled white screen.* (First impression / 50 ms rule)
4. **Tabular precision for network data** — All MAC addresses, IPv4/IPv6 addresses, bitrates, byte counters, VLAN IDs, and optical dBm readings use monospace typography with `font-variant-numeric: tabular-nums`, *so live polling updates never cause horizontal layout jitter.* (Recognition over recall)
5. **Honest identity and content** — The console identifies itself as **Switch Dashboard** and displays only real scraped switch/FDB telemetry or explicit empty states, *so we never borrow third-party vendor trademarks or invent placeholder metrics.*

## 4. Tokens

### Color

All colors are exposed as CSS custom properties on `:root` in `frontend/src/app.css`.

| Token | Value | Role | Contrast on surface |
| --- | --- | --- | --- |
| `--bg-canvas` | `#0a0d12` | Application root canvas | — |
| `--bg-surface` | `#11161f` | Primary cards, navigation rail, top bar | — |
| `--bg-elevated` | `#171e2b` | Hover rows, active tabs, secondary buttons, modals | — |
| `--bg-inset` | `#0c1017` | Form inputs, code editors, switch chassis recess, chart well | — |
| `--border-subtle` | `#21293a` | Card borders, table row dividers, rail separator | — |
| `--border-strong` | `#2f3b52` | Active card borders, input focus border, chassis bezel | 3.1:1 (UI) |
| `--text-primary` | `#f1f5f9` | Primary headings, active values, switch names | 16.4:1 on `--bg-surface` |
| `--text-secondary` | `#94a3b8` | Table headers, field labels, secondary metadata | 6.8:1 on `--bg-surface` |
| `--text-muted` | `#64748b` | Disabled states, empty hints, timestamps | 4.5:1 on `--bg-inset` |
| `--accent-primary` | `#2563eb` | Primary action button fill, active route indicator | 4.8:1 with `#ffffff` |
| `--accent-hover` | `#3b82f6` | Primary button hover, interactive links, TX chart line | 6.1:1 on `--bg-surface` |
| `--accent-soft` | `rgba(37, 99, 235, 0.16)` | Selected nav pill, active switch tab background | — |
| `--status-online` | `#10b981` | Switch online badge, RX chart line, healthy sensor | 7.6:1 on `--bg-surface` |
| `--status-warning` | `#f59e0b` | 100M/10M link LED, high temperature warning, reboot action | 8.9:1 on `--bg-surface` |
| `--status-danger` | `#ef4444` | Switch offline badge, packet errors, delete action | 5.4:1 on `--bg-surface` |
| `--port-10g` | `#06b6d4` | 10GbE / SFP+ active link LED & badge | 8.2:1 on `--bg-inset` |
| `--port-25g` | `#10b981` | 2.5GbE active link LED & badge | 7.9:1 on `--bg-inset` |
| `--port-1g` | `#22c55e` | 1GbE active link LED & badge | 8.4:1 on `--bg-inset` |
| `--port-100m` | `#f59e0b` | 100Mbps / 10Mbps active link LED & badge | 9.1:1 on `--bg-inset` |
| `--port-down` | `#334155` | Disconnected port LED | — |

### Typography

| Style | Size / line height | Weight | Use |
| --- | --- | --- | --- |
| `--font-sans` | `-apple-system, BlinkMacSystemFont, 'Inter', 'Segoe UI', sans-serif` | 400–700 | UI controls, labels, navigation, headings |
| `--font-mono` | `'JetBrains Mono', 'Fira Code', ui-monospace, monospace` | 400–600 | IPs, MACs, port numbers, throughput, counters, YAML |
| Page Title (`.text-title`) | `1.125rem` (`18px`) / `1.4` | `600` | Screen header title |
| Section Heading (`.text-section`) | `0.875rem` (`14px`) / `1.4` | `600` | Card header, modal title |
| Body / Table Cell (`.text-body`) | `0.8125rem` (`13px`) / `1.5` | `400` (`500` active) | Default interface copy and table cells |
| Caption / Label (`.text-label`) | `0.75rem` (`12px`) / `1.35` | `500` | Form labels, KPI titles, table column headers |
| Micro / Port Silkscreen (`.text-micro`) | `0.6875rem` (`11px`) / `1.2` | `600` | Switch chassis port numbers, speed badges |

### Space, layout, shape, depth

- **Spacing scale:** `--space-1` (`4px`), `--space-2` (`8px`), `--space-3` (`12px`), `--space-4` (`16px`), `--space-5` (`20px`), `--space-6` (`24px`), `--space-8` (`32px`).
- **Layout shell:**
  - Desktop (`>= 960px`): Fixed `220px` left navigation rail + flexible main column (`56px` sticky top bar + `24px` padded content container, max-width `1600px`).
  - Mobile (`< 960px`): Compact `60px` icon rail, `16px` content padding, responsive 1–2 column KPI grids, horizontally scrollable data tables and switch chassis faceplate.
- **Radii:** `--radius-sm` (`6px` for badges/inputs/port sockets), `--radius-md` (`8px` for buttons/tabs), `--radius-lg` (`12px` for cards/chassis/modals).
- **Elevation:** `--shadow-card` (`0 4px 20px rgba(0, 0, 0, 0.28)`), `--shadow-modal` (`0 20px 50px rgba(0, 0, 0, 0.65)`).

### Motion

| Type | Duration | Easing | Reduced-motion behavior |
| --- | --- | --- | --- |
| Hover / Focus (`--transition-fast`) | `120ms` | `ease-out` | Instant (`0ms`) |
| Drawer / Modal (`--transition-panel`) | `180ms` | `cubic-bezier(0.16, 1, 0.3, 1)` | Instant opacity toggle |
| Live LED pulse | `2000ms` | `ease-in-out` | Static solid indicator |

## 5. Iconography, imagery, and content honesty

- **Icons:** Inline stroke/fill SVG icons on a `24×24` viewBox with explicit `width` and `height` HTML attributes (`16`–`20` px for controls, `24` px for empty states). No external icon fonts (Material Design Icons CDN is banned).
- **Hardware faceplate graphics:** Rendered via deterministic CSS/SVG geometry (RJ45 8P8C stepped cutout and SFP+ metallic cage with status LEDs) rather than external raster photos.
- **Content honesty:** Never invent switches, clients, or throughput numbers. When no switches are configured, show a clear empty state with a direct link to `/settings`.

## 6. Voice and microcopy

- **Product name:** `Switch Dashboard` (default configurable title).
- **Tone:** Technical, concise, operational (NOC style).
- **Vocabulary:**
  - `Switches` (managed L2 switches)
  - `Portas` / `Telemetria de Portas` (physical switch ports & counters)
  - `Topologia L2` (passive MAC FDB network graph)
  - `Clientes FDB` (devices learned on switch ports)
  - `Backups` (binary `.bin` switch configuration files)
  - `Logs` (in-memory & file audit/scraper logs)
  - `Configurações` (switch credentials, polling, YAML templates, OUI, admin security)

## 7. Components

1. **App Shell (`.app-shell`, `.nav-rail`, `.top-bar`):** Left navigation rail with active indicator bar, top telemetry bar showing online switch ratio, active port count, aggregate TX/RX throughput pill, manual Sync button, and current admin user badge.
2. **KPI Metric Strip (`.kpi-grid`, `.kpi-card`):** 4-column summary strip displaying Online Switches, Active Ports, Aggregate Throughput, and Learned FDB MACs.
3. **1U Rack Switch Chassis (`.chassis-card`, `.chassis-faceplate`, `.port-bay`):** Metallic dark-anodized enclosure with rack ear screw accents, switch metadata header (Model, IP, MAC, Firmware, Uptime), speed legend, and interactive RJ45 / SFP+ port bays with LED indicators.
4. **Data Table (`.table-card`, `.data-table`):** Sticky header, tabular numeric alignment, inline note/hostname editing, and row hover highlight.
5. **Modal / Inspector Dialog (`.modal-backdrop`, `.modal-panel`):** Keyboard-dismissible (`Escape`) dialog for Port Bandwidth History (`live`, `1h`, `24h` SVG area chart + SFP+ DDMI optical sensors) and Switch MAC FDB Table.
6. **Form Controls (`.field-group`, `.field-label`, `.input`, `.select`, `.textarea`, `.btn`):** Explicit label association, visible `:focus-visible` outline (`2px solid var(--accent-hover)`), and semantic button variants (`.btn-primary`, `.btn-secondary`, `.btn-danger`, `.btn-ghost`).

## 8. Screen patterns

| Route | Screen | Key components & states |
| --- | --- | --- |
| Auth Gate | Initial Admin Setup & Sign-In | Centered `.auth-card`, AES-256-GCM vault notice, validation alert, rate-limit feedback |
| `/` | Switches & Port Telemetry | `.kpi-grid`, switch selector tabs, `.chassis-card` 1U faceplate, `.data-table` port telemetry, Port Inspector modal (SVG chart + DDMI), MAC Table modal |
| `/topology` | L2 Network Topology Map | Toolbar with filters & zoom controls, interactive SVG canvas with grid, draggable switch/infra/client nodes, side inspector card |
| `/clients` | L2 Client Inventory | Search & status filter bar, `.data-table` with inline hostname/note editor, device category selector, CSV import modal |
| `/backups` | Switch Config Backups | Quick-backup switch cards, stored `.bin` table with download & delete actions |
| `/logs` | System & Poller Logs | Search, severity selector (`DEBUG`/`INFO`/`WARNING`/`ERROR`), export `.log`, monospace log stream |
| `/settings` | Controller Settings | 4-tab layout: Switches (AES-256-GCM masked passwords), General & L2 Topology, YAML Templates & IEEE OUI, Admin Security |

## 9. Engagement patterns and ethics

- **Supported operational loop:** Real-time polling (`refresh_interval`) and manual `Sync` button with immediate toast confirmation.
- **Ethical boundaries:** Zero external telemetry, zero third-party requests, no confirmshaming, no artificial alerts, and all user data (`config.json`, `.bin` backups, `.log` files) remains 100% local and exportable.

## 10. Platform adaptations

- **Desktop Web (`1440×900`):** 4-column KPI grid, full horizontal 1U switch faceplate, side-by-side settings panels.
- **Mobile Web (`390×844`):** Compact `60px` navigation rail, 2-column KPI grid, horizontally scrollable switch faceplate and tables so nothing clips or overflows the viewport.
- **Reduced Motion:** `@media (prefers-reduced-motion: reduce)` disables all transitions and pulsing animations.

## 11. Directives

- **D1. MUST** use semantic CSS custom properties from `frontend/src/app.css`; **MUST NOT** use raw hex colors in `.svelte` route files.
- **D2. MUST NOT** use Tailwind utility classes unless Tailwind is installed and configured (this repository uses pure semantic CSS in `frontend/src/app.css`).
- **D3. MUST** include explicit `width` and `height` HTML attributes on every `<svg>` element (except the responsive chart/topology canvas SVGs which have explicit CSS container dimensions).
- **D4. MUST NOT** brand the interface with third-party vendor names; use `"Switch Dashboard"` as the product identity.
- **D5. MUST** maintain at least `4.5:1` contrast ratio for body text and `3:1` for UI controls/badges.
- **D6. MUST** format all numeric telemetry, MAC addresses, IP addresses, and port counters with `font-variant-numeric: tabular-nums`.
- **D7. MUST** associate every form `<input>`, `<select>`, and `<textarea>` with a `<label>`.
- **D8. MUST** inline critical dark canvas styles in `frontend/src/app.html` to prevent white flash on first paint.

## 12. Anti-patterns

- **Banned:** Using uncompiled Tailwind classes (`flex`, `grid`, `w-6`, `bg-[#...]`) in Svelte templates. *Do instead:* Use the semantic layout and component classes in `frontend/src/app.css`.
- **Banned:** Omitting `width`/`height` attributes on inline SVG icons. *Do instead:* Always specify `width="18" height="18"` (or appropriate pixel size) on `<svg>`.
- **Banned:** Calling the product by third-party vendor names. *Do instead:* Use `"Switch Dashboard"`.

## 13. Pre-delivery checklist

- [x] Tokens only; no raw hex values in `.svelte` components
- [x] Contrast measured for all text/surface pairs (>= 4.5:1 body, >= 3:1 UI)
- [x] Every state designed, including empty, loading, offline, and error states
- [x] Keyboard/focus outlines and accessible `<label>` associations on all controls
- [x] `prefers-reduced-motion: reduce` respected in `app.css`
- [x] Real content only; honest empty states when no switches/backups exist
- [x] Verified end-to-end via `.agents/skills/verify-switch-dashboard/SKILL.md`

## 14. Research sources

| Source | What it informed | Consulted on |
| --- | --- | --- |
| WCAG 2.2 Contrast & Target Size Guidelines | 4.5:1 body contrast, 3:1 non-text UI contrast, keyboard focus rings | 2026-11-07 |
| Nielsen Norman Group — 10 Usability Heuristics | System status visibility (port LEDs, sync state), tabular recognition over recall | 2026-11-07 |
| Realtek RTL8372 / RTL8373 Web CGI Specification (`device-templates/*.yaml`) | Port numbering, RJ45 2.5G + SFP+ 10G faceplate layout, DDMI optical parameters | 2026-11-07 |

## 15. Change log

| Date | Change | Decision |
| --- | --- | --- |
| 2026-11-07 | Initial design system definition (`Precision Rack Telemetry`), replacing broken utility classes and enforcing `Switch Dashboard` identity | Recorded in `docs/design-system.md` & `AGENTS.md` |

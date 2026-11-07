# F02 — 1U Switch Faceplate, Port Telemetry & SFP+ DDMI

## Preconditions
- Authenticated session from `F01`.
- Mock Horaco switch configured in `switches[0]` pointing to `127.0.0.1:<mock_port>` (`enabled: true`, `port_count: 6`).

## Steps & Assertions
1. **Immediate Poll & Horaco CGI Scrape**:
   - Saving `POST /api/config` triggers an immediate poll cycle (`s.poller.TriggerPoll()`).
   - Poll engine authenticates against `/login.cgi` and scrapes `/info.cgi`, `/port.cgi`, `/port.cgi?page=stats`, and `/mac.cgi?page=fwd_tbl`.
2. **Switch & Port Telemetry Verification**:
   - `GET /api/switches` returns `switches[0]` with `status: "online"`, `model: "ZX-SWTGW215AS"`, and `ports` array of length `6`.
   - Port 1 reports `speed: "2.5G"` (`Link Up`), Port 5 reports `speed: "10G"` (`Link Up`).
   - `POST /api/switches/<ip>/refresh_mac` refreshes the L2 MAC FDB table (`count: 3`).
3. **Port Note Persistence**:
   - `POST /api/notes` with `{"key": "<ip>:5", "note": "10G Fiber Core Uplink"}` returns `200 OK`.
4. **SFP+ DDMI Optical Diagnostics**:
   - `GET /api/switches/<ip>/transceiver` scrapes `/transceiver.cgi` on demand, decodes 16-bit register pairs, and returns `200 OK` with `temperature`, `voltage`, `current`, `tx_power`, and `rx_power`.

# F03 — L2 Topology Map & Layout Persistence

## Preconditions
- Switch `127.0.0.1:<mock_port>` polled with MAC FDB entries populated from `/mac.cgi`.

## Steps & Assertions
1. **Passive L2 Graph Construction**:
   - `GET /api/topology` returns `nodes` and `links`.
   - `nodes` contains the managed switch node (`type: "switch"`) and 3 discovered L2 client nodes connected to its ports (`Port 1`, `Port 2`, `Port 5`).
   - `links` connects each client node to the switch with port and link speed metadata (`2.5G`, `1G`, `10G`).
2. **Canvas Coordinate Persistence**:
   - `POST /api/config/settings` with `{"map_positions": {"<ip>": {"x": 420, "y": 260}}}` returns `200 OK`.
   - Subsequent `GET /api/config/settings` returns the persisted `map_positions`.

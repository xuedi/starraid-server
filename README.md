# server

The StarRaid authoritative game server (Go). See [../docs/server.md](../docs/server.md).

The sole gameplay authority and primary writer of live state: it owns the in-memory active
slice of the world, runs the adaptive tick loop, validates every action, and persists to
PostgreSQL (the single source of truth). Clients and NPCs both connect over the wire
[`protocol`](../protocol) (Protobuf over TCP).

```sh
go run ./cmd/server        # or: just run-server  (from the meta repo)
```

Config via env: `STARRAID_LISTEN` (game port), `STARRAID_ADMIN` (admin surface),
`DATABASE_URL` (PostgreSQL DSN).

Layout: `cmd/server` (entry), `internal/config`, `internal/game` (world + tick loop). The
schema + migrations will live here too (the server owns them — see docs/database.md).

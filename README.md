# server

The StarRaid authoritative game server (Go). See [../docs/server.md](../docs/server.md).

The sole gameplay authority and primary writer of live state: it owns the in-memory active
slice of the world, runs the adaptive tick loop, validates every action, and persists to
PostgreSQL (the single source of truth). Clients and NPCs both connect over the wire
[`protocol`](../protocol) (Protobuf over TCP).

## Prerequisites

- Go 1.26+
- Docker (for the PostgreSQL dev database)
- A local checkout of the [`protocol`](../protocol) repo (its generated Go bindings are
  committed, so no `protoc` is needed just to build the server)

## Getting started

```sh
just install     # wire the local Go workspace to the protocol checkout + fetch deps
just db-up       # start PostgreSQL in Docker
just run         # run the server  (or: just build, just test)
```

`just install` writes a local `go.work` that points at the `protocol` repo. By default it
looks for a sibling checkout (`../protocol`); if yours lives elsewhere, set the path:

```sh
just protocol_path=/path/to/protocol install
# or persistently:
export STARRAID_PROTOCOL_PATH=/path/to/protocol
```

`go.work` is gitignored — it is per-developer local config, never committed. Run `just` to
list every recipe (`build`, `run`, `test`, `fmt`, `vet`, `db-up`/`db-down`/`db-logs`, `clean`).

## Configuration (env)

| Var | Default | Purpose |
|-----|---------|---------|
| `STARRAID_LISTEN` | `:60000` | game wire-protocol port (Protobuf over TCP) |
| `STARRAID_ADMIN` | `:8080` | admin surface for the admin tool |
| `DATABASE_URL` | `postgres://starraid:starraid@localhost:5432/starraid?sslmode=disable` | PostgreSQL DSN |
| `STARRAID_HANDSHAKE_TIMEOUT` | `10s` | version+login handshake read deadline |
| `STARRAID_DEV_USER` / `STARRAID_DEV_SECRET` | `dev` / *(empty)* | dev-auth stub; an empty secret rejects all logins |

Login end-to-end: start the server with a dev secret, then drive it with the
[`npc`](../npc) bot.

```sh
STARRAID_DEV_SECRET=s3cr3t just run
# in another shell, from the npc repo:
just run -user dev -secret s3cr3t
```

## Layout

`cmd/server` (entry) · `internal/config` · `internal/wire` (framing + codec) ·
`internal/auth` (authenticator + dev stub) · `internal/session` (handshake state machine) ·
`internal/game` (world + tick loop). The DB schema + migrations will live here too (the server
owns them — see [../docs/database.md](../docs/database.md)).

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Go REST API for todos with accounts/auth. [ARCHITECTURE.md](ARCHITECTURE.md) is the authoritative design doc (read it before adding a feature, module or backend); [README.md](README.md) documents the HTTP contract, config keys and error codes.

## Commands

```bash
make run                # go run ./cmd/api (DB_TYPE=memory by default; .env is auto-loaded by the Makefile)
make build              # bin/api and bin/migrate
make test               # go test -race ./...  — DB integration tests skip themselves
make test-integration   # Docker Postgres + Mongo replica set, full suite, then e2e on each backend
make test-db-down
make fmt                # gofmt -s -w .
make migrate            # go run ./cmd/migrate, for DB_TYPE=postgres|mongo
make sqlc               # regenerate pgstore/sqlcgen from each module's queries.sql
make goose status       # raw goose against DATABASE_URI
```

CI (`.github/workflows/ci.yml`) also runs `go vet ./...` and `govulncheck`.

Single test / package:
```bash
go test -race -run TestName ./internal/modules/identity/application
# A store test against a real DB (start DBs with `docker compose -f docker-compose.test.yml up -d --wait`):
TEST_POSTGRES_URI='postgres://postgres:postgres@localhost:55432/postgres?sslmode=disable' \
TEST_MONGO_URI='mongodb://localhost:57017/?directConnection=true' \
  go test -race -count=1 ./internal/modules/todos/adapters/persistence/...
# End-to-end suite on a real backend:
TEST_E2E_BACKEND=postgres TEST_POSTGRES_URI=... go test -race -count=1 ./cmd/api
```

Running the API requires `JWT_PRIVATE_KEY_PATH` / `JWT_PUBLIC_KEY_PATH` (RSA PEM pair) in every mode. Without `SMTP_HOST`, verification/reset emails go to the log. `tests/api.http` exercises every endpoint.

## Architecture

Feature-based **modular monolith** with **hexagonal (ports & adapters)** layering inside each module and **CQRS** in the application layer.

- `cmd/api` — the composition root and the only place that picks implementations. `storage.go` builds every repository adapter + `tx.Runner` per `DB_TYPE` (memory | mongo | postgres); `app.go` wires modules and the router. `app_test.go` is the end-to-end suite over the real router.
- `cmd/migrate` — applies Postgres (embedded goose SQL) or Mongo migrations; `migrate generate ...` drives the Mongo codegen.
- `internal/modules/{identity,todos}` — each has `domain/` (entities, invariants; imports only `platform/apperrors`), `application/` (`ports.go`: write repositories + read-only readers; `command/`: one struct + handler per write; `query/`: one struct + handler per read), `transport/httpapi/` (endpoints: run a command, read the response with a query), `adapters/` (port implementations), and `<module>.go` as its public API (`New` building `Commands`/`Queries`, `RegisterRoutes`, cross-module methods). `todos` covers todos, projects and collections.
- `internal/platform` — shared technical kernel with no business concepts: `apperrors`, `auth` (Principal, bearer middleware), `config`, `database/*`, `httpx`, `ids`, `ratelimit`, `tx`.

Rules that matter when changing code:

- **Dependencies point inward.** `application` never imports adapters, HTTP or drivers. HTTP handlers live under `transport/`, never `adapters/`.
- **CQRS.** A write is a command handler that returns at most a new ID or issued credentials; a read is a query handler that depends only on reader ports (so it can't write). `command` and `query` never import each other. Endpoints use `httpx.Endpoint` / `auth.ForUser` and decode into transport-owned request types, never straight into commands.
- **Cross-module calls go through a gateway.** identity needs todos (overview, account deletion) via its own `TodoDeleter` (commands) / `TodoLister` (queries) ports, implemented in `identity/adapters/todosgateway` by calling the public `todos.Module`. Never import another module's internals or touch its tables.
- **Every backend must behave identically.** Each module has a store contract in `adapters/persistence/storetest`, run by memstore, mongostore and pgstore. A new repository or reader method needs: the port, all three stores, a storetest case, and for Postgres a query in that module's `pgstore/queries.sql` + `make sqlc` (never hand-edit `sqlcgen/`).
- **Transactions travel in the context.** `tx.Runner.WithinTx(ctx, fn)`; repositories pick up the tx from ctx (`postgres.Executor`, Mongo session ctx), nested calls join the outer tx. Multi-record commands (register, refresh rotation, logout-all, password reset/change, deleting a project or collection, account deletion) must use one unit of work. Mongo txs need a replica set; on standalone it falls back to `tx.None`.
- **Errors become HTTP exactly once.** Domain/application return `apperrors` values with a status and stable `code`; adapters map driver errors to port errors (`ErrNotFound`, `ErrInvalidID`, `ErrEmailTaken`); handlers call `httpx.RespondErr`. Non-app errors and 5xx are logged and returned as generic `500 INTERNAL`.
- **Todos are owner-scoped.** Every todos-module repository call takes the user ID; a foreign record must look like a missing one (`NOT_FOUND`, `PROJECT_NOT_FOUND`, …). Filtering/sorting semantics are defined once in `domain.Query`; DB stores translate it and the contract checks they match.
- **Auth**: RS256 access JWTs carry a token version (`ver`) checked against the user on every request, so revocation is immediate. Refresh tokens are opaque, stored as SHA-256, rotated per family; reuse revokes the family.
- **Migrations**: Postgres in `internal/platform/database/postgres/migrations` (numbered goose SQL, also the sqlc schema); Mongo as Go files in `internal/platform/database/mongodb/migrations/versions` (contiguous versions, registered via blank import) plus indexes in `schema/*.json`. Released migrations are never edited — add a new file, for both backends.
- IDs are 24-hex ObjectIDs on memory/Mongo and UUIDs on Postgres; parse with `platform/ids`.

When adding a use case or endpoint, also cover it in `application` tests and `cmd/api` end-to-end tests (`app_test.go`, `organize_test.go`), and document it in the README. ARCHITECTURE.md has step-by-step checklists for adding a use case, module, or storage backend.

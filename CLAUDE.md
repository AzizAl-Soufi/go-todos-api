# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Go REST API for todos with accounts (Firebase Authentication sign-in plus passkeys), teams with roles and invitations, and audit logs. [ARCHITECTURE.md](ARCHITECTURE.md) is the authoritative design doc (read it before adding a feature, module or backend); [README.md](README.md) documents the HTTP contract, config keys and error codes.

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

Running the API requires `FIREBASE_PROJECT_ID` in every mode; `FIREBASE_CREDENTIALS_FILE` (service account) is optional and enables passkey sign-in. Without `SMTP_HOST`, invitation emails go to the log. `tests/api.http` exercises every endpoint.

## Architecture

Feature-based **modular monolith** with **hexagonal (ports & adapters)** layering inside each module and **CQRS** in the application layer.

- `cmd/api` — the composition root and the only place that picks implementations. `storage.go` builds every repository adapter + `tx.Runner` per `DB_TYPE` (memory | mongo | postgres); `app.go` wires modules and the router. `app_test.go` is the end-to-end suite over the real router.
- `cmd/migrate` — applies Postgres (embedded goose SQL) or Mongo migrations; `migrate generate ...` drives the Mongo codegen.
- `internal/modules/{identity,todos,teams,audit}` — each has `domain/` (entities, invariants; imports only `platform/apperrors`, plus `platform/access` for roles), `application/` (`ports.go`: write repositories + read-only readers; `command/`: one struct + handler per write; `query/`: one struct + handler per read), `transport/httpapi/` (endpoints: run a command, read the response with a query), `adapters/` (port implementations), and `<module>.go` as its public API (`New` building `Commands`/`Queries`, `RegisterRoutes`, cross-module methods). `todos` covers todos, projects and collections (personal or team-owned); `teams` covers teams, members, roles, invitations; `audit` is a leaf module holding the team audit log and personal security log.
- `internal/platform` — shared technical kernel with no business concepts: `access` (roles, `Actor` = caller + team roles, `Resolver` port, middleware), `apperrors`, `auth` (Principal, bearer middleware), `config`, `database/*`, `httpx` (incl. `ClientInfo` for audit IP/UA), `ids`, `mail` (Sender: log/SMTP), `opaque` (single-use tokens), `ratelimit`, `tx`.

Rules that matter when changing code:

- **Dependencies point inward.** `application` never imports adapters, HTTP or drivers. HTTP handlers live under `transport/`, never `adapters/`.
- **CQRS.** A write is a command handler that returns at most a new ID or issued credentials; a read is a query handler that depends only on reader ports (so it can't write). `command` and `query` never import each other. Endpoints use `httpx.Endpoint` / `auth.ForUser` and decode into transport-owned request types, never straight into commands.
- **Cross-module calls go through a gateway.** The caller declares a port with its own types and an `adapters/<other>gateway` implements it via the other module's public package: identity → todos (`TodoDeleter`/`TodoLister`), identity → teams (`TeamMemberships`), teams → todos (`TeamData`), identity/teams/todos → audit (`AuditLog`). Never import another module's internals or touch its tables, and keep the graph acyclic: todos and audit get team roles only from `access.Actor` (teams implements `access.Resolver`), never by importing teams.
- **Team authorization.** Todos/teams/audit routes run behind `auth.Middleware` + `access.Middleware(teams.Resolver())`; endpoints use `access.ForActor`. In todos, reads are limited by `domain.Scope` (personal + the actor's teams; `application.ScopeOf`), and commands check the role for team records (`CanEdit` create/update, `CanManage` delete projects/collections). The teams module checks roles against the stored membership. Non-members get `TEAM_NOT_FOUND`, low roles `INSUFFICIENT_ROLE`.
- **Audit inside the unit of work.** Changes to team records and team/membership changes record an event through the module's `AuditLog` port in the same `WithinTx` as the change (todos: `commit`); identity records security events. Personal records aren't audited.
- **Every backend must behave identically.** Each module has a store contract in `adapters/persistence/storetest`, run by memstore, mongostore and pgstore. A new repository or reader method needs: the port, all three stores, a storetest case, and for Postgres a query in that module's `pgstore/queries.sql` + `make sqlc` (never hand-edit `sqlcgen/`).
- **Transactions travel in the context.** `tx.Runner.WithinTx(ctx, fn)`; repositories pick up the tx from ctx (`postgres.Executor`, Mongo session ctx), nested calls join the outer tx. Multi-record commands (Firebase sign-in create/link, logout-all, passkey registration and sign-in, deleting a project or collection, every team command, audited changes, team and account deletion) must use one unit of work. Mongo txs need a replica set; on standalone it falls back to `tx.None`.
- **Errors become HTTP exactly once.** Domain/application return `apperrors` values with a status and stable `code`; adapters map driver errors to port errors (`ErrNotFound`, `ErrInvalidID`, `ErrEmailTaken`); handlers call `httpx.RespondErr`. Non-app errors and 5xx are logged and returned as generic `500 INTERNAL`.
- **Todos are scope-checked.** Every todos-module repository call takes a `domain.Scope`; a record outside it must look like a missing one (`NOT_FOUND`, `PROJECT_NOT_FOUND`, …). `DeleteByUser` removes personal records only; team records outlive their creator (no FKs to users/teams across modules). Filtering/sorting semantics are defined once in `domain.Query`; DB stores translate it and the contract checks they match.
- **Auth**: clients sign in with Firebase (Google, email/password) and send Firebase ID tokens on every request; `identity` verifies them (adapters/firebase), loads the account by `firebaseUid` and refuses sign-ins older than the logout-all cutoff (`sessionsValidAfter`). `POST /auth` (ID token as Bearer header, read by the handler since the account may not exist yet) creates the account or links a pre-Firebase one by verified email. Passkey sign-in returns a Firebase custom token. Tests use a fake Firebase (no network).
- **Migrations**: Postgres in `internal/platform/database/postgres/migrations` (numbered goose SQL, also the sqlc schema); Mongo as Go files in `internal/platform/database/mongodb/migrations/versions` (contiguous versions, registered via blank import) plus indexes in `schema/*.json`. Released migrations are never edited — add a new file, for both backends.
- IDs are 24-hex ObjectIDs on memory/Mongo and UUIDs on Postgres; parse with `platform/ids`.

When adding a use case or endpoint, also cover it in `application` tests and `cmd/api` end-to-end tests (`app_test.go`, `organize_test.go`, `teams_test.go`, `passkeys_test.go`), and document it in the README. ARCHITECTURE.md has step-by-step checklists for adding a use case, module, or storage backend.

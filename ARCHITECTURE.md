# Architecture

How the Todos API is put together and why. Read this before adding a feature, a module or a storage backend. For running the service and the HTTP contract, see [README.md](README.md).

## Table of Contents

- [Overview](#overview)
- [Layout](#layout)
- [Modules](#modules)
  - [Layers inside a module](#layers-inside-a-module)
  - [Commands and queries (CQRS)](#commands-and-queries-cqrs)
  - [Dependency rules](#dependency-rules)
  - [identity](#identity)
  - [todos](#todos)
  - [Talking across modules](#talking-across-modules)
- [Platform (shared kernel)](#platform-shared-kernel)
- [Composition root](#composition-root)
- [Request lifecycle](#request-lifecycle)
- [Persistence](#persistence)
  - [Three backends, one contract](#three-backends-one-contract)
  - [Transactions (unit of work)](#transactions-unit-of-work)
  - [Migrations](#migrations)
- [Errors](#errors)
- [Security design](#security-design)
- [Testing strategy](#testing-strategy)
- [Extending the system](#extending-the-system)
  - [Add a use case](#add-a-use-case)
  - [Add a module](#add-a-module)
  - [Add a storage backend](#add-a-storage-backend)
- [Decisions and trade-offs](#decisions-and-trade-offs)

## Overview

The API is a **feature-based modular monolith**: one binary, split into modules that each own one business capability (a bounded context). Inside every module the code follows **Clean Architecture / Hexagonal (ports & adapters)**: business rules in the middle, technology at the edges, and dependencies pointing inwards only. The application layer is split by **CQRS**: every write is a command with its own handler, every read a query with its own handler.

The goals behind that shape:

- **Change in one place.** Everything about todos lives under `modules/todos`; everything about accounts and sessions under `modules/identity`.
- **Swap technology without touching business code.** The same use cases run on memory, MongoDB or PostgreSQL, and send mail through a log or SMTP, chosen at startup.
- **Split later if needed.** Modules only talk through small public APIs, so one could become its own service without rewriting the others.

## Layout

```
cmd/
├── api/                      composition root: config → storage → modules → router → server
└── migrate/                  migration CLI: PostgreSQL (embedded SQL) and MongoDB (migrator + codegen)

internal/
├── modules/                  one directory per feature (bounded context)
│   ├── identity/             accounts, passwords, sessions, email verification, authentication
│   │   ├── identity.go       the module's public API (New, RegisterRoutes, Authenticator)
│   │   ├── domain/           entities + invariants (User, Session, OneTimeToken, errors)
│   │   ├── application/      ports.go: write repositories and read-only readers
│   │   │   ├── command/      write use cases: one command struct + handler each
│   │   │   └── query/        read use cases: one query struct + handler each
│   │   ├── transport/
│   │   │   └── httpapi/      driving adapter: HTTP handlers, request/response shapes
│   │   └── adapters/         driven adapters
│   │       ├── persistence/  memstore | mongostore | pgstore (+ queries.sql, sqlcgen/), storetest/
│   │       ├── security/     bcrypt hasher, RS256 JWT issuer/verifier
│   │       ├── mail/         log mailer, SMTP mailer
│   │       └── todosgateway/ the only bridge to the todos module
│   └── todos/                todos, projects, collections; same shape: todos.go, domain/,
│                             application/{command,query}/, transport/, adapters/persistence/
│
└── platform/                 shared kernel: technical building blocks, no business concepts
    ├── apperrors/            typed application errors (HTTP status + stable code)
    ├── auth/                 Principal, Authenticator port, bearer-token middleware
    ├── config/               env config (keys derived from struct tags)
    ├── database/             memory | mongodb | postgres clients, tx runners, migrations, dbtest/
    ├── httpx/                JSON responses and errors, request ID, access log, recovery, body limit
    ├── ids/                  ObjectID / UUID parsing
    ├── ratelimit/            per-IP token bucket
    ├── tx/                   unit-of-work port (tx.Runner)
    └── testutil/             shared test helpers
```

## Modules

### Layers inside a module

```
            driving side                                   driven side
   ┌──────────────────────┐                        ┌───────────────────────────┐
   │ transport/httpapi    │                        │ adapters/persistence      │
   │ (HTTP → command,     │                        │ adapters/security, mail   │
   │  then query)         │                        │ adapters/todosgateway     │
   └──────────┬───────────┘                        └─────────────▲─────────────┘
              │ calls                                            │ implements
   ┌──────────▼───────────────────────────────────────────────────┴────────────┐
   │ application   command/ (writes)  query/ (reads)  ports.go (interfaces)    │
   └──────────────────────────────────┬────────────────────────────────────────┘
                                      │ uses
                                      ▼
   ┌───────────────────────────────────────────────────────────────────────────┐
   │ domain        entities, value rules, domain errors                        │
   └───────────────────────────────────────────────────────────────────────────┘
```

| Layer | Contains | Knows about |
|-------|----------|-------------|
| `domain` | Entities (`User`, `Session`, `OneTimeToken`, `Todo`, `Project`, `Collection`), invariants (`NormalizeEmail`, `ValidatePassword`, `NormalizeTitle`), domain errors | Nothing but `platform/apperrors` |
| `application` | `ports.go`: the **ports**, split into write repositories (`UserRepository`, `TodoRepository`, `Mailer`, …) and read-only readers (`UserReader`, `TodoReader`, …) | `domain` |
| `application/command` | One struct per write (`Register`, `CreateTodo`, …) and its handler | `application` ports, `domain`, `platform/tx` |
| `application/query` | One struct per read (`GetOverview`, `ListTodos`, …), its handler and read views | `application` read ports, `domain`, `platform/auth` |
| `transport/httpapi` | Endpoints: decode request → run one command → read the response with a query | `command`, `query`, `domain` errors, `platform/httpx`, `platform/auth` |
| `adapters/*` | Implementations of the ports: databases, bcrypt, JWT, SMTP, other modules | `application` ports, drivers, `platform/database` |
| `<module>.go` | The module's public API: `New`, `RegisterRoutes`, plus what other modules may call | Its own layers |

### Commands and queries (CQRS)

Every use case is either a **command** (it changes state) or a **query** (it reads state), never both:

```
POST /todos ─► decode ─► command.CreateTodo ─► CreateTodoHandler ─► TodoRepository.Create
                               │ returns the new ID only
                               ▼
               query.GetTodo ─► GetTodoHandler ─► TodoReader.Get ─► JSON response
```

- **A command is a plain struct** (`command.CreateTodo{UserID, Changes}`) handled by its own handler (`CreateTodoHandler.Handle(ctx, cmd)`). It returns an error and at most what only the write can produce: a new ID, or freshly issued credentials (`Register`, `Login`, `Refresh`, `ChangePassword`). Everything else is read back through a query.
- **A query is a plain struct** (`query.ListTodos{UserID, Query, Overdue, IncludeArchived}`) handled by its own handler, returning data. Query handlers depend only on read-only ports (`TodoReader`, `ProjectReader`, `UserReader`, `AccessTokenVerifier`), so the compiler guarantees they can't write.
- **Each module builds its handlers once**: `Module.Commands` (`command.Handlers`) and `Module.Queries` (`query.Handlers`). The transport receives both and composes them per endpoint.
- **The read side has its own views.** `query.ProjectView` and `query.CollectionView` add the progress of their todos, and `query.Overview` adds the user's todos. The write side never computes them.
- **One store serves both sides.** Each persistence adapter implements the repository and the reader of its aggregate (`application.TodoStore` = `TodoRepository` + `TodoReader`). The split is in the ports, not the database. A separate read model (a cache, a projection) could later replace the readers without touching commands.
- Cross-module calls follow the same split: identity's account deletion uses the `TodoDeleter` port, its overview uses `TodoLister`.

### Dependency rules

- `domain` imports nothing from the module or other modules (only `platform/apperrors`).
- `application` depends on `domain` and declares the ports it needs. It never imports adapters, HTTP or drivers.
- `query` never imports `command` and depends only on read ports, so a query cannot change state; `command` never imports `query`.
- `transport/*` (driving side) calls application use cases; `adapters/*` (driven side) implement application ports. HTTP handlers never live under `adapters/`.
- Modules talk to each other only through a module's public package, via a gateway adapter on the caller's side. No module imports another module's internals or reads its tables.
- Each module owns its SQL (`adapters/persistence/pgstore/queries.sql` → its own `sqlcgen` package). The schema itself is shared in `platform/database/postgres/migrations`.
- `platform` never imports `modules`.
- Only `cmd/api` knows every concrete adapter.

### identity

Accounts, passwords, sessions, email verification and authentication of every request.

| Port (application/ports.go) | Adapters |
|-----------------------------|----------|
| `UserRepository` (commands) / `UserReader` (queries) | `memstore.Users`, `mongostore.Users`, `pgstore.Users` |
| `SessionRepository` | `memstore.Sessions`, `mongostore.Sessions`, `pgstore.Sessions` |
| `OneTimeTokenRepository` (used twice: email verifications, password resets) | `memstore.OneTimeTokens`, `mongostore.OneTimeTokens`, `pgstore.OneTimeTokens` |
| `PasswordHasher` | `security.BcryptHasher` |
| `AccessTokenIssuer` (commands) / `AccessTokenVerifier` (queries) | `security.JWT` (RS256) |
| `Mailer` | `mail.LogMailer`, `mail.SMTPMailer` |
| `TodoDeleter` (commands) / `TodoLister` (queries) | `todosgateway.Gateway` → `todos.Module` |
| `tx.Runner` | `tx.None`, `postgres.TxRunner`, `mongodb.TxRunner` |

Commands: `Register`, `Login`, `Refresh`, `Logout`, `LogoutAll`, `VerifyEmail`, `ResendVerification`, `RequestPasswordReset`, `ResetPassword`, `ChangePassword`, `DeleteAccount`. Shared steps (start a session, issue or redeem a one-time token, hash or check a password, revoke every credential) live once in `command/command.go`.

Queries: `GetUser`, `GetOverview`, and `Authenticate`, whose handler also implements `platform/auth.Authenticator` for the auth middleware.

### todos

Organizing work: todos, projects (lists of todos) and collections (groups of projects). They form one module because they are one consistency boundary: deleting a project deletes its todos, a project's progress is computed from its todos, and both need to change in one transaction. Everything is scoped to the owner: every repository call takes the user ID, and a foreign record looks exactly like a missing one.

| Port (application/ports.go) | Adapters |
|-----------------------------|----------|
| `TodoRepository` (commands) / `TodoReader` (queries) | `memstore.Todos`, `mongostore.Todos`, `pgstore.Todos` |
| `ProjectRepository` / `ProjectReader` | `memstore.Projects`, `mongostore.Projects`, `pgstore.Projects` |
| `CollectionRepository` / `CollectionReader` | `memstore.Collections`, `mongostore.Collections`, `pgstore.Collections` |
| `tx.Runner` | `tx.None`, `postgres.TxRunner`, `mongodb.TxRunner` |

Domain (`domain/`):

- `Todo` with `Status` (`pending` … `cancelled`), `Priority`, deadline, notes and tags. `New` and `Apply` keep the invariants, for example `completedAt` is set exactly while the status is `done`. Creating and updating take the same `Changes` (a create just requires the title); `Patch[T]` marks fields that an update can clear (`dueAt`, `projectId`).
- `Meta` (name, description, icon, color, archived) is shared by `Project` and `Collection`, and likewise created and updated from the same `MetaChanges`.
- `Query` defines filtering (`Matches`) and ordering (`Compare`) for todo lists, and `Progress` the per-project summary. The memory store applies them directly. The database stores translate them to SQL and MongoDB filters, and the store contract checks that every backend returns exactly what `Query` says.

Commands: `CreateTodo`, `UpdateTodo`, `DeleteTodo`, `CreateProject`, `UpdateProject`, `DeleteProject`, `CreateCollection`, `UpdateCollection`, `DeleteCollection`, and `DeleteUserData` for account deletion.

Queries: `GetTodo`, `ListTodos`, `GetProject`, `ListProjects`, `GetCollection`, `ListCollections`. Project and collection queries return views enriched with `Progress`.

Public API for other modules (`todos.go`): `ListForUser` and `DeleteAllForUser`, returning the `todos.Todo` read model rather than the domain entity.

### Talking across modules

identity needs todos for the `/me` overview and for account deletion. It doesn't import the todos internals. Instead:

1. identity's `application` declares the ports it needs: `TodoDeleter` for the command side and `TodoLister` (with its own `TodoSummary` type) for the query side.
2. identity's `adapters/todosgateway` implements that port by calling the public `todos.Module`.
3. `cmd/api` wires the gateway with the todos module instance.

The dependency is one-way (identity → todos) and fits in one small file. If todos became a separate service, only the gateway would change.

## Platform (shared kernel)

`platform` holds technical building blocks every module may use. It has no business concepts: no users, no todos.

| Package | Responsibility |
|---------|----------------|
| `apperrors` | Error type carrying an HTTP status, a stable `code` and a message; constructors such as `Validation`, `NotFound`, `Unauthorized`, `InternalWrap`; `NotFoundAs` / `UnresolvedAs` turn a repository's generic not-found into a specific error |
| `auth` | `Principal` (the authenticated caller), the `Authenticator` port, the bearer-token middleware, `RequireVerifiedEmail`, and `ForUser` (an endpoint that receives the caller's user ID) |
| `config` | Loads env / `.env`; every key is declared once as a struct tag with an optional default |
| `database/*` | Connection clients, transaction runners, migrations, and `dbtest` throwaway databases for tests |
| `httpx` | `Endpoint` (a handler that returns status, body or error), JSON success and error responses, middleware chain, request ID, access log, panic recovery, body limit, JSON 404/405 |
| `ids` | Parses ObjectIDs and UUIDs, returning `ErrInvalidID` for the wrong format |
| `ratelimit` | In-process per-key token bucket and its HTTP middleware |
| `tx` | `Runner` port for units of work; `None` runs without a transaction |

## Composition root

`cmd/api` is the only place that chooses implementations:

```
main.go   run(ctx)
  └── config.LoadConfig()
  └── newApp(ctx, cfg)                                     app.go
        ├── newStorage(cfg)          by DB_TYPE            storage.go
        │     memory   → memstores + tx.None
        │     postgres → connect, optional migrate, pgstores + postgres.TxRunner
        │     mongo    → connect, optional migrate, mongostores + mongodb.TxRunner
        ├── security.NewJWTFromFiles (checks the key pair matches)
        ├── newMailer(cfg)           SMTP if SMTP_HOST is set, else log
        ├── wireModules(...)         todos.New(repo); identity.New(Deps{…, Todos: todosgateway.New(todos)})
        └── newRouter(cfg, modules)
```

## Request lifecycle

Every request passes through the same outer middleware, then route-level middleware, then a handler:

```
Recovery            panic → 500 INTERNAL (logged with stack)
└── RequestID       reuse a well-formed X-Request-ID or generate one; echo it back
    └── AccessLog   one structured log line per request (method, path, status, duration, request_id)
        └── LimitBody   1 MiB request bodies
            └── JSONErrors(mux)   unknown route → 404, wrong method → 405, both as JSON errors
                └── route middleware
                      public auth routes:   ratelimit.Middleware
                      bearer routes:        auth.Middleware(identity.Authenticator())
                      todos module routes:  + auth.RequireVerifiedEmail (if AUTH_REQUIRE_VERIFIED_EMAIL)
                      password change:      ratelimit + auth
                    └── endpoint → command handler → repositories
                                 └──→ query handler   → readers → JSON
```

`auth.Middleware` reads the bearer token and runs identity's `Authenticate` query. That checks the JWT signature and expiry, loads the user and compares the token version, so revocation is immediate. It then stores a `Principal` in the context. Handlers read it with `auth.PrincipalFrom`; the todos module never sees a token.

## Persistence

### Three backends, one contract

| Backend | Use | Notes |
|---------|-----|-------|
| memory | Local development, unit and end-to-end tests | Maps behind a mutex; values are copied in and out so callers never share state |
| PostgreSQL | Production | `pgx/v5` pool; queries written in SQL and generated with `sqlc` per module; foreign keys cascade from `users` |
| MongoDB | Production | Driver v2; one collection per aggregate; unique and TTL indexes from migrations |

Repositories follow the contracts documented on the ports. For example, a missing record is `apperrors.ErrNotFound`, a malformed ID is `apperrors.ErrInvalidID`, and `SessionRepository.Revoke` reports whether this call did the revoking.

Each module has a **store contract** in `adapters/persistence/storetest`. Every backend runs the same suite, so a behaviour the application relies on cannot differ between backends. Examples: duplicate emails, owner scoping, single-use tokens, exactly one winner when a session is revoked concurrently, transaction rollback. For todo lists the contract runs many filter and sort combinations and compares each backend's answer with `domain.Query` applied to the same fixtures.

Dynamic filters stay in static, type-checked SQL. `ListTodos` in `queries.sql` takes every filter as a parameter that is "off" when empty or NULL, for example `(cardinality(@statuses) = 0 OR status = ANY(@statuses))`, and orders with `CASE` on the sort key. sqlc generates one typed function for it. On MongoDB, missing values sort first, so "no deadline last" sorts on a computed flag in an aggregation.

The two one-time-token tables (`email_verifications`, `password_resets`) have identical shapes, so a single `OneTimeTokens` implementation per backend serves both, parameterised by table or collection.

### Transactions (unit of work)

`tx.Runner.WithinTx(ctx, fn)` starts a transaction and passes it to `fn` through the context. Repositories don't take a transaction parameter: they look it up in the context.

- **PostgreSQL**: `postgres.Executor(ctx, pool)` returns the `pgx.Tx` from the context, or the pool.
- **MongoDB**: the session context created by `WithTransaction` is passed down, and driver calls made with it join the transaction.
- **Memory**: `tx.None` runs `fn` directly.

Nested `WithinTx` calls join the outer transaction. Commands that write more than one record use one unit of work: register, refresh rotation, logout-all, password reset and change, deleting a project (with its todos), deleting a collection (detaching its projects), and account deletion. Account deletion spans both modules: the todos gateway runs inside the same transaction as the identity deletes, and the todos module's `DeleteUserData` command joins it.

MongoDB transactions need a replica set or sharded cluster. `mongodb.NewTxRunner` checks the topology at startup and falls back to `tx.None`, with a warning, on a standalone server.

### Migrations

- **PostgreSQL**: numbered goose SQL files in `platform/database/postgres/migrations`, embedded in the binaries with `embed`. `postgres.Migrate` applies them; it runs from `cmd/migrate` or at startup when `MIGRATE_ON_START=true`.
- **MongoDB**: Go migrations registered from `platform/database/mongodb/migrations/versions` (versions must be contiguous), plus indexes declared in the `schema/*.json` files, applied by the same two entry points.

Released migrations are never edited; changes go into a new file.

## Errors

Errors flow outwards as values and are turned into HTTP exactly once:

1. `domain` and `application` return `apperrors` values with a status and stable code (`EMAIL_TAKEN`, `INVALID_RESET_TOKEN`, …) or wrap unexpected failures with `apperrors.InternalWrap`.
2. Adapters translate driver errors into port errors: no rows becomes `ErrNotFound`, a unique violation becomes `ErrEmailTaken`.
3. Handlers call `httpx.RespondErr`, which writes `{"error": {"code", "message"}}`. Anything that isn't an application error, or is a 5xx, is logged with the request ID and returned as a generic `500 INTERNAL`, so internal details never leak.

## Security design

| Concern | Design |
|---------|--------|
| Passwords | bcrypt cost 12; 8–72 bytes (bcrypt silently truncates longer input, so it is rejected); unknown emails are compared against a dummy hash to keep timing equal |
| Access tokens | RS256 JWT, 15 minutes, claims `sub`, `ver`, `token_type`; only RS256 accepted; expiration required |
| Revocation | Per-request token-version check against the user. Logout-all, password reset and change, and account deletion take effect immediately |
| Refresh tokens | Opaque 256-bit random strings stored as SHA-256. Rotation inside a family; reuse revokes the family; the conditional `Revoke` guarantees one winner under concurrency |
| One-time tokens | Email verification (24 h) and password reset (1 h): random, stored hashed, single-use, at most one pending per user per kind, replaced on re-issue |
| Account enumeration | Login and forgot-password answer the same way for unknown emails; reset emails are sent after the response so SMTP latency doesn't leak |
| Abuse | Per-IP token bucket on public auth endpoints and password change; 1 MiB body limit; `X-Forwarded-For` not trusted |
| Email headers | SMTP adapter rejects CR/LF in addresses and replaces them with spaces in names |

## Testing strategy

| Level | Where | Runs on |
|-------|-------|---------|
| Domain rules | `modules/*/domain/*_test.go` | pure Go |
| Commands and queries | `modules/identity/application/usecases_test.go` | memory stores, real bcrypt (min cost) and JWT, fake mailer, test clock |
| Store contracts | `modules/*/adapters/persistence/storetest` run from each store package | memory always; PostgreSQL / MongoDB when `TEST_POSTGRES_URI` / `TEST_MONGO_URI` are set |
| Adapter edge cases | e.g. invalid IDs rejected before the database, SMTP header injection | pure Go |
| Platform | middleware, rate limiter, config, migrations, codegen | pure Go / real databases for migrations |
| End to end | `cmd/api/app_test.go`: real router and middleware over HTTP | memory, or `TEST_E2E_BACKEND=postgres\|mongo` |

`make test` runs everything that needs no database. `make test-integration` starts PostgreSQL and a MongoDB replica set in Docker (`docker-compose.test.yml`), runs the whole suite against them, then runs the end-to-end suite on each database. `platform/database/dbtest` gives every test its own freshly migrated database and drops it afterwards. CI runs both.

## Extending the system

### Add a use case

1. Put new rules in `domain` (with tests).
2. Decide whether it writes or reads. For a write, add a command struct and handler in `application/command` (and a field in `command.Handlers`); for a read, the same in `application/query`. If it needs something new from the outside world, declare a port in `ports.go` first: a repository method for commands, a reader method for queries.
3. Implement the port in each adapter. For a repository method, add it to the module's `storetest` contract so every backend is held to it; for PostgreSQL add the query to `queries.sql` and run `make sqlc`.
4. Add the endpoint and route in `transport/httpapi`: decode, run the command, read the response with a query.
5. Cover it in `application` tests and `cmd/api/app_test.go`, and document the endpoint in the README.

### Add a module

1. Create `internal/modules/<name>/` with `domain/`, `application/` (ports, `command/`, `query/`), `transport/httpapi/`, `adapters/` and a `<name>.go` public API (`New` building `Commands` and `Queries`, `RegisterRoutes`).
2. Protect routes with the `protect` middleware that `cmd/api` passes in; read the caller with `auth.PrincipalFrom`.
3. If another module needs it, expose a small method on the public API and add a gateway adapter on the caller's side. Never import another module's internals.
4. Add its tables to a new migration and its queries to its own `queries.sql` and `sqlc.yml` entry.
5. Wire it in `cmd/api/app.go` (and storage in `storage.go`).

### Add a storage backend

1. Implement every repository port of every module under `adapters/persistence/<backend>store`.
2. Make each package run its module's `storetest` contract.
3. Provide a `tx.Runner`, or `tx.None` if the database has no transactions.
4. Add a case to `newStorage` in `cmd/api/storage.go` and a value for `DB_TYPE`.

## Decisions and trade-offs

- **Modular monolith, not microservices.** One deployable keeps operations simple, and module boundaries keep the code ready to split if load or team size ever demands it.
- **CQRS in the code, not in the infrastructure.** Commands and queries are separate handlers with separate ports, which keeps every use case small and makes "reads never write" a compile-time fact. There is no separate read database or event store: one store serves both sides until a measured need for a read model appears.
- **No ORM.** SQL is written by hand and type-checked by `sqlc`; MongoDB uses the driver directly. Queries stay visible and reviewable.
- **Shared database, owned tables.** Both modules use one database (so cross-module transactions are possible), but each module touches only its own tables.
- **Token version check on every request.** It costs one user lookup per request, and buys immediate revocation without a token blacklist.
- **Opaque refresh tokens, JWT access tokens.** Access tokens verify cheaply; refresh tokens stay revocable and are useless if the database leaks.
- **In-process rate limiting.** Simple and dependency-free, but limits apply per replica. A shared store (e.g. Redis) is the next step once the API runs several replicas.
- **No caching or distributed tracing yet.** There is no measured need. Request IDs and structured logs cover correlation for now.

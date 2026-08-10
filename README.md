# Todos Stateless API

Go HTTP API for user-todo management with pluggable storage. Memory, MongoDB, PostgreSQL. RS256 JWTs. No ORM.

## Stack

- Go 1.26.5, `net/http` method patterns
- MongoDB (native driver v2) / PostgreSQL (`pgx/v5` + `sqlc` + goose) / memory (`sync.RWMutex`)
- `golang-jwt/jwt/v5`, RSA key pair
- Viper for env config

## Run

```bash
# Memory (default)
make run

# PostgreSQL
DB_TYPE=postgres DATABASE_URI=postgres://user:pass@localhost:5432/todos make goose up
DB_TYPE=postgres make run

# MongoDB
DB_TYPE=mongo DATABASE_URI=mongodb://localhost:27017 DATABASE_NAME=todos make run
```

`JWT_PRIVATE_KEY_PATH` and `JWT_PUBLIC_KEY_PATH` required in all modes. Migrations are manual.

## Architecture

Repository pattern. Domain at center. Handlers know HTTP. Services know workflows. Repositories know storage. `internal/common` knows neither users nor todos.

`cmd/api.go` composes the app. `DB_TYPE` selects the adapter: `memory` (default), `mongo`, `postgres`.

```
/
├── cmd
│   ├── api.go                       dependency wiring and HTTP routes
│   └── main.go                      config load, signal handling, server start
├── internal
│   ├── common
│   │   ├── common.go                JSON response helpers and panic recovery
│   │   ├── config/config.go         Viper environment configuration
│   │   ├── errors                  application error types and mappings
│   │   ├── middleware              JWT validation and authorization context
│   │   └── repository.go            generic repository interface
│   ├── pkg/database
│   │   ├── memory/client.go         map-backed client with RWMutex
│   │   ├── mongodb/client.go        MongoDB driver v2 client wrapper
│   │   └── postgres
│   │       ├── migrations           goose SQL migrations
│   │       ├── postgres.go           pgx/v5 pool wrapper
│   │       └── sqlc                 generated database code
│   ├── todos
│   │   ├── domain                   Todo structs, DTOs, and validation
│   │   ├── handler                  todo HTTP handlers
│   │   ├── repository               port and memory/MongoDB/PostgreSQL adapters
│   │   └── service                  todo workflows and ownership checks
│   └── users
│       ├── domain                   User structs, DTOs, and validation
│       ├── handler                  user HTTP handlers
│       ├── repository               port and memory/MongoDB/PostgreSQL adapters
│       └── service                  registration, auth, and token workflows
├── m.md                             ignored/stale workspace artifact
├── m.test                           repository artifact, not a Go test
├── secrets                           RSA key files loaded by JWT middleware
├── sqlc.yml                         sqlc generation configuration
└── tests                             manual HTTP request examples
```


## Auth

RS256 access tokens (15min) and refresh tokens (7 days). `RequireAuth` verifies signature, expiry, `token_type == access`, stores `CustomerInfo` in context. Services re-fetch the user from DB on protected calls.

No token versioning, rotation, blacklist, or logout endpoint.

## Endpoints

| Method | Path | Auth | Handler |
|--------|------|------|---------|
| GET | `/health` | No | inline |
| POST | `/register` | No | `UsersHandler.Register` |
| POST | `/refresh-token` | No | `UsersHandler.RefreshToken` |
| POST | `/auth` | Yes | `UsersHandler.Auth` |
| DELETE | `/user` | Yes | `UsersHandler.Delete` |
| GET | `/overview` | Yes | `UsersHandler.GetOverview` |
| POST | `/todos` | Yes | `TodosHandler.Create` |
| GET | `/todos` | Yes | `TodosHandler.GettAll` |
| GET | `/todos/{id}` | Yes | `TodosHandler.Get` |
| PUT | `/todos/{id}` | Yes | `TodosHandler.Update` |
| DELETE | `/todos/{id}` | Yes | `TodosHandler.Delete` |

Go 1.22 method patterns. `r.PathValue("id")` for params.

## Errors

`ValidationError` (400), `NotFoundError` (404), `ConflictError` (409), `UnauthorizedError` (401), `InternalError` (500). Handlers use `apperrors.From(err)`. Panics are recovered at the router root.

## Schema

**MongoDB**: `users`, `todos` collections. `bson.ObjectID` internally, strings at domain boundary. No indexes declared.

**PostgreSQL**: `users` (UUID PK, name, email UNIQUE, created_at), `todos` (UUID PK, user_id FK CASCADE, title, completed, created_at).

**Memory**: `map[string]*User`, `map[string]*Todo` with `sync.RWMutex`. Returns pointers held in maps.

## Config

```
PORT
DEBUG
DB_TYPE
DATABASE_URI
DATABASE_NAME
JWT_PRIVATE_KEY_PATH
JWT_PUBLIC_KEY_PATH
```

No defaults for `PORT` or RSA keys. `DATABASE_NAME` required for MongoDB.

## TO-DO

- Add password authentication and password hashing. The current registration DTO accepts only name and email.
- Add email verification.
- Add rate limiting, caching, and tracing if operational requirements justify them.
- Add token versioning, refresh-token rotation, logout, and revocation checks.
- Add automatic migration execution or an explicit deployment migration step.
- Add handler, service, repository, JWT, middleware, route, migration, and adapter integration tests. The current automated coverage is two todo DTO validation tests.
- Add a unique MongoDB email index. Current uniqueness is implemented with find-then-insert.
- Make account deletion transactional across todos and the user.
- Stop exposing stored memory-repository pointers outside the lock scope.

- `domain/` — structs and validation only. No imports to HTTP or storage.
- `repository/interface.go` — the port. Adapters live in the same package.
- `service/` — imports repository port + middleware context. No HTTP.
- `handler/` — imports service + domain DTOs + `common`. No storage driver.
- `common/` — config, errors, middleware, response writers. Shared by both features.

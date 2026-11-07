# Todos Stateless API

Go HTTP API for organizing todos, like Google Tasks with a touch of Notion: todos live in **projects** (lists), projects can be grouped into **collections**, and every todo has a status, priority, deadline, notes and tags. Built as a **feature-based modular monolith** with **Clean Architecture / Hexagonal (ports & adapters)** inside each module. Pluggable storage: memory, MongoDB, PostgreSQL. Password auth with reset and change, RS256 access tokens, rotating refresh tokens, email verification. No ORM.

Design: [ARCHITECTURE.md](ARCHITECTURE.md) · Changes: [CHANGELOG.md](CHANGELOG.md)

## Table of Contents

- [Stack](#stack)
- [Getting Started](#getting-started)
  - [Run](#run)
  - [Configuration](#configuration)
- [Architecture](#architecture)
- [Authentication](#authentication)
  - [Access and refresh tokens](#access-and-refresh-tokens)
  - [Verifying email](#verifying-email)
  - [Resetting and changing passwords](#resetting-and-changing-passwords)
  - [Rate limiting](#rate-limiting)
- [Organizing Todos](#organizing-todos)
  - [Collections, projects and the inbox](#collections-projects-and-the-inbox)
  - [Todo fields](#todo-fields)
  - [Archiving](#archiving)
  - [Progress](#progress)
- [Database Schema](#database-schema)
  - [Enums](#enums)
  - [PostgreSQL tables](#postgresql-tables)
  - [MongoDB collections](#mongodb-collections)
  - [Migrations](#migrations)
- [Errors](#errors)
  - [Error format](#error-format)
  - [Common error codes](#common-error-codes)
- [Endpoints](#endpoints)
  - [Conventions](#conventions)
  - [Health](#health)
    - [GET `/health`](#get-health)
  - [Auth](#auth)
    - [POST `/auth/register`](#post-authregister)
    - [POST `/auth/login`](#post-authlogin)
    - [POST `/auth/refresh`](#post-authrefresh)
    - [POST `/auth/logout`](#post-authlogout)
    - [POST `/auth/logout-all`](#post-authlogout-all)
  - [Email verification](#email-verification)
    - [POST `/auth/verify-email`](#post-authverify-email)
    - [GET `/auth/verify-email?token={token}`](#get-authverify-emailtokentoken)
    - [POST `/auth/verify-email/resend`](#post-authverify-emailresend)
  - [Password](#password)
    - [POST `/auth/password/forgot`](#post-authpasswordforgot)
    - [POST `/auth/password/reset`](#post-authpasswordreset)
    - [POST `/auth/password/change`](#post-authpasswordchange)
  - [Account](#account)
    - [GET `/me`](#get-me)
    - [DELETE `/me`](#delete-me)
  - [Todos](#todos)
    - [POST `/todos`](#post-todos)
    - [GET `/todos`](#get-todos)
    - [GET `/todos/{id}`](#get-todosid)
    - [PATCH `/todos/{id}`](#patch-todosid)
    - [DELETE `/todos/{id}`](#delete-todosid)
  - [Projects](#projects)
    - [POST `/projects`](#post-projects)
    - [GET `/projects`](#get-projects)
    - [GET `/projects/{id}`](#get-projectsid)
    - [PATCH `/projects/{id}`](#patch-projectsid)
    - [DELETE `/projects/{id}`](#delete-projectsid)
  - [Collections](#collections)
    - [POST `/collections`](#post-collections)
    - [GET `/collections`](#get-collections)
    - [GET `/collections/{id}`](#get-collectionsid)
    - [PATCH `/collections/{id}`](#patch-collectionsid)
    - [DELETE `/collections/{id}`](#delete-collectionsid)
- [Testing](#testing)
  - [Unit tests](#unit-tests)
  - [Integration tests](#integration-tests)
- [TO-DO](#to-do)

## Stack

- Go (toolchain pinned in `go.mod`), `net/http` method patterns
- MongoDB (driver v2) / PostgreSQL (`pgx/v5` + `sqlc` + embedded `goose` migrations) / memory
- `golang-jwt/jwt/v5` (RS256 only), `bcrypt` password hashing
- Viper for env config

## Getting Started

### Run

```bash
# Memory (default)
make run

# PostgreSQL: apply migrations as a deploy step, or set MIGRATE_ON_START=true
DB_TYPE=postgres DATABASE_URI=postgres://user:pass@localhost:5432/todos make migrate
DB_TYPE=postgres DATABASE_URI=postgres://user:pass@localhost:5432/todos make run

# MongoDB (use a replica set for transactional account deletion)
DB_TYPE=mongo DATABASE_URI=mongodb://localhost:27017 DATABASE_NAME=todos make migrate
DB_TYPE=mongo DATABASE_URI=mongodb://localhost:27017 DATABASE_NAME=todos make run
```

`JWT_PRIVATE_KEY_PATH` and `JWT_PUBLIC_KEY_PATH` (PEM RSA key pair) are required in all modes. Without `SMTP_HOST`, verification and password reset emails are written to the log instead of being sent. `tests/api.http` walks through every endpoint.

### Configuration

All settings are environment variables (a `.env` file in the working directory is read too); see `.env.example`.

| Key | Default | Description |
|-----|---------|-------------|
| `PORT` | – | HTTP port |
| `DEBUG` | `false` | Debug logging |
| `APP_BASE_URL` | `http://localhost:8000` | Public API URL, used in email verification links |
| `MIGRATE_ON_START` | `false` | Apply pending migrations at startup |
| `DB_TYPE` | `memory` | `memory`, `mongo` or `postgres` |
| `DATABASE_URI` | – | Connection string (mongo, postgres) |
| `DATABASE_NAME` | – | Database name (mongo) |
| `MONGO_MIGRATIONS_CONFIG` | `./cmd/migrate/mongo.yaml` | MongoDB migrator config |
| `JWT_PRIVATE_KEY_PATH` | – | RSA private key (PEM) |
| `JWT_PUBLIC_KEY_PATH` | – | RSA public key (PEM) |
| `AUTH_REQUIRE_VERIFIED_EMAIL` | `false` | Block the todos API until the email is verified |
| `AUTH_RATE_LIMIT_PER_MINUTE` | `20` | Per-IP budget for the rate-limited auth endpoints |
| `AUTH_RATE_LIMIT_BURST` | `10` | Burst on top of that budget |
| `AUTH_PASSWORD_RESET_URL` | `APP_BASE_URL` + `/reset-password` | Page the reset email links to (see [Resetting and changing passwords](#resetting-and-changing-passwords)) |
| `SMTP_HOST` | – | SMTP server; empty means emails are logged |
| `SMTP_PORT` | `587` | SMTP port (STARTTLS when offered) |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | – | SMTP credentials |
| `SMTP_FROM` | – | Sender address |

## Architecture

A **feature-based modular monolith**: `internal/modules/identity` (accounts, passwords, sessions, email verification, authentication) and `internal/modules/todos`, on top of a shared kernel in `internal/platform`. Inside each module, **Clean Architecture / Hexagonal** layering with **CQRS** in the application layer: `domain` → `application` (ports, plus `command/` for writes and `query/` for reads, one handler per use case) → `transport/httpapi` (driving: run a command, read the result with a query) and `adapters/` (driven: persistence, security, mail, other modules). Modules talk only through each other's public API, and only `cmd/api` knows the concrete adapters.

See **[ARCHITECTURE.md](ARCHITECTURE.md)** for the layout, dependency rules, request lifecycle, transactions, persistence and testing strategy, and how to add a use case, module or storage backend.

## Authentication

### Access and refresh tokens

- **Passwords**: bcrypt (cost 12), 8–72 bytes. Login compares against a dummy hash for unknown emails, so timing doesn't reveal which accounts exist.
- **Access tokens**: RS256 JWT, 15 min, claims `sub`, `ver`, `token_type=access`. Only RS256 is accepted (no `none`, no HS256).
- **Revocation checks**: every authenticated request loads the user and compares the token's `ver` with the user's token version. Logout-all, password reset and change, and account deletion therefore take effect immediately.
- **Refresh tokens**: opaque random 256-bit strings, stored only as SHA-256 hashes, valid 7 days. Each refresh rotates the token: the old session is revoked (conditionally, so concurrent refreshes have exactly one winner) and a new one is created in the same family. Presenting an already-used token revokes the whole family (reuse detection).
- **Logout** revokes the refresh token's family (that login). **Logout-all** bumps the token version and revokes every session.

### Verifying email

A single-use token (hashed, 24 h) is emailed on registration and on resend; resending invalidates the previous link. With `AUTH_REQUIRE_VERIFIED_EMAIL=true` the todos API answers 403 until the email is verified. Completing a password reset also verifies the address, since the user just proved they receive mail there.

### Resetting and changing passwords

- **Forgot**: `POST /auth/password/forgot` always answers 202, whether or not the account exists. For an existing account it stores a single-use reset token (hashed, 1 h; a new request replaces the previous one) and emails a link to `AUTH_PASSWORD_RESET_URL?token=…`. The email is sent after the response, so SMTP latency can't reveal which addresses have accounts.
- **Reset**: the page behind that link asks for a new password and calls `POST /auth/password/reset` with the token. The API has no HTML pages, so point `AUTH_PASSWORD_RESET_URL` at your client.
- **Change**: a logged-in user calls `POST /auth/password/change` with the current and new password and gets a fresh token pair back.
- Both reset and change revoke every session and access token issued before, and cancel any pending reset link.
- Accounts created before password auth have no password. They can't log in or change their password, but the reset flow gives them one.

### Rate limiting

A per-client-IP token bucket guards the public `/auth/*` endpoints and password change (`429` with `Retry-After`). It's in-process, so the limit applies per replica. `X-Forwarded-For` is not trusted.

## Organizing Todos

### Collections, projects and the inbox

```
Collection "Work"                      optional folder for projects
├── Project "Website relaunch"         a list of todos
│   ├── todo: Draft sitemap
│   └── todo: Pick CMS
└── Project "Hiring"
    └── todo: Write job ad

Project "Groceries"                    projects don't need a collection
└── todo: Buy milk

Inbox                                  todos without a project
└── todo: Call the bank
```

- A **project** is a list of todos, like a Google Tasks list. A todo belongs to at most one project; todos without one are in the **inbox** (`projectId: null`, filter with `GET /todos?project=inbox`).
- A **collection** groups projects, like a folder or a Notion teamspace. A project belongs to at most one collection.
- Projects and collections have a `name`, `description`, `icon` (usually an emoji) and `color`.
- **Deleting a project deletes its todos**, atomically. **Deleting a collection keeps its projects**; they are simply no longer filed under it.
- Everything is private to its owner. Referencing another user's project or collection answers `PROJECT_NOT_FOUND` / `COLLECTION_NOT_FOUND`, exactly as if it didn't exist.

### Todo fields

| Field | Values | Notes |
|-------|--------|-------|
| `status` | `pending`, `in_progress`, `blocked`, `done`, `cancelled` | Default `pending`. `completedAt` is set when it becomes `done` and cleared when it leaves `done` |
| `priority` | `none`, `low`, `medium`, `high`, `urgent` | Default `none` |
| `dueAt` | RFC 3339 timestamp, or `YYYY-MM-DD` (midnight UTC) | The deadline; `null` clears it. `overdue` is true while an open todo is past it |
| `notes` | Free text, up to 10,000 characters | |
| `tags` | Up to 20 strings of 1–50 characters | Trimmed and de-duplicated; matched exactly by `?tag=` |
| `completed` | `true` / `false` | Kept for earlier clients: derived from `status` in responses; sending `true` sets `done`, `false` reopens a done todo as `pending` |

Timestamps are stored with millisecond precision.

### Archiving

Set `"archived": true` on a project or collection to hide it without deleting anything; `false` restores it. Archived projects are left out of `GET /projects` and of their collection's project list, and **their todos are left out of `GET /todos`**, unless you ask for them: `?archived=true|all` for projects and collections, `?include_archived=true` or `?project={id}` for todos.

### Progress

Every project and collection response carries a `progress` summary of its todos:

```json
{
  "total": 5,
  "pending": 1,
  "inProgress": 1,
  "blocked": 0,
  "done": 2,
  "cancelled": 1,
  "overdue": 1,
  "percentDone": 50
}
```

`percentDone` is `done / (total - cancelled)`, rounded down, so cancelled todos don't count as remaining work. A collection's progress adds up its active projects.

## Database Schema

Both databases hold the same data; only naming differs (`snake_case` columns in PostgreSQL, `camelCase` fields in MongoDB). Tokens are never stored, only their SHA-256 hashes. Every user owns at most one pending email verification and one pending password reset.

### Enums

```
└── enums
    ├── TodoStatus (todos.status, text)
    │   ├── pending
    │   ├── in_progress
    │   ├── blocked
    │   ├── done
    │   └── cancelled
    │
    ├── Priority (todos.priority, stored as a number so it sorts)
    │   ├── 0 none
    │   ├── 1 low
    │   ├── 2 medium
    │   ├── 3 high
    │   └── 4 urgent
    │
    └── Color (projects.color, collections.color)
        ├── default
        ├── gray
        ├── brown
        ├── orange
        ├── yellow
        ├── green
        ├── blue
        ├── purple
        ├── pink
        └── red
```

### PostgreSQL tables

```
└── database
    │
    ├── users
    │   ├── id: uuid [PK] (default gen_random_uuid())
    │   ├── name: text
    │   ├── email: text [UNIQUE] (lower-cased)
    │   ├── password_hash: text (default '': no password, legacy account)
    │   ├── email_verified_at: timestamptz (nullable)
    │   ├── token_version: integer (default 0)
    │   └── created_at: timestamptz
    │
    ├── collections
    │   ├── id: uuid [PK]
    │   ├── user_id: uuid [FK → users.id, ON DELETE CASCADE]
    │   ├── name: text
    │   ├── description: text (default '')
    │   ├── icon: text (default '')
    │   ├── color: text (enum Color, default 'default')
    │   ├── archived_at: timestamptz (nullable)
    │   ├── created_at: timestamptz
    │   ├── updated_at: timestamptz
    │   │
    │   └── [Indexes]
    │       └── [Index] idx_collections_user_created (user_id, created_at)
    │
    ├── projects
    │   ├── id: uuid [PK]
    │   ├── user_id: uuid [FK → users.id, ON DELETE CASCADE]
    │   ├── collection_id: uuid (nullable) [FK → collections.id, ON DELETE SET NULL]
    │   ├── name: text
    │   ├── description: text (default '')
    │   ├── icon: text (default '')
    │   ├── color: text (enum Color, default 'default')
    │   ├── archived_at: timestamptz (nullable)
    │   ├── created_at: timestamptz
    │   ├── updated_at: timestamptz
    │   │
    │   └── [Indexes]
    │       ├── [Index] idx_projects_user_created (user_id, created_at)
    │       └── [Index] idx_projects_collection (collection_id)
    │
    ├── todos
    │   ├── id: uuid [PK]
    │   ├── user_id: uuid [FK → users.id, ON DELETE CASCADE]
    │   ├── project_id: uuid (nullable: inbox) [FK → projects.id, ON DELETE CASCADE]
    │   ├── title: text
    │   ├── notes: text (default '')
    │   ├── status: text (enum TodoStatus, CHECK, default 'pending')
    │   ├── priority: smallint (enum Priority, CHECK 0–4, default 0)
    │   ├── tags: text[] (default '{}')
    │   ├── due_at: timestamptz (nullable)
    │   ├── completed_at: timestamptz (nullable)
    │   ├── created_at: timestamptz
    │   ├── updated_at: timestamptz
    │   │
    │   └── [Indexes]
    │       ├── [Index] idx_todos_user_created (user_id, created_at DESC)
    │       ├── [Index] idx_todos_project (project_id)
    │       ├── [Index] idx_todos_user_due (user_id, due_at)
    │       └── [GIN Index] idx_todos_tags (tags)
    │
    ├── sessions
    │   ├── id: uuid [PK]
    │   ├── user_id: uuid [FK → users.id, ON DELETE CASCADE]
    │   ├── family_id: uuid (one family per login; rotation stays in it)
    │   ├── token_hash: text [UNIQUE]
    │   ├── expires_at: timestamptz
    │   ├── revoked_at: timestamptz (nullable)
    │   ├── created_at: timestamptz
    │   │
    │   └── [Indexes]
    │       ├── [Index] idx_sessions_user_id (user_id)
    │       └── [Index] idx_sessions_family_id (family_id)
    │
    ├── email_verifications
    │   ├── user_id: uuid [PK] [FK → users.id, ON DELETE CASCADE]
    │   ├── token_hash: text [UNIQUE]
    │   ├── expires_at: timestamptz
    │   └── created_at: timestamptz
    │
    └── password_resets
        ├── user_id: uuid [PK] [FK → users.id, ON DELETE CASCADE]
        ├── token_hash: text [UNIQUE]
        ├── expires_at: timestamptz
        └── created_at: timestamptz
```

### MongoDB collections

```
└── database
    │
    ├── users
    │   ├── _id: objectId [PK]
    │   ├── name: string
    │   ├── email: string (lower-cased)
    │   ├── passwordHash: string ("": no password, legacy account)
    │   ├── emailVerifiedAt: ISODate (absent until verified)
    │   ├── tokenVersion: int
    │   ├── createdAt: ISODate
    │   │
    │   └── [Indexes]
    │       └── [Unique Index] { email: -1 }
    │
    ├── collections
    │   ├── _id: objectId [PK]
    │   ├── userId: objectId [FK → users._id]
    │   ├── name: string
    │   ├── description: string
    │   ├── icon: string
    │   ├── color: string (enum Color)
    │   ├── archivedAt: ISODate (nullable)
    │   ├── createdAt: ISODate
    │   ├── updatedAt: ISODate
    │   │
    │   └── [Indexes]
    │       └── [Index] { userId: 1, createdAt: 1 }
    │
    ├── projects
    │   ├── _id: objectId [PK]
    │   ├── userId: objectId [FK → users._id]
    │   ├── collectionId: objectId (nullable) [FK → collections._id]
    │   ├── name, description, icon, color, archivedAt, createdAt, updatedAt (as collections)
    │   │
    │   └── [Indexes]
    │       ├── [Index] { userId: 1, createdAt: 1 }
    │       └── [Index] { collectionId: 1 }
    │
    ├── todos
    │   ├── _id: objectId [PK]
    │   ├── userId: objectId [FK → users._id]
    │   ├── projectId: objectId (nullable: inbox) [FK → projects._id]
    │   ├── title: string
    │   ├── notes: string
    │   ├── status: string (enum TodoStatus)
    │   ├── priority: int (enum Priority)
    │   ├── tags: [string]
    │   ├── dueAt: ISODate (nullable)
    │   ├── completedAt: ISODate (nullable)
    │   ├── createdAt: ISODate
    │   ├── updatedAt: ISODate
    │   │
    │   └── [Indexes]
    │       ├── [Index] { userId: 1, createdAt: -1 }
    │       ├── [Index] { userId: 1, projectId: 1 }
    │       ├── [Index] { userId: 1, dueAt: 1 }
    │       └── [Index] { userId: 1, tags: 1 }
    │
    ├── sessions
    │   ├── _id: objectId [PK]
    │   ├── userId: objectId [FK → users._id]
    │   ├── familyId: string (UUID; one family per login)
    │   ├── tokenHash: string
    │   ├── expiresAt: ISODate
    │   ├── revokedAt: ISODate (absent until revoked)
    │   ├── createdAt: ISODate
    │   │
    │   └── [Indexes]
    │       ├── [Unique Index] { tokenHash: 1 }
    │       ├── [Index] { userId: 1 }
    │       ├── [Index] { familyId: 1 }
    │       └── [TTL Index] { expiresAt: 1 } (expireAfterSeconds: 0)
    │
    ├── email_verifications
    │   ├── _id: objectId [PK] (= users._id)
    │   ├── tokenHash: string
    │   ├── expiresAt: ISODate
    │   ├── createdAt: ISODate
    │   │
    │   └── [Indexes]
    │       ├── [Unique Index] { tokenHash: 1 }
    │       └── [TTL Index] { expiresAt: 1 } (expireAfterSeconds: 0)
    │
    ├── password_resets
    │   ├── _id: objectId [PK] (= users._id)
    │   ├── tokenHash: string
    │   ├── expiresAt: ISODate
    │   ├── createdAt: ISODate
    │   │
    │   └── [Indexes]
    │       ├── [Unique Index] { tokenHash: 1 }
    │       └── [TTL Index] { expiresAt: 1 } (expireAfterSeconds: 0)
    │
    └── schema_versions (migration bookkeeping)
        ├── version: int
        ├── status: string ("applied")
        └── applied_at: ISODate
```

MongoDB doesn't enforce the `[FK]` references; the application does (deleting a project deletes its todos, deleting a collection detaches its projects, deleting an account removes everything), in one transaction on a replica set.

### Migrations

- **PostgreSQL**: SQL files in `internal/platform/database/postgres/migrations`, embedded in the binaries and applied with goose. Run `make migrate` (or `migrate` in the image) as a deploy step, or set `MIGRATE_ON_START=true`.
- **MongoDB**: Go migrations in `internal/platform/database/mongodb/migrations/versions` plus the index definitions in its `schema/` JSON files. Apply them the same way.

## Errors

### Error format

All API errors use the following structure:

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable explanation"
  }
}
```

`code` is stable and meant for programmatic handling; `message` is for people and may change. Unexpected failures are logged with the request ID and returned as `500` with code `INTERNAL`, so internal details never leak. Unknown routes return `404 NOT_FOUND` and wrong methods `405 METHOD_NOT_ALLOWED` (with an `Allow` header), in the same shape. Every response carries `X-Request-ID`, and access logs are structured and correlated by it.

### Common error codes

| Status | Code | When |
|--------|------|------|
| 400 | `INVALID_BODY` | Malformed JSON, wrong types, trailing data, or a body over 1 MiB |
| 401 | `MISSING_ACCESS_TOKEN` | Bearer endpoint called without `Authorization: Bearer <token>` |
| 401 | `INVALID_ACCESS_TOKEN`, `MALFORMED_ACCESS_TOKEN`, `EXPIRED_ACCESS_TOKEN` | The access token can't be used |
| 401 | `TOKEN_REVOKED` | The token was issued before a logout-all, password reset or change, or account deletion |
| 403 | `EMAIL_NOT_VERIFIED` | Todos endpoints, only when `AUTH_REQUIRE_VERIFIED_EMAIL=true` |
| 429 | `RATE_LIMITED` | Rate-limited endpoint called too often; see `Retry-After` |

Endpoint-specific codes are listed with each endpoint.

## Endpoints

### Conventions

- **Bearer** endpoints need `Authorization: Bearer <access_token>`.
- **Rate-limited** endpoints share a per-client-IP budget (`AUTH_RATE_LIMIT_PER_MINUTE`, `AUTH_RATE_LIMIT_BURST`).
- Timestamps are RFC 3339 in UTC. IDs are 24-character hex strings on memory and MongoDB, and UUIDs on PostgreSQL.
- Responses that return tokens use this `authorization` object:

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "Q2x4dk1yb0pYbE5xV3Z0c2Fmb0l5bU9nR1lrS3dCcU4",
  "token_type": "Bearer",
  "expires_in": 900
}
```

`expires_in` is the access token's lifetime in seconds (15 minutes). The refresh token is valid for 7 days and is stored only as a SHA-256 hash.

### Health

#### GET `/health`

Liveness probe. No authentication.

Response `200 OK` (`text/plain`)

```
ok
```

### Auth

#### POST `/auth/register`

Creates an account and logs it in. Rate-limited. A verification email is sent; without `SMTP_HOST` the link is written to the log.

Request

```json
{
  "name": "Aziz Al-Soufi",
  "email": "aziz@example.com",
  "password": "correct horse battery"
}
```

Response `201 Created`

```json
{
  "user": {
    "id": "672c5f3a9e1b4d0012a7c3e1",
    "name": "Aziz Al-Soufi",
    "email": "aziz@example.com",
    "emailVerified": false,
    "createdAt": "2026-11-07T10:15:22.427234Z"
  },
  "authorization": {
    "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "Q2x4dk1yb0pYbE5xV3Z0c2Fmb0l5bU9nR1lrS3dCcU4",
    "token_type": "Bearer",
    "expires_in": 900
  }
}
```

`name` is required, trimmed, and at most 100 characters. `email` is trimmed and lower-cased. `password` must be 8–72 bytes. Errors: `INVALID_NAME`, `NAME_TOO_LONG`, `INVALID_EMAIL`, `WEAK_PASSWORD`, `PASSWORD_TOO_LONG` (400); `EMAIL_TAKEN` (409).

#### POST `/auth/login`

Logs in with email and password and starts a new session. Rate-limited.

Request

```json
{
  "email": "aziz@example.com",
  "password": "correct horse battery"
}
```

Response `200 OK`

```json
{
  "user": {
    "id": "672c5f3a9e1b4d0012a7c3e1",
    "name": "Aziz Al-Soufi",
    "email": "aziz@example.com",
    "emailVerified": true,
    "createdAt": "2026-11-07T10:15:22.427234Z"
  },
  "authorization": {
    "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "bW9yZVJhbmRvbUJ5dGVzRm9yVGhlUmVmcmVzaFRva2Vu",
    "token_type": "Bearer",
    "expires_in": 900
  }
}
```

Unknown emails and wrong passwords both return `INVALID_CREDENTIALS` (401) and take the same time, so the response doesn't reveal which accounts exist. Accounts created before password auth have no password and also get `INVALID_CREDENTIALS` until they [reset it](#post-authpasswordforgot).

#### POST `/auth/refresh`

Exchanges a refresh token for a new token pair. Rate-limited. The presented token is rotated: it stops working and the response carries its replacement.

Request

```json
{
  "refresh_token": "Q2x4dk1yb0pYbE5xV3Z0c2Fmb0l5bU9nR1lrS3dCcU4"
}
```

Response `200 OK`

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "TmV4dFJvdGF0ZWRSZWZyZXNoVG9rZW5WYWx1ZUhlcmU",
  "token_type": "Bearer",
  "expires_in": 900
}
```

Errors: `INVALID_VALUE` (400) when `refresh_token` is empty; `INVALID_REFRESH_TOKEN` (401) when it is unknown, expired, revoked, or the account is gone. Presenting a token that was already rotated returns `REFRESH_TOKEN_REUSED` (401) and revokes every token of that login, since it suggests the token was stolen. If two refreshes race with the same token, exactly one succeeds.

#### POST `/auth/logout`

Ends one login: revokes the refresh token and every token rotated from it. Rate-limited; no access token needed.

Request

```json
{
  "refresh_token": "TmV4dFJvdGF0ZWRSZWZyZXNoVG9rZW5WYWx1ZUhlcmU"
}
```

Response `204 No Content`

Unknown or already-revoked tokens also return 204, so logout is idempotent. Access tokens already issued stay valid until they expire (at most 15 minutes); use logout-all to cut them off immediately. Errors: `INVALID_VALUE` (400) when `refresh_token` is empty.

#### POST `/auth/logout-all`

Logs the caller out everywhere. Bearer. Bumps the account's token version, so every access token issued so far is rejected immediately (`TOKEN_REVOKED`), and revokes every refresh token.

Request: no body.

Response `204 No Content`

### Email verification

#### POST `/auth/verify-email`

Marks the email address as verified using the token from the verification email. Rate-limited.

Request

```json
{
  "token": "dmVyaWZpY2F0aW9uVG9rZW5Gcm9tVGhlRW1haWxMaW5r"
}
```

Response `200 OK`

```json
{
  "emailVerified": true
}
```

Tokens are single-use, valid for 24 hours, and stored only as SHA-256 hashes. Errors: `INVALID_VERIFICATION_TOKEN` (400) when the token is empty, unknown, expired, already used, or replaced by a resend.

#### GET `/auth/verify-email?token={token}`

The link sent in the verification email (`APP_BASE_URL` + this path). Same behaviour, response and errors as [POST `/auth/verify-email`](#post-authverify-email). Rate-limited.

Response `200 OK`

```json
{
  "emailVerified": true
}
```

#### POST `/auth/verify-email/resend`

Sends a new verification email. Bearer. The previous link stops working.

Request: no body.

Response `202 Accepted`

```json
{
  "sent": true
}
```

Errors: `EMAIL_ALREADY_VERIFIED` (409).

### Password

#### POST `/auth/password/forgot`

Starts a password reset. Rate-limited. If an account with this email exists, a single-use reset link (`AUTH_PASSWORD_RESET_URL?token=…`, valid 1 hour) is emailed to it; a new request replaces the previous link. The response is the same either way, so the endpoint can't be used to find out which emails have accounts.

Request

```json
{
  "email": "aziz@example.com"
}
```

Response `202 Accepted`

```json
{
  "accepted": true
}
```

Malformed or unknown emails also return 202.

#### POST `/auth/password/reset`

Sets a new password with the token from the reset email. Rate-limited. Every session and access token of the account is revoked, and the email address is marked verified.

Request

```json
{
  "token": "cmVzZXRUb2tlbkZyb21UaGVQYXNzd29yZFJlc2V0TGluaw",
  "password": "a brand new password"
}
```

Response `204 No Content`

The new password follows the registration rules. A rejected password doesn't use up the token, so the user can try again. Errors: `WEAK_PASSWORD`, `PASSWORD_TOO_LONG` (400); `INVALID_RESET_TOKEN` (400) when the token is empty, unknown, expired, already used, or replaced by a newer request.

#### POST `/auth/password/change`

Changes the password of the logged-in user. Bearer and rate-limited. Every other session and access token is revoked; the response carries a fresh token pair so the caller stays logged in.

Request

```json
{
  "current_password": "correct horse battery",
  "new_password": "a brand new password"
}
```

Response `200 OK`

```json
{
  "authorization": {
    "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "QWZ0ZXJDaGFuZ2luZ1RoZVBhc3N3b3JkVGhpc0lzTmV3",
    "token_type": "Bearer",
    "expires_in": 900
  }
}
```

Errors: `INCORRECT_PASSWORD` (400) when `current_password` is wrong, including for accounts without a password (they use [the reset flow](#post-authpasswordforgot)); `WEAK_PASSWORD`, `PASSWORD_TOO_LONG` (400).

### Account

#### GET `/me`

Returns the caller's profile and todos, newest first. Bearer.

Response `200 OK`

```json
{
  "id": "672c5f3a9e1b4d0012a7c3e1",
  "name": "Aziz Al-Soufi",
  "email": "aziz@example.com",
  "emailVerified": true,
  "createdAt": "2026-11-07T10:15:22.427234Z",
  "todos": [
    {
      "id": "672c60119e1b4d0012a7c3f4",
      "userId": "672c5f3a9e1b4d0012a7c3e1",
      "title": "Try the modular monolith",
      "status": "pending",
      "completed": false,
      "createdAt": "2026-11-07T10:19:05.118Z"
    }
  ]
}
```

`todos` lists every todo, including those in archived projects; it is `[]` when the user has none. Use [GET `/todos`](#get-todos) for the full fields, filters and sorting.

#### DELETE `/me`

Deletes the account with its todos, projects, collections, sessions and pending email verification and password reset. Bearer. Everything is removed in one transaction (on MongoDB this needs a replica set; on a standalone server the deletes run without one), and all of the user's tokens stop working.

Request: no body.

Response `204 No Content`

### Todos

All todos endpoints are Bearer, and also need a verified email when `AUTH_REQUIRE_VERIFIED_EMAIL=true`. Field rules are under [Todo fields](#todo-fields).

#### POST `/todos`

Creates a todo for the caller, in a project or in the inbox. Only `title` is required.

Request

```json
{
  "title": "Pick a CMS",
  "projectId": "672c61a09e1b4d0012a7c401",
  "notes": "Compare Strapi and Payload. Budget: 2k/yr.",
  "status": "in_progress",
  "priority": "high",
  "tags": ["research", "website"],
  "dueAt": "2026-11-20T17:00:00Z"
}
```

Response `201 Created`

```json
{
  "id": "672c60119e1b4d0012a7c3f4",
  "userId": "672c5f3a9e1b4d0012a7c3e1",
  "projectId": "672c61a09e1b4d0012a7c401",
  "title": "Pick a CMS",
  "notes": "Compare Strapi and Payload. Budget: 2k/yr.",
  "status": "in_progress",
  "priority": "high",
  "tags": ["research", "website"],
  "dueAt": "2026-11-20T17:00:00Z",
  "overdue": false,
  "completed": false,
  "completedAt": null,
  "createdAt": "2026-11-07T10:19:05.118Z",
  "updatedAt": "2026-11-07T10:19:05.118Z"
}
```

Omit `projectId` (or send `null`) for the inbox. Errors: `INVALID_TITLE`, `TITLE_TOO_LONG`, `NOTES_TOO_LONG`, `INVALID_STATUS`, `INVALID_PRIORITY`, `INVALID_TAG`, `TOO_MANY_TAGS`, `INVALID_FIELD` (wrong JSON type or unparsable date) (400); `PROJECT_NOT_FOUND` (404).

#### GET `/todos`

Lists the caller's todos, filtered and sorted. Without parameters: every todo except those in archived projects, newest first.

| Parameter | Example | Meaning |
|-----------|---------|---------|
| `project` | `672c61a0…` or `inbox` | Todos of one project (even an archived one), or of the inbox |
| `status` | `pending,in_progress` | Any of these statuses |
| `priority` | `high,urgent` | Any of these priorities |
| `tag` | `website` | Has this tag (exact match) |
| `due_before` | `2026-11-30` | Deadline strictly before (RFC 3339 or date) |
| `due_after` | `2026-11-01T00:00:00Z` | Deadline at or after |
| `overdue` | `true` | Open todos past their deadline |
| `q` | `cms` | Case-insensitive text search in title and notes |
| `sort` | `due` | `created` (newest first, default), `updated` (recently changed first), `due` (soonest deadline first, none last), `priority` (most important first, then soonest deadline) |
| `include_archived` | `true` | Also list todos of archived projects |

Filters combine with AND. Todos without a deadline never match `due_before`, `due_after` or `overdue`.

Response `200 OK`: an array of todos (same shape as above), `[]` when nothing matches.

```json
[
  {
    "id": "672c60119e1b4d0012a7c3f4",
    "title": "Pick a CMS",
    "status": "in_progress",
    "priority": "high",
    "dueAt": "2026-11-20T17:00:00Z",
    "overdue": false,
    "...": "..."
  }
]
```

Errors: `INVALID_STATUS`, `INVALID_PRIORITY`, `INVALID_SORT`, `INVALID_QUERY` (bad date or boolean) (400); `PROJECT_NOT_FOUND` (404) for an unknown `project`.

#### GET `/todos/{id}`

Returns one of the caller's todos.

Response `200 OK`: a todo, as for [POST `/todos`](#post-todos).

Errors: `NOT_FOUND` (404) when the todo doesn't exist or belongs to someone else; `INVALID_ID` (400) on MongoDB and PostgreSQL when the ID isn't a valid ObjectID or UUID (the memory backend answers `NOT_FOUND`).

#### PATCH `/todos/{id}`

Partially updates one of the caller's todos: send only the fields to change. `PUT /todos/{id}` does the same, for earlier clients.

Request

```json
{
  "status": "done",
  "dueAt": null,
  "projectId": null
}
```

Response `200 OK`: the updated todo.

`null` clears `dueAt`, and moves the todo to the inbox for `projectId`; it is rejected for the other fields. Errors: `INVALID_BODY` (400) for an empty object `{}`; the field errors of [POST `/todos`](#post-todos); `INVALID_VALUE` (400) for `"completed": null`; `INVALID_ID` (400) and `NOT_FOUND` (404) as for [GET `/todos/{id}`](#get-todosid); `PROJECT_NOT_FOUND` (404).

#### DELETE `/todos/{id}`

Deletes one of the caller's todos.

Request: no body.

Response `200 OK`

```json
{}
```

Errors: `INVALID_ID` (400) and `NOT_FOUND` (404) as for [GET `/todos/{id}`](#get-todosid).

### Projects

All projects endpoints are Bearer (+ verified email when required).

#### POST `/projects`

Creates a project (a list of todos), optionally inside a collection. Only `name` is required.

Request

```json
{
  "name": "Website relaunch",
  "description": "New site live before Q1.",
  "icon": "🚀",
  "color": "blue",
  "collectionId": "672c61709e1b4d0012a7c3fd"
}
```

Response `201 Created`

```json
{
  "id": "672c61a09e1b4d0012a7c401",
  "collectionId": "672c61709e1b4d0012a7c3fd",
  "name": "Website relaunch",
  "description": "New site live before Q1.",
  "icon": "🚀",
  "color": "blue",
  "archived": false,
  "archivedAt": null,
  "createdAt": "2026-11-07T10:21:44.031Z",
  "updatedAt": "2026-11-07T10:21:44.031Z",
  "progress": {
    "total": 0,
    "pending": 0,
    "inProgress": 0,
    "blocked": 0,
    "done": 0,
    "cancelled": 0,
    "overdue": 0,
    "percentDone": 0
  }
}
```

`name` is trimmed, 1–100 characters; `description` up to 2,000 characters; `icon` up to 32 bytes; `color` from the [Color enum](#enums) (default `default`). Errors: `INVALID_NAME`, `NAME_TOO_LONG`, `DESCRIPTION_TOO_LONG`, `ICON_TOO_LONG`, `INVALID_COLOR`, `INVALID_FIELD` (400); `COLLECTION_NOT_FOUND` (404).

#### GET `/projects`

Lists the caller's projects with their progress, oldest first.

| Parameter | Example | Meaning |
|-----------|---------|---------|
| `collection` | `672c6170…` | Only projects in this collection |
| `archived` | `true` | `false` (default): active only; `true`: archived only; `all`: both |

Response `200 OK`: an array of projects, as for [POST `/projects`](#post-projects).

Errors: `INVALID_QUERY` (400); `COLLECTION_NOT_FOUND` (404) for an unknown `collection`.

#### GET `/projects/{id}`

Returns one project with its progress, archived or not.

Response `200 OK`: a project, as for [POST `/projects`](#post-projects).

Errors: `INVALID_ID` (400) on MongoDB and PostgreSQL for a malformed ID; `PROJECT_NOT_FOUND` (404).

#### PATCH `/projects/{id}`

Renames, restyles, archives or moves a project: send only the fields to change.

Request

```json
{
  "archived": true,
  "collectionId": null
}
```

Response `200 OK`: the updated project.

`"collectionId": null` takes the project out of its collection; `"archived": true|false` archives or restores it. Errors: `INVALID_BODY` (400) for an empty object `{}`; the field errors of [POST `/projects`](#post-projects); `INVALID_ID` (400); `PROJECT_NOT_FOUND`, `COLLECTION_NOT_FOUND` (404).

#### DELETE `/projects/{id}`

Deletes the project **and all of its todos**, in one transaction. To keep the todos, move them first (`PATCH /todos/{id}` with another `projectId`, or `null` for the inbox) or archive the project instead.

Request: no body.

Response `204 No Content`

Errors: `INVALID_ID` (400); `PROJECT_NOT_FOUND` (404).

### Collections

All collections endpoints are Bearer (+ verified email when required).

#### POST `/collections`

Creates a collection to group projects. Only `name` is required; the fields follow the rules of [POST `/projects`](#post-projects).

Request

```json
{
  "name": "Work",
  "icon": "💼",
  "color": "purple"
}
```

Response `201 Created`

```json
{
  "id": "672c61709e1b4d0012a7c3fd",
  "name": "Work",
  "description": "",
  "icon": "💼",
  "color": "purple",
  "archived": false,
  "archivedAt": null,
  "createdAt": "2026-11-07T10:20:56.512Z",
  "updatedAt": "2026-11-07T10:20:56.512Z",
  "progress": {
    "total": 0,
    "pending": 0,
    "inProgress": 0,
    "blocked": 0,
    "done": 0,
    "cancelled": 0,
    "overdue": 0,
    "percentDone": 0
  },
  "projects": []
}
```

Errors: `INVALID_NAME`, `NAME_TOO_LONG`, `DESCRIPTION_TOO_LONG`, `ICON_TOO_LONG`, `INVALID_COLOR`, `INVALID_FIELD` (400).

#### GET `/collections`

Lists the caller's collections, oldest first, each with its active projects and their combined progress (a ready-made sidebar).

| Parameter | Example | Meaning |
|-----------|---------|---------|
| `archived` | `all` | `false` (default): active only; `true`: archived only; `all`: both |

Response `200 OK`

```json
[
  {
    "id": "672c61709e1b4d0012a7c3fd",
    "name": "Work",
    "icon": "💼",
    "color": "purple",
    "archived": false,
    "progress": { "total": 5, "done": 2, "percentDone": 50, "...": "..." },
    "projects": [
      { "id": "672c61a09e1b4d0012a7c401", "name": "Website relaunch", "progress": { "...": "..." }, "...": "..." }
    ],
    "...": "..."
  }
]
```

Errors: `INVALID_QUERY` (400).

#### GET `/collections/{id}`

Returns one collection with its active projects and their combined progress.

Response `200 OK`: a collection, as for [GET `/collections`](#get-collections).

Errors: `INVALID_ID` (400); `COLLECTION_NOT_FOUND` (404).

#### PATCH `/collections/{id}`

Renames, restyles or archives a collection: send only the fields to change (`name`, `description`, `icon`, `color`, `archived`).

Request

```json
{
  "name": "Work 2027",
  "archived": false
}
```

Response `200 OK`: the updated collection.

Archiving a collection doesn't archive its projects. Errors: `INVALID_BODY` (400) for an empty object `{}`; the field errors of [POST `/collections`](#post-collections); `INVALID_ID` (400); `COLLECTION_NOT_FOUND` (404).

#### DELETE `/collections/{id}`

Deletes the collection only. Its projects and their todos are kept and are no longer filed under any collection.

Request: no body.

Response `204 No Content`

Errors: `INVALID_ID` (400); `COLLECTION_NOT_FOUND` (404).

## Testing

### Unit tests

```bash
make test        # go test -race ./...
```

Covers domains, commands and queries, adapters, middleware, migrations and codegen, plus an end-to-end suite (`cmd/api/app_test.go`) that drives every endpoint through the production router. Without database settings it runs on the memory backend and skips the database integration tests.

### Integration tests

```bash
make test-integration   # needs Docker
make test-db-down       # stop the test databases
```

This starts PostgreSQL and a single-node MongoDB replica set from `docker-compose.test.yml`, then:

1. Runs every test with `TEST_POSTGRES_URI` and `TEST_MONGO_URI` set. Each persistence adapter runs its module's **store contract** (`adapters/persistence/storetest`): the same suite for memory, MongoDB and PostgreSQL, covering CRUD, ownership, unique emails, single-use tokens, concurrent refresh-token rotation, todo filters, sorting and progress, and transaction rollback and nesting. Migrations are also tested on existing data, upgrade and rollback.
2. Runs the end-to-end suite again on each database (`TEST_E2E_BACKEND=postgres|mongo`).

Every test gets its own freshly migrated database (`internal/platform/database/dbtest`), dropped when it ends. CI runs the same target on every pull request and before each image is published.

## TO-DO

- Caching and distributed tracing were deliberately left out: there is no measured need yet. Request IDs and structured access logs cover correlation for now.
- A shared rate-limit store (e.g. Redis) once the API runs as several replicas; the current limiter counts per process.

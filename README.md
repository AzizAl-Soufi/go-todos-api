# Todos Stateless API

Go HTTP API for organizing todos, like Google Tasks with a touch of Notion: todos live in **projects** (lists), projects can be grouped into **collections**, and every todo has a status, priority, deadline, notes and tags. **Teams** share projects with roles (owner, admin, member, viewer) and email invitations, and an **audit log** records team activity and account security. Built as a **feature-based modular monolith** with **Clean Architecture / Hexagonal (ports & adapters)** inside each module. Pluggable storage: memory, MongoDB, PostgreSQL. Password auth with reset and change, RS256 access tokens, rotating refresh tokens, email verification. No ORM.

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
- [Teams and Roles](#teams-and-roles)
  - [Teams and team content](#teams-and-team-content)
  - [Roles](#roles)
  - [Invitation flow](#invitation-flow)
  - [Audit logs](#audit-logs)
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
  - [Passkeys](#passkeys-1)
    - [POST `/me/passkeys/options`](#post-mepasskeysoptions)
    - [POST `/me/passkeys`](#post-mepasskeys)
    - [GET `/me/passkeys`](#get-mepasskeys)
    - [DELETE `/me/passkeys/{id}`](#delete-mepasskeysid)
    - [POST `/auth/passkeys/login/options`](#post-authpasskeysloginoptions)
    - [POST `/auth/passkeys/login`](#post-authpasskeyslogin)
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
  - [Teams](#teams)
    - [POST `/teams`](#post-teams)
    - [GET `/teams`](#get-teams)
    - [GET `/teams/{id}`](#get-teamsid)
    - [PATCH `/teams/{id}`](#patch-teamsid)
    - [DELETE `/teams/{id}`](#delete-teamsid)
    - [PATCH `/teams/{id}/members/{userId}`](#patch-teamsidmembersuserid)
    - [DELETE `/teams/{id}/members/{userId}`](#delete-teamsidmembersuserid)
    - [POST `/teams/{id}/transfer-ownership`](#post-teamsidtransfer-ownership)
  - [Invitations](#invitations)
    - [POST `/teams/{id}/invitations`](#post-teamsidinvitations)
    - [GET `/teams/{id}/invitations`](#get-teamsidinvitations)
    - [DELETE `/teams/{id}/invitations/{invitationId}`](#delete-teamsidinvitationsinvitationid)
    - [POST `/invitations/accept`](#post-invitationsaccept)
    - [POST `/invitations/decline`](#post-invitationsdecline)
  - [Audit](#audit)
    - [GET `/teams/{id}/audit-log`](#get-teamsidaudit-log)
    - [GET `/me/audit-log`](#get-meaudit-log)
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
| `PASSKEY_RP_ID` | host of `APP_BASE_URL` | Domain passkeys are bound to: the site's host or a parent domain of it (see [Passkeys](#passkeys)) |
| `PASSKEY_RP_NAME` | `Todos` | Name the browser shows when creating a passkey |
| `PASSKEY_ORIGINS` | origin of `APP_BASE_URL` | Comma-separated origins (`https://app.example.com`) of the pages allowed to use passkeys |
| `SMTP_HOST` | – | SMTP server; empty means emails are logged |
| `SMTP_PORT` | `587` | SMTP port (STARTTLS when offered) |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | – | SMTP credentials |
| `SMTP_FROM` | – | Sender address |

## Architecture

A **feature-based modular monolith**: `internal/modules/identity` (accounts, passwords, sessions, email verification, authentication), `internal/modules/todos` (todos, projects, collections), `internal/modules/teams` (teams, roles, invitations) and `internal/modules/audit` (team and security logs), on top of a shared kernel in `internal/platform`. Inside each module, **Clean Architecture / Hexagonal** layering with **CQRS** in the application layer: `domain` → `application` (ports, plus `command/` for writes and `query/` for reads, one handler per use case) → `transport/httpapi` (driving: run a command, read the result with a query) and `adapters/` (driven: persistence, security, mail, other modules). Modules talk only through each other's public API, and only `cmd/api` knows the concrete adapters.

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

### Passkeys

Users can sign in with a **passkey** (WebAuthn) instead of their email and password. A passkey is a key pair kept by the device or password manager (Touch ID, Windows Hello, a phone, a security key); the API stores only the public key and checks a signature, so there is nothing to phish or leak.

- **Adding one** (signed in): `POST /me/passkeys/options` returns the options for `navigator.credentials.create()` and a single-use `token`; the client sends the browser's answer back to `POST /me/passkeys` with that token.
- **Signing in** (no email needed): `POST /auth/passkeys/login/options` returns the options for `navigator.credentials.get()`; the browser lets the user pick a passkey, and `POST /auth/passkeys/login` returns the same user and tokens as a password login.
- Passkeys are discoverable and require user verification (biometrics or device PIN), so one passkey stands in for both factors. Existing passkeys are excluded when adding another, so a device can't register twice.
- Each challenge is single-use and valid 5 minutes, and is tied to its purpose (a sign-in challenge can't add a passkey, and one account's can't add a passkey to another). Only a hash of the challenge token is stored.
- A sign counter that goes backwards (a possibly cloned security key) fails the sign-in. Failed passkey sign-ins on an existing account go to its security log, as do added and removed passkeys.
- Passkeys are bound to `PASSKEY_RP_ID` and only work on pages served from `PASSKEY_ORIGINS`. Both default to `APP_BASE_URL`; when the web client runs elsewhere (say `https://app.example.com` with the API on `https://api.example.com`), set `PASSKEY_RP_ID=example.com` and `PASSKEY_ORIGINS=https://app.example.com`. Changing the RP ID later invalidates every registered passkey.
- An account has at most 20 passkeys. Passkeys are an addition to the password: registering still needs one, and a password reset or change doesn't remove passkeys (review them with `GET /me/passkeys`).

Client sketch:

```js
// Add a passkey (signed in)
const { token, options } = await api.post("/me/passkeys/options");
const cred = await navigator.credentials.create({
  publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey),
});
await api.post("/me/passkeys", { token, name: "MacBook", credential: cred.toJSON() });

// Sign in
const login = await api.post("/auth/passkeys/login/options");
const assertion = await navigator.credentials.get({
  publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(login.options.publicKey),
});
const { user, authorization } = await api.post("/auth/passkeys/login", { token: login.token, credential: assertion.toJSON() });
```

### Rate limiting

A per-client-IP token bucket guards the public `/auth/*` endpoints (including passkey sign-in) and password change (`429` with `Retry-After`). It's in-process, so the limit applies per replica. `X-Forwarded-For` is not trusted.

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
- Everything is private to its owner, or shared with a team (see [Teams and Roles](#teams-and-roles)). Referencing a project or collection you can't reach answers `PROJECT_NOT_FOUND` / `COLLECTION_NOT_FOUND`, exactly as if it didn't exist.

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

## Teams and Roles

### Teams and team content

A **team** shares projects, collections and their todos among its members, like a Notion teamspace. Everyone keeps a private space next to their teams:

```
Ann's view
├── Personal                           only Ann sees these
│   ├── Inbox
│   └── Project "Groceries"
└── Team "Platform"                    every member sees these
    └── Collection "Roadmap"
        └── Project "Launch"
            └── todo: Ship it          belongs to the team
```

- Create a team project or collection by sending `"teamId"` on `POST /projects` or `POST /collections`. Without it, they are personal. The team is fixed at creation (`TEAM_IMMUTABLE` on `PATCH`).
- A todo belongs to the team of its project. The inbox is always personal: moving a team todo to the inbox (`"projectId": null`) or to a personal project makes it the mover's personal todo, and moving a todo into a team project hands it to that team.
- A project and its collection always belong to the same team, or are both personal (`TEAM_MISMATCH`).
- `GET /todos`, `/projects` and `/collections` list personal **and** team records; `?team={teamId}` keeps one team's, `?team=personal` only personal ones. Responses carry `teamId` (`null` when personal) and `userId` (the creator).
- Records of teams you're not in look exactly like missing ones (`NOT_FOUND`, `PROJECT_NOT_FOUND`, `TEAM_NOT_FOUND`, …). Memberships are read on every request, so a removed member loses access at once.
- Deleting a team deletes its projects, collections, todos, invitations and audit log. Deleting your account keeps the team records you created, with the team.

### Roles

| Role | Can |
|------|-----|
| `owner` | Everything an admin can, plus delete the team and transfer ownership. Exactly one per team |
| `admin` | Invite and remove people, change roles, rename the team, delete team projects and collections, read the team audit log |
| `member` | Create and edit team projects, collections and todos, delete todos |
| `viewer` | Read only |

Owners and admins only act on people **strictly below** them, and only hand out roles strictly below their own: an admin manages members and viewers; the owner also manages admins. Ownership is never assigned, only transferred (`POST /teams/{id}/transfer-ownership`); the previous owner becomes an admin. The owner can't leave without transferring ownership or deleting the team, and can't delete their account while they own a team with other members (`OWNS_TEAMS`); teams they own alone are deleted with the account.

### Invitation flow

1. An owner or admin invites an email address with a role: `POST /teams/{id}/invitations`. The email contains a link `APP_BASE_URL/invitations/accept?token=…` (logged instead of sent when SMTP isn't configured), valid for 7 days.
2. The invitee signs in (or registers) **with that email address** and calls `POST /invitations/accept` with the token, or `POST /invitations/decline`.
3. Each token works once. Inviting the same address again replaces the pending invitation (the old link stops working); owners and admins can list (`GET /teams/{id}/invitations`) and revoke them.

The client page behind the link should sign the user in and post the token. Members are shown with the name and email they joined with.

### Audit logs

Every change in a team is written to the **team audit log**, in the same transaction as the change: team, membership, role, invitation and ownership changes, and creating, updating and deleting team projects, collections and todos. Each user also has a **security log** about their account: registration, logins (and failed login attempts), logouts, refresh-token reuse, email verification, password reset requests, resets and changes.

| Area | Actions |
|------|---------|
| Team | `team.created`, `team.renamed`, `team.ownership_transferred` |
| Members | `member.joined`, `member.role_changed`, `member.removed`, `member.left` |
| Invitations | `invitation.created`, `invitation.revoked`, `invitation.declined` |
| Content | `project.*`, `collection.*`, `todo.*` with `created`, `updated`, `deleted` |
| Security | `user.registered`, `session.login`, `session.login_failed`, `session.logout`, `session.logout_all`, `session.refresh_reuse_detected`, `email.verified`, `password.reset_requested`, `password.reset`, `password.changed` |

An event records the actor, action, target, small `details` (a title, the changed fields, old and new role), the client IP and User-Agent, and the time. Personal records are not audited. Owners and admins read the team log with `GET /teams/{id}/audit-log`; everyone reads their own security log with `GET /me/audit-log`. A deleted account's security log is deleted with it.

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
    ├── Role (team_members.role, team_invitations.role)
    │   ├── owner   (never in an invitation)
    │   ├── admin
    │   ├── member
    │   └── viewer
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
    │   ├── user_id: uuid (creator; owner when personal)
    │   ├── team_id: uuid (nullable: personal)
    │   ├── name: text
    │   ├── description: text (default '')
    │   ├── icon: text (default '')
    │   ├── color: text (enum Color, default 'default')
    │   ├── archived_at: timestamptz (nullable)
    │   ├── created_at: timestamptz
    │   ├── updated_at: timestamptz
    │   │
    │   └── [Indexes]
    │       ├── [Index] idx_collections_user_created (user_id, created_at)
    │       └── [Partial Index] idx_collections_team_created (team_id, created_at) WHERE team_id IS NOT NULL
    │
    ├── projects
    │   ├── id: uuid [PK]
    │   ├── user_id: uuid (creator; owner when personal)
    │   ├── team_id: uuid (nullable: personal; fixed at creation)
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
    │       ├── [Partial Index] idx_projects_team_created (team_id, created_at) WHERE team_id IS NOT NULL
    │       └── [Index] idx_projects_collection (collection_id)
    │
    ├── todos
    │   ├── id: uuid [PK]
    │   ├── user_id: uuid (creator; owner when personal)
    │   ├── team_id: uuid (nullable: personal; the project's team)
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
    │       ├── [Partial Index] idx_todos_team (team_id) WHERE team_id IS NOT NULL
    │       └── [GIN Index] idx_todos_tags (tags)
    │
    ├── teams
    │   ├── id: uuid [PK]
    │   ├── name: text
    │   ├── created_at: timestamptz
    │   └── updated_at: timestamptz
    │
    ├── team_members
    │   ├── team_id: uuid [PK] [FK → teams.id, ON DELETE CASCADE]
    │   ├── user_id: uuid [PK]
    │   ├── name: text (as when they joined)
    │   ├── email: text (as when they joined)
    │   ├── role: text (enum Role, CHECK)
    │   ├── joined_at: timestamptz
    │   │
    │   └── [Indexes]
    │       └── [Index] idx_team_members_user (user_id)
    │
    ├── team_invitations
    │   ├── id: uuid [PK]
    │   ├── team_id: uuid [FK → teams.id, ON DELETE CASCADE]
    │   ├── email: text
    │   ├── role: text (enum Role without owner, CHECK)
    │   ├── invited_by: uuid
    │   ├── token_hash: text [UNIQUE]
    │   ├── expires_at: timestamptz
    │   ├── created_at: timestamptz
    │   │
    │   └── [Constraints]
    │       └── [Unique] (team_id, email)
    │
    ├── audit_events
    │   ├── id: uuid [PK]
    │   ├── team_id: uuid (nullable: a security event)
    │   ├── user_id: uuid (nullable: a team event; CHECK exactly one is set)
    │   ├── actor_id: uuid (nullable: unknown)
    │   ├── action: text
    │   ├── target_type: text (default '')
    │   ├── target_id: text (default '')
    │   ├── details: jsonb (string values, default '{}')
    │   ├── ip: text (default '')
    │   ├── user_agent: text (default '')
    │   ├── created_at: timestamptz
    │   │
    │   └── [Indexes]
    │       ├── [Partial Index] idx_audit_events_team (team_id, created_at DESC, id DESC)
    │       └── [Partial Index] idx_audit_events_user (user_id, created_at DESC, id DESC)
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
    ├── password_resets
    │   ├── user_id: uuid [PK] [FK → users.id, ON DELETE CASCADE]
    │   ├── token_hash: text [UNIQUE]
    │   ├── expires_at: timestamptz
    │   └── created_at: timestamptz
    │
    ├── passkeys
    │   ├── id: uuid [PK]
    │   ├── user_id: uuid [FK → users.id, ON DELETE CASCADE]
    │   ├── name: text
    │   ├── credential_id: bytea [UNIQUE] (the authenticator's credential ID)
    │   ├── credential: bytea (WebAuthn credential record: public key, sign counter, flags)
    │   ├── created_at: timestamptz
    │   ├── last_used_at: timestamptz (nullable)
    │   │
    │   └── [Indexes]
    │       └── [Index] idx_passkeys_user_created (user_id, created_at, id)
    │
    └── passkey_challenges
        ├── token_hash: text [PK]
        ├── user_id: uuid (nullable: sign-in) [FK → users.id, ON DELETE CASCADE]
        ├── state: bytea (WebAuthn ceremony state)
        ├── expires_at: timestamptz
        ├── created_at: timestamptz
        │
        └── [Indexes]
            ├── [Index] idx_passkey_challenges_expires_at (expires_at)
            └── [Index] idx_passkey_challenges_user_id (user_id)
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
    │   ├── userId: objectId (creator; owner when personal)
    │   ├── teamId: objectId (null or absent: personal)
    │   ├── name: string
    │   ├── description: string
    │   ├── icon: string
    │   ├── color: string (enum Color)
    │   ├── archivedAt: ISODate (nullable)
    │   ├── createdAt: ISODate
    │   ├── updatedAt: ISODate
    │   │
    │   └── [Indexes]
    │       ├── [Index] { userId: 1, createdAt: 1 }
    │       └── [Index] { teamId: 1, createdAt: 1 }
    │
    ├── projects
    │   ├── _id: objectId [PK]
    │   ├── userId: objectId (creator; owner when personal)
    │   ├── teamId: objectId (null or absent: personal)
    │   ├── collectionId: objectId (nullable) [FK → collections._id]
    │   ├── name, description, icon, color, archivedAt, createdAt, updatedAt (as collections)
    │   │
    │   └── [Indexes]
    │       ├── [Index] { userId: 1, createdAt: 1 }
    │       ├── [Index] { teamId: 1, createdAt: 1 }
    │       └── [Index] { collectionId: 1 }
    │
    ├── todos
    │   ├── _id: objectId [PK]
    │   ├── userId: objectId (creator; owner when personal)
    │   ├── teamId: objectId (null or absent: personal)
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
    │       ├── [Index] { userId: 1, tags: 1 }
    │       └── [Index] { teamId: 1, projectId: 1 }
    │
    ├── teams
    │   ├── _id: objectId [PK]
    │   ├── name: string
    │   ├── createdAt: ISODate
    │   └── updatedAt: ISODate
    │
    ├── team_members
    │   ├── _id: objectId [PK]
    │   ├── teamId: objectId [FK → teams._id]
    │   ├── userId: objectId
    │   ├── name, email: string (as when they joined)
    │   ├── role: string (enum Role)
    │   ├── joinedAt: ISODate
    │   │
    │   └── [Indexes]
    │       ├── [Unique Index] { teamId: 1, userId: 1 }
    │       └── [Index] { userId: 1 }
    │
    ├── team_invitations
    │   ├── _id: objectId [PK]
    │   ├── teamId: objectId [FK → teams._id]
    │   ├── email: string
    │   ├── role: string (enum Role without owner)
    │   ├── invitedBy: objectId
    │   ├── tokenHash: string
    │   ├── expiresAt: ISODate
    │   ├── createdAt: ISODate
    │   │
    │   └── [Indexes]
    │       ├── [Unique Index] { tokenHash: 1 }
    │       └── [Unique Index] { teamId: 1, email: 1 }
    │
    ├── audit_events
    │   ├── _id: objectId [PK]
    │   ├── teamId: objectId (null: a security event)
    │   ├── userId: objectId (null: a team event)
    │   ├── actorId: objectId (null: unknown)
    │   ├── action, targetType, targetId: string
    │   ├── details: { string: string }
    │   ├── ip, userAgent: string
    │   ├── createdAt: ISODate
    │   │
    │   └── [Indexes]
    │       ├── [Index] { teamId: 1, createdAt: -1, _id: -1 }
    │       └── [Index] { userId: 1, createdAt: -1, _id: -1 }
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
    ├── passkeys
    │   ├── _id: objectId [PK]
    │   ├── userId: objectId [FK → users._id]
    │   ├── name: string
    │   ├── credentialId: binData
    │   ├── credential: binData (WebAuthn credential record)
    │   ├── createdAt: ISODate
    │   ├── lastUsedAt: ISODate (absent until used)
    │   │
    │   └── [Indexes]
    │       ├── [Unique Index] { credentialId: 1 }
    │       └── [Index] { userId: 1, createdAt: 1, _id: 1 }
    │
    ├── passkey_challenges
    │   ├── _id: string [PK] (token hash)
    │   ├── userId: objectId (absent for sign-in) [FK → users._id]
    │   ├── state: binData (WebAuthn ceremony state)
    │   ├── expiresAt: ISODate
    │   ├── createdAt: ISODate
    │   │
    │   └── [Indexes]
    │       ├── [Index] { userId: 1 }
    │       └── [TTL Index] { expiresAt: 1 } (expireAfterSeconds: 0)
    │
    └── schema_versions (migration bookkeeping)
        ├── version: int
        ├── status: string ("applied")
        └── applied_at: ISODate
```

MongoDB doesn't enforce the `[FK]` references; the application does (deleting a project deletes its todos, deleting a collection detaches its projects, deleting a team or an account removes what belongs to it), in one transaction on a replica set. In both databases, IDs that point into another module (users, teams) are plain values without foreign keys: team records outlive their creator's account, and each module cleans up its own data.

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
| 403 | `EMAIL_NOT_VERIFIED` | Todos, teams and audit endpoints, only when `AUTH_REQUIRE_VERIFIED_EMAIL=true` |
| 403 | `INSUFFICIENT_ROLE` | Your role in the team doesn't allow this (see [Roles](#roles)) |
| 404 | `TEAM_NOT_FOUND` | The team doesn't exist or you're not a member |
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

### Passkeys

See [Passkeys](#passkeys) for the flow. `options` is what the browser's `navigator.credentials` call takes (`{"publicKey": {...}}`, binary values base64url-encoded); `credential` is the `PublicKeyCredential` the browser returns, serialized with `toJSON()`.

#### POST `/me/passkeys/options`

Starts adding a passkey to the caller's account. Bearer.

Request: no body.

Response `200 OK`

```json
{
  "token": "b3xQ8mJb2c3lJbH0Zk1yX2RvbmVfZm9yX3Rlc3Rz",
  "options": {
    "publicKey": {
      "rp": { "name": "Todos", "id": "example.com" },
      "user": { "name": "aziz@example.com", "displayName": "Aziz Al-Soufi", "id": "NjcyYzVmM2E5ZTFiNGQwMDEyYTdjM2Ux" },
      "challenge": "kq2l3n6Pq0qLRY7mJ5D8eW8xP2v7cTfVb6dB4mG0QzY",
      "pubKeyCredParams": [{ "type": "public-key", "alg": -7 }, { "type": "public-key", "alg": -257 }, "…"],
      "timeout": 300000,
      "authenticatorSelection": { "requireResidentKey": true, "residentKey": "required", "userVerification": "required" },
      "attestation": "none",
      "extensions": { "credProps": true }
    }
  }
}
```

`excludeCredentials` lists the account's existing passkeys, when it has any. The token is valid for 5 minutes and one attempt.

Errors: `TOO_MANY_PASSKEYS` (409) when the account already has 20.

#### POST `/me/passkeys`

Verifies the browser's answer and stores the passkey. Bearer.

Request

```json
{
  "token": "b3xQ8mJb2c3lJbH0Zk1yX2RvbmVfZm9yX3Rlc3Rz",
  "name": "MacBook",
  "credential": { "id": "…", "rawId": "…", "type": "public-key", "response": { "clientDataJSON": "…", "attestationObject": "…" }, "clientExtensionResults": {} }
}
```

`name` is optional (default `Passkey`, at most 64 characters).

Response `201 Created`: the passkey, as in [GET `/me/passkeys`](#get-mepasskeys).

Errors: `INVALID_VALUE` (400) without `credential`; `INVALID_PASSKEY_CHALLENGE` (400) for an unknown, used or expired token, or one issued to another account or for sign-in; `PASSKEY_REJECTED` (400) when the answer doesn't verify (wrong origin, wrong challenge, no user verification); `PASSKEY_NAME_TOO_LONG` (400); `PASSKEY_EXISTS` (409); `TOO_MANY_PASSKEYS` (409).

#### GET `/me/passkeys`

Lists the caller's passkeys, oldest first. Bearer.

Response `200 OK`

```json
[
  {
    "id": "672c7a41c3d84b0012f0a1b2",
    "name": "MacBook",
    "createdAt": "2026-11-07T10:30:00Z",
    "lastUsedAt": "2026-11-07T18:02:11Z"
  }
]
```

`lastUsedAt` is `null` until the passkey is used to sign in.

#### DELETE `/me/passkeys/{id}`

Removes one of the caller's passkeys. Sessions it started stay valid; use [logout-all](#post-authlogout-all) to end them. Bearer.

Response `204 No Content`

Errors: `PASSKEY_NOT_FOUND` (404), also for another user's passkey.

#### POST `/auth/passkeys/login/options`

Starts a passkey sign-in. Rate-limited.

Request: no body.

Response `200 OK`

```json
{
  "token": "Zq1vV0x4bUJ0c2ZkN2t3eG9fc2lnbl9pbl90b2tlbg",
  "options": {
    "publicKey": {
      "challenge": "1H3pMZ0nC8d6rQyB9vXkT2sLwEa5fJuGoN4hR7iKcYU",
      "timeout": 300000,
      "rpId": "example.com",
      "userVerification": "required"
    }
  }
}
```

There is no `allowCredentials`: the browser offers every passkey it holds for the site, and the chosen one names the account.

#### POST `/auth/passkeys/login`

Verifies the passkey's signature and signs the user in. Rate-limited.

Request

```json
{
  "token": "Zq1vV0x4bUJ0c2ZkN2t3eG9fc2lnbl9pbl90b2tlbg",
  "credential": { "id": "…", "rawId": "…", "type": "public-key", "response": { "clientDataJSON": "…", "authenticatorData": "…", "signature": "…", "userHandle": "…" }, "clientExtensionResults": {} }
}
```

Response `200 OK`: the same `user` and `authorization` as [POST `/auth/login`](#post-authlogin).

Errors: `INVALID_VALUE` (400) without `credential`; `INVALID_PASSKEY_CHALLENGE` (400) for an unknown, used or expired token; `INVALID_PASSKEY` (401) when the passkey is unknown or removed, the signature or origin doesn't verify, or the sign counter went backwards.

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

Deletes the account with its personal todos, projects and collections, its sessions, passkeys, security log and pending email verification and password reset, and takes it out of every team. Team records the user created stay with their team, and teams the user owns alone are deleted. Bearer. Everything is removed in one transaction (on MongoDB this needs a replica set; on a standalone server the deletes run without one), and all of the user's tokens stop working.

Request: no body.

Response `204 No Content`

Errors: `OWNS_TEAMS` (409) while the user owns a team that has other members: transfer ownership or delete the team first.

### Todos

All todos endpoints are Bearer, and also need a verified email when `AUTH_REQUIRE_VERIFIED_EMAIL=true`. Field rules are under [Todo fields](#todo-fields).

#### POST `/todos`

Creates a todo for the caller, in a project or in the inbox. Only `title` is required. A todo in a team project belongs to that team (needs the `member` role or above).

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
  "teamId": null,
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

Omit `projectId` (or send `null`) for the inbox. Errors: `INVALID_TITLE`, `TITLE_TOO_LONG`, `NOTES_TOO_LONG`, `INVALID_STATUS`, `INVALID_PRIORITY`, `INVALID_TAG`, `TOO_MANY_TAGS`, `INVALID_FIELD` (wrong JSON type or unparsable date) (400); `INSUFFICIENT_ROLE` (403) for a viewer of the project's team; `PROJECT_NOT_FOUND` (404).

#### GET `/todos`

Lists the caller's personal todos and those of their teams, filtered and sorted. Without parameters: every todo except those in archived projects, newest first.

| Parameter | Example | Meaning |
|-----------|---------|---------|
| `team` | `672c7011…` or `personal` | Only one team's todos, or only personal ones |
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

Errors: `INVALID_STATUS`, `INVALID_PRIORITY`, `INVALID_SORT`, `INVALID_QUERY` (bad date or boolean) (400); `PROJECT_NOT_FOUND` (404) for an unknown `project`; `TEAM_NOT_FOUND` (404) for a `team` you're not in.

#### GET `/todos/{id}`

Returns one of the caller's todos.

Response `200 OK`: a todo, as for [POST `/todos`](#post-todos).

Errors: `NOT_FOUND` (404) when the todo doesn't exist or you can't reach it (someone else's, or a team you're not in); `INVALID_ID` (400) on MongoDB and PostgreSQL when the ID isn't a valid ObjectID or UUID (the memory backend answers `NOT_FOUND`).

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

`null` clears `dueAt`, and moves the todo to the inbox for `projectId`; it is rejected for the other fields. Moving a todo changes its team with its project (see [Teams and team content](#teams-and-team-content)); team todos need the `member` role or above, on both sides of a move (`INSUFFICIENT_ROLE`, 403). Errors: `INVALID_BODY` (400) for an empty object `{}`; the field errors of [POST `/todos`](#post-todos); `INVALID_VALUE` (400) for `"completed": null`; `INVALID_ID` (400) and `NOT_FOUND` (404) as for [GET `/todos/{id}`](#get-todosid); `PROJECT_NOT_FOUND` (404).

#### DELETE `/todos/{id}`

Deletes one of the caller's todos, or a team todo (`member` role or above).

Request: no body.

Response `200 OK`

```json
{}
```

Errors: `INVALID_ID` (400) and `NOT_FOUND` (404) as for [GET `/todos/{id}`](#get-todosid).

### Projects

All projects endpoints are Bearer (+ verified email when required).

#### POST `/projects`

Creates a project (a list of todos), personal or for a team (`teamId`, needs the `member` role or above), optionally inside a collection of the same team. Only `name` is required.

Request

```json
{
  "name": "Website relaunch",
  "description": "New site live before Q1.",
  "icon": "🚀",
  "color": "blue",
  "teamId": null,
  "collectionId": "672c61709e1b4d0012a7c3fd"
}
```

Response `201 Created`

```json
{
  "id": "672c61a09e1b4d0012a7c401",
  "userId": "672c5f3a9e1b4d0012a7c3e1",
  "teamId": null,
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

`name` is trimmed, 1–100 characters; `description` up to 2,000 characters; `icon` up to 32 bytes; `color` from the [Color enum](#enums) (default `default`). Errors: `INVALID_NAME`, `NAME_TOO_LONG`, `DESCRIPTION_TOO_LONG`, `ICON_TOO_LONG`, `INVALID_COLOR`, `INVALID_FIELD`, `TEAM_MISMATCH` (400); `INSUFFICIENT_ROLE` (403); `COLLECTION_NOT_FOUND`, `TEAM_NOT_FOUND` (404).

#### GET `/projects`

Lists the caller's personal projects and those of their teams, with their progress, oldest first.

| Parameter | Example | Meaning |
|-----------|---------|---------|
| `team` | `672c7011…` or `personal` | Only one team's projects, or only personal ones |
| `collection` | `672c6170…` | Only projects in this collection |
| `archived` | `true` | `false` (default): active only; `true`: archived only; `all`: both |

Response `200 OK`: an array of projects, as for [POST `/projects`](#post-projects).

Errors: `INVALID_QUERY` (400); `COLLECTION_NOT_FOUND` (404) for an unknown `collection`; `TEAM_NOT_FOUND` (404) for a `team` you're not in.

#### GET `/projects/{id}`

Returns one project with its progress, archived or not.

Response `200 OK`: a project, as for [POST `/projects`](#post-projects).

Errors: `INVALID_ID` (400) on MongoDB and PostgreSQL for a malformed ID; `PROJECT_NOT_FOUND` (404).

#### PATCH `/projects/{id}`

Renames, restyles, archives or moves a project between collections (of its own team): send only the fields to change. Team projects need the `member` role or above; the team can't change (`TEAM_IMMUTABLE`).

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

Deletes the project **and all of its todos**, in one transaction. To keep the todos, move them first (`PATCH /todos/{id}` with another `projectId`, or `null` for the inbox) or archive the project instead. Team projects need the `admin` role or above.

Request: no body.

Response `204 No Content`

Errors: `INVALID_ID` (400); `INSUFFICIENT_ROLE` (403); `PROJECT_NOT_FOUND` (404).

### Collections

All collections endpoints are Bearer (+ verified email when required).

#### POST `/collections`

Creates a collection to group projects, personal or for a team (`teamId`, needs the `member` role or above). Only `name` is required; the fields follow the rules of [POST `/projects`](#post-projects).

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
  "userId": "672c5f3a9e1b4d0012a7c3e1",
  "teamId": null,
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

Errors: `INVALID_NAME`, `NAME_TOO_LONG`, `DESCRIPTION_TOO_LONG`, `ICON_TOO_LONG`, `INVALID_COLOR`, `INVALID_FIELD` (400); `INSUFFICIENT_ROLE` (403); `TEAM_NOT_FOUND` (404).

#### GET `/collections`

Lists the caller's personal collections and those of their teams, oldest first, each with its active projects and their combined progress (a ready-made sidebar).

| Parameter | Example | Meaning |
|-----------|---------|---------|
| `team` | `672c7011…` or `personal` | Only one team's collections, or only personal ones |
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

Deletes the collection only. Its projects and their todos are kept and are no longer filed under any collection. Team collections need the `admin` role or above (`INSUFFICIENT_ROLE`, 403).

Request: no body.

Response `204 No Content`

Errors: `INVALID_ID` (400); `COLLECTION_NOT_FOUND` (404).

### Teams

All teams endpoints are Bearer (+ verified email when required). Non-members get `TEAM_NOT_FOUND` (404) for every team endpoint, so team IDs don't leak. Rules are under [Teams and Roles](#teams-and-roles).

#### POST `/teams`

Creates a team; the caller becomes its owner.

Request

```json
{
  "name": "Platform"
}
```

Response `201 Created`

```json
{
  "id": "672c70119e1b4d0012a7c501",
  "name": "Platform",
  "role": "owner",
  "createdAt": "2026-11-07T11:02:13.402Z",
  "updatedAt": "2026-11-07T11:02:13.402Z",
  "members": [
    {
      "userId": "672c5f3a9e1b4d0012a7c3e1",
      "name": "Ann",
      "email": "ann@example.com",
      "role": "owner",
      "joinedAt": "2026-11-07T11:02:13.402Z"
    }
  ]
}
```

`role` is the caller's role. `name` is trimmed, 1–100 characters. Errors: `INVALID_TEAM_NAME`, `TEAM_NAME_TOO_LONG` (400).

#### GET `/teams`

Lists the caller's teams with their role in each, earliest joined first.

Response `200 OK`: an array of teams as above, without `members`; `[]` when the caller is in none.

#### GET `/teams/{id}`

Returns a team with every member, earliest joined first.

Response `200 OK`: a team, as for [POST `/teams`](#post-teams).

Errors: `TEAM_NOT_FOUND` (404).

#### PATCH `/teams/{id}`

Renames a team. Owners and admins.

Request

```json
{
  "name": "Core platform"
}
```

Response `200 OK`: the team.

Errors: `INVALID_TEAM_NAME`, `TEAM_NAME_TOO_LONG` (400); `INSUFFICIENT_ROLE` (403); `TEAM_NOT_FOUND` (404).

#### DELETE `/teams/{id}`

Deletes the team with its members, invitations, audit log, and all its projects, collections and todos, in one transaction. Owner only.

Request: no body.

Response `204 No Content`

Errors: `INSUFFICIENT_ROLE` (403); `TEAM_NOT_FOUND` (404).

#### PATCH `/teams/{id}/members/{userId}`

Changes another member's role. The caller must outrank both the member's current and the new role (an admin manages members and viewers; the owner also admins).

Request

```json
{
  "role": "admin"
}
```

Response `200 OK`: the team with its members.

Errors: `INVALID_ROLE`, `OWNER_NOT_ASSIGNABLE` (400); `INSUFFICIENT_ROLE` (403); `MEMBER_NOT_FOUND`, `TEAM_NOT_FOUND` (404); `CANNOT_CHANGE_OWN_ROLE` (409).

#### DELETE `/teams/{id}/members/{userId}`

Removes a member, or leaves the team when `userId` is the caller's own ID. Removing needs a role above the member's; anyone but the owner can leave.

Request: no body.

Response `204 No Content`

Errors: `INSUFFICIENT_ROLE` (403); `MEMBER_NOT_FOUND`, `TEAM_NOT_FOUND` (404); `OWNER_CANNOT_LEAVE` (409).

#### POST `/teams/{id}/transfer-ownership`

Makes another member the owner; the caller becomes an admin. Owner only.

Request

```json
{
  "userId": "672c5f8e9e1b4d0012a7c3e9"
}
```

Response `200 OK`: the team with its members.

Errors: `INVALID_USER_ID` (400); `INSUFFICIENT_ROLE` (403); `MEMBER_NOT_FOUND`, `TEAM_NOT_FOUND` (404); `CANNOT_TRANSFER_TO_SELF` (409).

### Invitations

#### POST `/teams/{id}/invitations`

Emails an invitation to join the team with a role below the caller's. Owners and admins. Inviting the same address again replaces its pending invitation.

Request

```json
{
  "email": "bo@example.com",
  "role": "member"
}
```

Response `201 Created`

```json
{
  "id": "672c71029e1b4d0012a7c50a",
  "teamId": "672c70119e1b4d0012a7c501",
  "email": "bo@example.com",
  "role": "member",
  "invitedBy": "672c5f3a9e1b4d0012a7c3e1",
  "expired": false,
  "expiresAt": "2026-11-14T11:06:10.551Z",
  "createdAt": "2026-11-07T11:06:10.551Z"
}
```

The token is only in the email. Errors: `INVALID_EMAIL`, `INVALID_ROLE`, `OWNER_NOT_ASSIGNABLE` (400); `INSUFFICIENT_ROLE` (403) for members and viewers, or a role not below the caller's; `TEAM_NOT_FOUND` (404); `ALREADY_MEMBER` (409).

#### GET `/teams/{id}/invitations`

Lists the team's pending invitations, oldest first; `expired` marks those past `expiresAt`. Owners and admins.

Response `200 OK`: an array of invitations, as above.

Errors: `INSUFFICIENT_ROLE` (403); `TEAM_NOT_FOUND` (404).

#### DELETE `/teams/{id}/invitations/{invitationId}`

Revokes a pending invitation; its link stops working. Owners and admins.

Request: no body.

Response `204 No Content`

Errors: `INSUFFICIENT_ROLE` (403); `INVITATION_NOT_FOUND`, `TEAM_NOT_FOUND` (404).

#### POST `/invitations/accept`

Joins the team with the invited role, using the token from the invitation email. Bearer, rate-limited. The caller's account email must be the invited address; the token is used up.

Request

```json
{
  "token": "dGhpcy1pcy1hbi1pbnZpdGF0aW9uLXRva2Vu"
}
```

Response `200 OK`: the team with its members, as for [GET `/teams/{id}`](#get-teamsid).

Errors: `INVALID_INVITATION_TOKEN` (400) for an unknown, used, revoked, replaced or expired token; `INVITATION_EMAIL_MISMATCH` (403); `ALREADY_MEMBER` (409).

#### POST `/invitations/decline`

Turns an invitation down; the token is used up. Bearer, rate-limited, same body and token errors as accepting.

Response `204 No Content`

### Audit

Audit log endpoints are Bearer (+ verified email when required) and return one page, newest first:

```json
{
  "events": [
    {
      "id": "672c72a19e1b4d0012a7c520",
      "teamId": "672c70119e1b4d0012a7c501",
      "userId": null,
      "actorId": "672c5f8e9e1b4d0012a7c3e9",
      "action": "todo.updated",
      "targetType": "todo",
      "targetId": "672c72809e1b4d0012a7c51c",
      "details": { "title": "Ship it", "fields": "priority,status" },
      "ip": "203.0.113.7",
      "userAgent": "Mozilla/5.0 ...",
      "createdAt": "2026-11-07T11:12:40.203Z"
    }
  ],
  "nextCursor": "MTc2MjUxMzk2MDIwM182NzJjNzJhMTllMWI0ZDAwMTJhN2M1MjA"
}
```

| Parameter | Example | Meaning |
|-----------|---------|---------|
| `limit` | `20` | Page size, 1–200 (default 50) |
| `cursor` | `MTc2…` | `nextCursor` of the previous page; `nextCursor` is `null` on the last page |

The IP is the connecting address (`X-Forwarded-For` is not trusted). Errors: `INVALID_LIMIT`, `INVALID_CURSOR` (400).

#### GET `/teams/{id}/audit-log`

The team audit log (actions are listed under [Audit logs](#audit-logs)). Owners and admins.

Errors: `INSUFFICIENT_ROLE` (403); `TEAM_NOT_FOUND` (404).

#### GET `/me/audit-log`

The caller's own security log. Events have `userId` set and `teamId` null; `actorId` is null when the actor is unknown (a failed login, a password reset request).

## Testing

### Unit tests

```bash
make test        # go test -race ./...
```

Covers domains, commands and queries, adapters, middleware, migrations and codegen, plus an end-to-end suite (`cmd/api/app_test.go`, `organize_test.go`, `teams_test.go`) that drives every endpoint through the production router. Without database settings it runs on the memory backend and skips the database integration tests.

### Integration tests

```bash
make test-integration   # needs Docker
make test-db-down       # stop the test databases
```

This starts PostgreSQL and a single-node MongoDB replica set from `docker-compose.test.yml`, then:

1. Runs every test with `TEST_POSTGRES_URI` and `TEST_MONGO_URI` set. Each persistence adapter runs its module's **store contract** (`adapters/persistence/storetest`): the same suite for memory, MongoDB and PostgreSQL, covering CRUD, ownership and team scopes, memberships and invitations, audit paging, unique emails, single-use tokens, concurrent refresh-token rotation, todo filters, sorting and progress, and transaction rollback and nesting. Migrations are also tested on existing data, upgrade and rollback.
2. Runs the end-to-end suite again on each database (`TEST_E2E_BACKEND=postgres|mongo`).

Every test gets its own freshly migrated database (`internal/platform/database/dbtest`), dropped when it ends. CI runs the same target on every pull request and before each image is published.

## TO-DO

- Caching and distributed tracing were deliberately left out: there is no measured need yet. Request IDs and structured access logs cover correlation for now.
- A shared rate-limit store (e.g. Redis) once the API runs as several replicas; the current limiter counts per process.

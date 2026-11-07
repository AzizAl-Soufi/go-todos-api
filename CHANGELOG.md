# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). The project has no tagged releases yet; earlier history is summarized by date and commit.

## [Unreleased]

The codebase was restructured into a modular monolith with Clean Architecture / Hexagonal layering. Password authentication, refresh-token rotation, email verification and password reset were added, along with integration tests against real databases. Todos can now be organized in projects and collections, with statuses, priorities, deadlines, notes, tags, archiving, progress summaries, filtering and sorting. Teams share projects with roles and email invitations, and an audit log records team activity and account security. **The HTTP API changes in breaking ways**; see [Upgrade notes](#upgrade-notes).

### Added

- **Teams.** `POST /teams` creates a team owned by the caller; `GET /teams`, `GET/PATCH/DELETE /teams/{id}`. Deleting a team deletes its projects, collections, todos, invitations and audit log in one transaction.
- **Roles.** `owner`, `admin`, `member`, `viewer`. Members edit team content, admins also manage people and delete team projects and collections, the owner also deletes the team. Managers act only on people below them and grant roles below their own. `PATCH /teams/{id}/members/{userId}` changes a role, `DELETE /teams/{id}/members/{userId}` removes a member (or leaves), `POST /teams/{id}/transfer-ownership` hands the team over.
- **Invitations.** `POST /teams/{id}/invitations` emails a single-use link (7 days) for an email address and role; `GET` lists and `DELETE /teams/{id}/invitations/{id}` revokes them. The invitee accepts or declines with `POST /invitations/accept|decline`, signed in with the invited address. Re-inviting replaces the pending link.
- **Team-owned content.** `teamId` on `POST /projects` and `POST /collections`; todos belong to the team of their project. `GET /todos`, `/projects` and `/collections` list personal and team records, filtered with `?team={id}|personal`; responses carry `teamId` and `userId` (creator). Moving a team todo to the inbox makes it the mover's personal todo.
- **Audit logs.** Team audit log (`GET /teams/{id}/audit-log`, owners and admins) for team, member, invitation and content changes; personal security log (`GET /me/audit-log`) for registration, logins and failed logins, logouts, refresh-token reuse, verification and password events. Events carry actor, target, details, client IP and User-Agent, are written in the same transaction as the change, and are paged newest first with `?limit=` and `?cursor=`.
- New error codes: `TEAM_NOT_FOUND`, `INSUFFICIENT_ROLE`, `MEMBER_NOT_FOUND`, `INVITATION_NOT_FOUND`, `INVALID_INVITATION_TOKEN`, `INVITATION_EMAIL_MISMATCH`, `ALREADY_MEMBER`, `OWNER_NOT_ASSIGNABLE`, `OWNER_CANNOT_LEAVE`, `CANNOT_CHANGE_OWN_ROLE`, `CANNOT_TRANSFER_TO_SELF`, `OWNS_TEAMS`, `INVALID_ROLE`, `INVALID_TEAM_NAME`, `TEAM_NAME_TOO_LONG`, `INVALID_EMAIL`, `TEAM_MISMATCH`, `TEAM_IMMUTABLE`, `INVALID_CURSOR`, `INVALID_LIMIT`, `INVALID_USER_ID`.
- **Schema.** PostgreSQL migrations `00007` (`teams`, `team_members`, `team_invitations`), `00008` (`audit_events`) and `00009` (`team_id` on collections, projects and todos; their foreign keys to `users` are dropped so team records outlive their creator). MongoDB migration 005 creates the new collections and indexes. Upgrade and rollback of `00009` are tested on real data.
- Platform packages `access` (roles, `Actor`, membership middleware), `mail` (shared `Sender`), `opaque` (single-use tokens), and `httpx.ClientInfo`.

- **Projects and collections.** A project is a list of todos, and a collection groups projects. Both have a name, description, icon and color.
  - Endpoints: `/projects` and `/collections` (create, list, get, `PATCH`, delete).
  - Deleting a project deletes its todos atomically; deleting a collection keeps its projects. Todos without a project stay in the inbox (`?project=inbox`).
- **Archiving.** `"archived": true` hides a project or collection, and the todos of archived projects, until asked for with `?archived=` or `?include_archived=true`.
- **Progress summaries.** Each project and collection reports counts per status, overdue todos and `percentDone`.
- **Richer todos:**
  - `status` (`pending`, `in_progress`, `blocked`, `done`, `cancelled`), `priority` (`none` … `urgent`), `dueAt`, `notes` and `tags`;
  - automatic `completedAt` and `updatedAt`, and a computed `overdue`.
- **Filtering, search and sorting** on `GET /todos`: by project or inbox, status, priority, tag, deadline window, overdue, and text search in title and notes; sorted by `created`, `updated`, `due` or `priority`.
- `PATCH /todos/{id}` (same as `PUT`); `null` clears `dueAt` or moves a todo to the inbox.
- **Schema.** PostgreSQL migration `00006` adds the `collections` and `projects` tables and the new todo columns. MongoDB migration 004 adds their indexes. Both convert existing todos' `completed` flag into `status` and `completedAt`; both upgrade and rollback are tested on real data.
- **Password authentication.** `POST /auth/register` (201, or 409 `EMAIL_TAKEN`) and `POST /auth/login`, with bcrypt hashing (cost 12) and passwords of 8–72 bytes. Unknown emails take as long to reject as wrong passwords.
- **Rotating refresh tokens.** Opaque tokens stored as SHA-256 hashes, valid 7 days. `POST /auth/refresh` rotates the token on every use. Replaying an already-used token revokes the whole login (`REFRESH_TOKEN_REUSED`), and concurrent refreshes have exactly one winner.
- **Logout.** `POST /auth/logout` ends one login; `POST /auth/logout-all` ends all of them and revokes every access token immediately.
- **Token versioning.** Access tokens carry a `ver` claim that is checked on every request, so revocation takes effect at once (`TOKEN_REVOKED`).
- **Email verification.** Single-use links (24 h) sent on registration and through `POST /auth/verify-email/resend`. Verify with `POST /auth/verify-email` or the emailed `GET /auth/verify-email?token=…` link. `AUTH_REQUIRE_VERIFIED_EMAIL=true` blocks the todos API until the address is verified (`EMAIL_NOT_VERIFIED`).
- **Password reset.** `POST /auth/password/forgot` always answers 202 and emails a single-use link (1 h) to `AUTH_PASSWORD_RESET_URL`. `POST /auth/password/reset` sets the new password, revokes all sessions and verifies the email. Legacy accounts without a password use this to get one.
- **Password change.** `POST /auth/password/change` checks the current password, revokes other sessions and returns a fresh token pair.
- **Account endpoints.** `GET /me` returns the profile with todos; `DELETE /me` deletes the account and all its data in one transaction, across modules.
- **Mail.** A `Mailer` port with a log adapter (default) and an SMTP adapter (`SMTP_*` settings), which rejects header injection.
- **Rate limiting.** A per-client-IP token bucket on the public `/auth/*` endpoints and on password change (`429 RATE_LIMITED` with `Retry-After`; `AUTH_RATE_LIMIT_PER_MINUTE`, `AUTH_RATE_LIMIT_BURST`).
- **Observability.** `X-Request-ID` on every response and a structured access log line per request.
- **Error codes.** Every error now carries a stable machine-readable `code`; see [Changed](#changed) for the new body.
- **JSON 404/405.** Unknown routes and wrong methods answer `404 NOT_FOUND` and `405 METHOD_NOT_ALLOWED` in the standard error shape.
- **Embedded migrations.** PostgreSQL migrations are embedded in the binaries and run with goose. `cmd/migrate` handles both databases, and `MIGRATE_ON_START=true` applies migrations at startup.
- **Schema.** New PostgreSQL migrations:
  - `00003`: `password_hash`, `email_verified_at` and `token_version` on `users`, plus the `sessions` and `email_verifications` tables;
  - `00004`: `password_resets`;
  - `00005`: index on `todos (user_id, created_at DESC)`.

  New MongoDB migrations 002 and 003 add the session, verification and reset indexes, including TTL indexes.
- **Transactions.** A `tx.Runner` unit of work for PostgreSQL and for MongoDB replica sets; standalone MongoDB falls back to running without transactions, with a warning.
- **Store contract tests.** Memory, MongoDB and PostgreSQL run the same contract suite per module.
- **Integration tests.** `make test-integration` runs the contracts and the end-to-end suite against real PostgreSQL and a MongoDB replica set in Docker (`docker-compose.test.yml`), and a new CI job runs it on every pull request and before each image is published.
- **Documentation.** `ARCHITECTURE.md`, this changelog, a README with a table of contents, database schema trees and per-endpoint request and response examples, and `tests/api.http` covering every endpoint.
- **Configuration keys:** `APP_BASE_URL`, `MIGRATE_ON_START`, `MONGO_MIGRATIONS_CONFIG`, `AUTH_REQUIRE_VERIFIED_EMAIL`, `AUTH_RATE_LIMIT_PER_MINUTE`, `AUTH_RATE_LIMIT_BURST`, `AUTH_PASSWORD_RESET_URL`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`.

### Changed

- `DELETE /me` now leaves every team: teams the user owns alone are deleted, owning a team with other members answers `409 OWNS_TEAMS`, and team records the user created stay with their team. It also deletes the user's security log.
- Todos, projects and collections endpoints now authorize through the caller's team memberships (loaded per request); store calls take a scope (personal + teams) instead of a user ID.
- Login runs in a transaction with its audit event; the identity mail adapter now writes messages for the shared platform `mail.Sender`.

- **CQRS application layer.** Each module's single service was split into commands (`application/command`) and queries (`application/query`), one small handler per use case.
  - Commands return at most a new ID or issued credentials; HTTP endpoints read the response back with a query.
  - Queries depend only on read-only ports, so they cannot change state.
  - Repositories and readers are separate interfaces implemented by the same stores; the API is unchanged.
- **Simpler code.**
  - Creating and updating share one input type (`domain.Changes`, `domain.MetaChanges`).
  - The HTTP plumbing lives once in the platform (`httpx.Endpoint`, `auth.ForUser`).
  - Repeated steps live in one place: issuing and redeeming one-time tokens, checking passwords, revoking credentials, mapping "not found" errors.
  - The todo update rules are split into small functions.
- Todos store a `status` instead of the `completed` flag. Responses still include `completed` (derived), and requests may still send it, so existing clients keep working.
- `GET /todos` hides todos of archived projects by default; `GET /me` still lists every todo, now with its `status`.
- **Breaking:** the error body is now `{"error": {"code": "…", "message": "…"}}` instead of `{"error": "…"}`.
- **Breaking:** auth and account routes moved:

  | Before | Now |
  |--------|-----|
  | `POST /register` | `POST /auth/register` (now requires `password`) |
  | `POST /auth` | `POST /auth/login` (email + password) |
  | `POST /refresh-token` | `POST /auth/refresh` (rotates the token) |
  | `GET /overview` | `GET /me` |
  | `DELETE /user` | `DELETE /me` |

- **Breaking:** login and register respond with `{user, authorization}`, where `authorization` is `{access_token, refresh_token, token_type, expires_in}`.
- **Breaking:** emails are trimmed and lower-cased on register and login.
- Todo titles are limited to 500 characters and names to 100 characters.
- Code reorganized into `internal/modules/{identity,todos}` (domain, application, transport, adapters) and `internal/platform` (shared kernel). HTTP handlers live in `transport/`, driven adapters in `adapters/`.
- Each module owns its SQL with its own `sqlc` package.
- The binaries moved: the API is built from `./cmd/api` and migrations from `./cmd/migrate` (was `./cmd/migration`).
- `make test` runs with `-race`; config tests no longer pick up the shell environment or a project `.env`.

### Removed

- The old layer-based packages (`internal/domain`, `handler`, `service`, `repository`, `middleware`, `shared`) and the single shared `sqlc` package.
- The old per-endpoint `tests/*.http` files, replaced by `tests/api.http`.

### Fixed

- The MongoDB `users` collection has a unique email index, so concurrent registrations can't create duplicates.
- Account deletion is atomic: todos, sessions, pending tokens and the user are removed in one transaction.

### Security

- Access tokens must carry an expiry, and the RSA key pair is checked to match at startup.
- Refresh, verification and reset tokens are stored only as SHA-256 hashes.
- Login and forgot-password responses don't reveal which emails have accounts.

### Upgrade notes

- **Clients** must move to the new routes and the new error and token response shapes listed under [Changed](#changed).
- **Clients reading `completed`** keep working. New clients should use `status`, since a todo can now also be `in_progress`, `blocked` or `cancelled`.
- **Run the migrations** (`make migrate`, `migrate` in the image, or `MIGRATE_ON_START=true`) before starting the new version.
- **Existing accounts have no password** and can't log in until they use `POST /auth/password/forgot`. That needs working email (`SMTP_*`) and `AUTH_PASSWORD_RESET_URL` pointing at a page in your client that calls `POST /auth/password/reset`.
- **Mixed-case emails** stored before this change are not found at login until the data is lower-cased.
- **MongoDB** needs a replica set for atomic account deletion and password changes.
- The two seed users in `cmd/migrate/seeds` have no passwords.

## History before this changelog

- **2026-11-07**:
  - Docker support; CI with tests and govulncheck; CD publishing the image to GHCR (`3defbca`).
  - Tests across `internal/`; in-memory repositories that copy values in and out (`b8832c1`).
  - Hardening and cleanup (`1dd9ffb`):
    - RS256-only access tokens, a 1 MiB body limit, panic recovery and generic `500` responses;
    - trimmed titles, and partial `PUT /todos/{id}` that rejects `null`;
    - shared helpers to remove duplication.
- **2026-08-12**: MongoDB migrations, schema-driven code generation and seeding (`7138cd5`).
- **2026-08-11**:
  - PostgreSQL support with `sqlc` alongside MongoDB (`0170116`, `aeff81c`).
  - String IDs in the service interfaces (`2bb80c9`).
  - Expanded README (`3533fe8`).
- **2026-08-09 – 2026-08-10**:
  - User management with registration, RS256 access and refresh tokens, and overview and delete endpoints.
  - Typed application errors.
  - Module path change (`5626271` … `77f3cc4`).
- **2026-08-06 – 2026-08-08**: First version: todos CRUD over HTTP with MongoDB and in-memory storage (`a13bbfd` … `e4d4b87`).

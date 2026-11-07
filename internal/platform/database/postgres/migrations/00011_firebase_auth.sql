-- +goose Up
-- Sign-in moves to Firebase Authentication (identity module). An account is
-- linked to its Firebase user by firebase_uid; accounts from before stay
-- unlinked (NULL) until their first Firebase sign-in with a verified email.
-- sessions_valid_after revokes sign-ins (log out everywhere).
--
-- password_hash, token_version, sessions, email_verifications and
-- password_resets are no longer used. They are left in place so this
-- release can be rolled back; a later migration drops them.
ALTER TABLE users
    ADD COLUMN firebase_uid         TEXT,
    ADD COLUMN sessions_valid_after TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_firebase_uid ON users (firebase_uid);

-- +goose Down
DROP INDEX IF EXISTS idx_users_firebase_uid;
ALTER TABLE users
    DROP COLUMN IF EXISTS sessions_valid_after,
    DROP COLUMN IF EXISTS firebase_uid;

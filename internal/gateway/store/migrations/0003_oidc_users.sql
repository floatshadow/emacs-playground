-- OIDC claims refreshed at every login and the session epoch that logout
-- increments (backend.md §2.6); the sign-in code table of earlier releases
-- goes.
DROP TABLE IF EXISTS invites;
ALTER TABLE users ADD COLUMN username TEXT;
ALTER TABLE users ADD COLUMN email TEXT;
ALTER TABLE users ADD COLUMN groups_json TEXT;
ALTER TABLE users ADD COLUMN session_epoch INTEGER NOT NULL DEFAULT 0;

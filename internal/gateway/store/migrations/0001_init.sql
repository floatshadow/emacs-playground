-- Initial schema (backend.md §2.7).
CREATE TABLE users (
  id TEXT PRIMARY KEY, kind TEXT NOT NULL,            -- local | invite | oidc
  subject TEXT UNIQUE, display_name TEXT,
  created_at INTEGER NOT NULL, last_seen_at INTEGER);
CREATE TABLE invites (
  code_hash TEXT PRIMARY KEY, label TEXT, max_uses INTEGER, uses INTEGER NOT NULL DEFAULT 0, expires_at INTEGER);
CREATE TABLE sessions (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id),
  state TEXT NOT NULL, container_id TEXT, remote_container_id TEXT, volume TEXT, image TEXT,
  token_hash TEXT NOT NULL, created_at INTEGER NOT NULL, last_active_at INTEGER, stopped_at INTEGER, failure TEXT);
CREATE TABLE progress (
  user_id TEXT NOT NULL, lesson_id TEXT NOT NULL, step_id TEXT NOT NULL,
  status TEXT NOT NULL,                                -- in-progress | done
  hints_used INTEGER NOT NULL DEFAULT 0, resets INTEGER NOT NULL DEFAULT 0,
  completed_at INTEGER, updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, lesson_id, step_id));
CREATE TABLE stuck_reports (
  id INTEGER PRIMARY KEY, user_id TEXT NOT NULL, lesson_id TEXT, step_id TEXT,
  created_at INTEGER NOT NULL, note TEXT, events_json TEXT NOT NULL);
CREATE TABLE audit (
  id INTEGER PRIMARY KEY, at INTEGER NOT NULL, user_id TEXT, action TEXT NOT NULL, details TEXT);

CREATE INDEX sessions_user ON sessions (user_id, created_at);
CREATE INDEX stuck_reports_created ON stuck_reports (created_at);

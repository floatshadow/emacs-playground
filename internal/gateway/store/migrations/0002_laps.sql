-- Finished "again, without hints" laps per lesson (api.md §2.4).
CREATE TABLE laps (
  user_id TEXT NOT NULL, lesson_id TEXT NOT NULL,
  count INTEGER NOT NULL, best_ms INTEGER NOT NULL, last_ms INTEGER NOT NULL, last_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, lesson_id));

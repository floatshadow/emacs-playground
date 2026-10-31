-- Gateway-issued reset generations reject obsolete reports (backend.md §2.9).
CREATE TABLE progress_resets (
  user_id TEXT NOT NULL, lesson_id TEXT NOT NULL, generation INTEGER NOT NULL,
  PRIMARY KEY (user_id, lesson_id));

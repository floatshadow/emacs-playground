-- The id of the last lap counted, so a lap reported twice counts once (api.md §2.4).
ALTER TABLE laps ADD COLUMN last_lap_id TEXT;

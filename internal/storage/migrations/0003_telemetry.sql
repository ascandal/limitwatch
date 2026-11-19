-- Recent raw telemetry for monitoring. Retention is capped by the store.
CREATE TABLE IF NOT EXISTS telemetry_samples (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    entity TEXT NOT NULL,
    value REAL NOT NULL,
    ts INTEGER NOT NULL,
    received_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS telemetry_stream_time ON telemetry_samples(name, entity, ts);

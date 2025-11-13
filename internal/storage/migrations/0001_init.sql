-- 0001_init.sql
-- Core tables: rules, alarms, alarm_events
BEGIN;

CREATE TABLE IF NOT EXISTS rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    signal TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('threshold','roc','duration')),
    severity TEXT NOT NULL CHECK (severity IN ('info','warn','crit')),
    latch INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    config TEXT NOT NULL, -- JSON string (type-specific)
    version INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS alarms (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
    entity TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('inactive','active','acked','silenced')),
    first_seen DATETIME,
    last_seen DATETIME,
    acked_by TEXT,
    silence_until DATETIME,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(rule_id, entity)
);

CREATE TABLE IF NOT EXISTS alarm_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alarm_id INTEGER NOT NULL REFERENCES alarms(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('activated','acked','silenced','cleared','note')),
    actor TEXT,
    at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    note TEXT,
    payload TEXT
);

COMMIT;

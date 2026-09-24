CREATE TABLE devices (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    address    TEXT NOT NULL,
    interval_s INTEGER NOT NULL,
    timeout_s  INTEGER NOT NULL,
    labels     TEXT NOT NULL,
    secret_id  TEXT REFERENCES secrets(id) ON DELETE RESTRICT,
    config     TEXT NOT NULL,
    enabled    INTEGER NOT NULL,
    revision   INTEGER NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;

CREATE TRIGGER devices_insert_changes AFTER INSERT ON devices BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER devices_update_changes AFTER UPDATE ON devices BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER devices_delete_changes AFTER DELETE ON devices BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

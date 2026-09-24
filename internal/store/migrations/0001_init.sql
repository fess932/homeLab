CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL
) STRICT;

CREATE TABLE sessions (
    token_hash BLOB PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
) STRICT;
CREATE INDEX sessions_expires ON sessions(expires_at);

CREATE TABLE assets (
    id         TEXT PRIMARY KEY,
    media_type TEXT NOT NULL,
    size       INTEGER NOT NULL,
    width      INTEGER NOT NULL,
    height     INTEGER NOT NULL,
    checksum   TEXT NOT NULL,
    path       TEXT NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE settings (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    title          TEXT NOT NULL,
    logo_asset_id  TEXT REFERENCES assets(id) ON DELETE SET NULL,
    start_page_id  TEXT,
    public_page_id TEXT,
    revision       INTEGER NOT NULL
) STRICT;
INSERT INTO settings (id, title, revision) VALUES (1, 'HomeDeck', 1);

CREATE TABLE pages (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    ord        INTEGER NOT NULL,
    theme      TEXT NOT NULL,
    revision   INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE page_groups (
    id        TEXT PRIMARY KEY,
    page_id   TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    title     TEXT NOT NULL,
    collapsed INTEGER NOT NULL,
    ord       INTEGER NOT NULL
) STRICT;
CREATE INDEX page_groups_page ON page_groups(page_id, ord);

CREATE TABLE widgets (
    id       TEXT PRIMARY KEY,
    page_id  TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    group_id TEXT NOT NULL REFERENCES page_groups(id) ON DELETE CASCADE,
    type     TEXT NOT NULL,
    public   INTEGER NOT NULL,
    config   TEXT NOT NULL,
    layout   TEXT NOT NULL,
    ord      INTEGER NOT NULL
) STRICT;
CREATE INDEX widgets_page ON widgets(page_id, ord);

CREATE TABLE secrets (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    kind              TEXT NOT NULL,
    mask              TEXT NOT NULL,
    encrypted_payload BLOB NOT NULL,
    key_version       INTEGER NOT NULL,
    created_at        INTEGER NOT NULL
) STRICT;

CREATE TABLE sources (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    url          TEXT NOT NULL,
    interval_s   INTEGER NOT NULL,
    timeout_s    INTEGER NOT NULL,
    labels       TEXT NOT NULL,
    secret_id    TEXT REFERENCES secrets(id) ON DELETE RESTRICT,
    tls          TEXT NOT NULL,
    enabled      INTEGER NOT NULL,
    revision     INTEGER NOT NULL,
    created_at   INTEGER NOT NULL,
    last_attempt INTEGER,
    last_success INTEGER,
    last_error   TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE services (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    url         TEXT NOT NULL,
    icon        TEXT NOT NULL,
    tags        TEXT NOT NULL,
    open_mode   TEXT NOT NULL,
    source_id   TEXT REFERENCES sources(id) ON DELETE SET NULL,
    revision    INTEGER NOT NULL,
    created_at  INTEGER NOT NULL
) STRICT;

CREATE TABLE checks (
    id              TEXT PRIMARY KEY,
    service_id      TEXT NOT NULL UNIQUE REFERENCES services(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL,
    target          TEXT NOT NULL,
    expected_status TEXT NOT NULL,
    interval_s      INTEGER NOT NULL,
    timeout_s       INTEGER NOT NULL,
    enabled         INTEGER NOT NULL,
    ca_pem          TEXT NOT NULL,
    revision        INTEGER NOT NULL
) STRICT;

CREATE TABLE presets (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    expression  TEXT NOT NULL,
    unit        TEXT NOT NULL,
    legend      TEXT NOT NULL,
    thresholds  TEXT NOT NULL,
    min_step_s  INTEGER NOT NULL,
    revision    INTEGER NOT NULL
) STRICT;

CREATE TABLE config_state (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    desired_revision INTEGER NOT NULL,
    applied_revision INTEGER NOT NULL,
    error            TEXT NOT NULL,
    updated_at       INTEGER NOT NULL
) STRICT;
INSERT INTO config_state VALUES (1, 1, 0, '', 0);

CREATE TABLE config_revisions (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    schema_version INTEGER NOT NULL,
    reason         TEXT NOT NULL,
    path           TEXT NOT NULL,
    created_at     INTEGER NOT NULL
) STRICT;

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

INSERT INTO meta (key, value) VALUES ('change_counter', '0');

CREATE TRIGGER pages_insert_changes AFTER INSERT ON pages BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER pages_update_changes AFTER UPDATE ON pages BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER pages_delete_changes AFTER DELETE ON pages BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER page_groups_insert_changes AFTER INSERT ON page_groups BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER page_groups_update_changes AFTER UPDATE ON page_groups BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER page_groups_delete_changes AFTER DELETE ON page_groups BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER widgets_insert_changes AFTER INSERT ON widgets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER widgets_update_changes AFTER UPDATE ON widgets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER widgets_delete_changes AFTER DELETE ON widgets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER services_insert_changes AFTER INSERT ON services BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER services_update_changes AFTER UPDATE ON services BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER services_delete_changes AFTER DELETE ON services BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER checks_insert_changes AFTER INSERT ON checks BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER checks_update_changes AFTER UPDATE ON checks BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER checks_delete_changes AFTER DELETE ON checks BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER presets_insert_changes AFTER INSERT ON presets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER presets_update_changes AFTER UPDATE ON presets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER presets_delete_changes AFTER DELETE ON presets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER assets_insert_changes AFTER INSERT ON assets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER assets_update_changes AFTER UPDATE ON assets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER assets_delete_changes AFTER DELETE ON assets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER secrets_insert_changes AFTER INSERT ON secrets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER secrets_update_changes AFTER UPDATE ON secrets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER secrets_delete_changes AFTER DELETE ON secrets BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER settings_insert_changes AFTER INSERT ON settings BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER settings_update_changes AFTER UPDATE ON settings BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER settings_delete_changes AFTER DELETE ON settings BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER sources_insert_changes AFTER INSERT ON sources BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER sources_update_changes AFTER UPDATE OF name, kind, url, interval_s, timeout_s, labels, secret_id, tls, enabled ON sources BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

CREATE TRIGGER sources_delete_changes AFTER DELETE ON sources BEGIN
    UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'change_counter';
END;

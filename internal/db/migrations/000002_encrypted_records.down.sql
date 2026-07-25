-- Reverses the end-to-end encryption migration.
--
-- This restores the plaintext schema shape but CANNOT restore plaintext data:
-- the server never held the keys, so sealed records are unreadable here. Rolling
-- back leaves empty record tables. Recovery is from client devices, which hold
-- the only copies of the content.

DROP TABLE IF EXISTS records;
DROP INDEX IF EXISTS idx_records_account_type;

CREATE TABLE cycles (
    id            TEXT PRIMARY KEY,
    account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    start_date    TEXT NOT NULL,
    end_date      TEXT,
    length_days   INTEGER CHECK (length_days IS NULL OR length_days BETWEEN 10 AND 200),
    bleeding_days INTEGER CHECK (bleeding_days IS NULL OR bleeding_days BETWEEN 0 AND 45),
    certainty     TEXT NOT NULL DEFAULT 'recorded'
                  CHECK (certainty IN ('recorded', 'uncertain', 'estimated')),
    source        TEXT NOT NULL DEFAULT 'manual'
                  CHECK (source IN ('manual', 'import', 'estimated')),
    notes         TEXT NOT NULL DEFAULT '',
    client_rev    TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    deleted_at    TEXT
);

CREATE INDEX idx_cycles_account_date ON cycles(account_id, start_date);
CREATE UNIQUE INDEX uq_cycles_account_start_live
    ON cycles(account_id, start_date) WHERE deleted_at IS NULL;

CREATE TABLE bleeding_observations (
    id             TEXT PRIMARY KEY,
    account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    observed_date  TEXT NOT NULL,
    flow           TEXT NOT NULL
                   CHECK (flow IN ('none', 'spotting', 'light', 'medium', 'heavy')),
    intermenstrual INTEGER NOT NULL DEFAULT 0 CHECK (intermenstrual IN (0, 1)),
    product_count  INTEGER CHECK (product_count IS NULL OR product_count BETWEEN 0 AND 50),
    notes          TEXT NOT NULL DEFAULT '',
    client_rev     TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    deleted_at     TEXT
);

CREATE TABLE daily_entries (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    entry_date   TEXT NOT NULL,
    pain_level   INTEGER CHECK (pain_level IS NULL OR pain_level BETWEEN 1 AND 5),
    mood_level   INTEGER CHECK (mood_level IS NULL OR mood_level BETWEEN 1 AND 5),
    energy_level INTEGER CHECK (energy_level IS NULL OR energy_level BETWEEN 1 AND 5),
    notes        TEXT NOT NULL DEFAULT '',
    client_rev   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    deleted_at   TEXT
);

CREATE TABLE symptom_definitions (
    id         TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    label      TEXT NOT NULL,
    category   TEXT NOT NULL
               CHECK (category IN ('pain', 'mood', 'energy', 'physical', 'other')),
    client_rev TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE TABLE symptom_logs (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    log_date    TEXT NOT NULL,
    logged_at   TEXT NOT NULL,
    symptom_key TEXT NOT NULL,
    severity    INTEGER NOT NULL CHECK (severity BETWEEN 1 AND 5),
    notes       TEXT NOT NULL DEFAULT '',
    client_rev  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    deleted_at  TEXT
);

CREATE TABLE biomarker_observations (
    id              TEXT PRIMARY KEY,
    account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    observed_date   TEXT NOT NULL,
    bbt_celsius     REAL,
    bbt_quality     TEXT NOT NULL DEFAULT 'normal'
                    CHECK (bbt_quality IN ('normal', 'disturbed', 'unreliable')),
    notes           TEXT NOT NULL DEFAULT '',
    client_rev      TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    deleted_at      TEXT
);

CREATE TABLE medication_logs (
    id         TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    log_date   TEXT NOT NULL,
    name       TEXT NOT NULL,
    dose       TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL DEFAULT 'other'
               CHECK (kind IN ('analgesic', 'hormonal', 'supplement', 'other')),
    notes      TEXT NOT NULL DEFAULT '',
    client_rev TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE INDEX idx_medication_account_date ON medication_logs(account_id, log_date);

DROP TABLE IF EXISTS sync_changes;

CREATE TABLE sync_changes (
    seq         INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL
                CHECK (entity_type IN ('cycle', 'bleeding_observation', 'daily_entry',
                                       'symptom_definition', 'symptom_log',
                                       'biomarker_observation', 'medication_log')),
    entity_id   TEXT NOT NULL,
    deleted     INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0, 1)),
    payload     TEXT,
    updated_at  TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);

CREATE INDEX idx_sync_changes_pull ON sync_changes(account_id, seq);

DROP TABLE IF EXISTS account_settings;

CREATE TABLE account_settings (
    account_id     TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    locale         TEXT NOT NULL DEFAULT 'fr',
    time_zone      TEXT NOT NULL DEFAULT 'Europe/Paris',
    life_stage     TEXT NOT NULL DEFAULT 'unknown'
                   CHECK (life_stage IN (
                       'unknown',
                       'reproductive_early',
                       'reproductive_peak',
                       'reproductive_late',
                       'menopause_transition_early',
                       'menopause_transition_late',
                       'postmenopause_early',
                       'postmenopause_late'
                   )),
    tracking_focus TEXT NOT NULL DEFAULT '[]',
    updated_at     TEXT NOT NULL
);

DROP TABLE IF EXISTS duo_payloads;
DROP TABLE IF EXISTS support_requests;

CREATE TABLE support_requests (
    id              TEXT PRIMARY KEY,
    link_id         TEXT NOT NULL REFERENCES duo_links(id) ON DELETE CASCADE,
    author_role     TEXT NOT NULL CHECK (author_role IN ('tracker', 'partner')),
    kind            TEXT NOT NULL DEFAULT 'general'
                    CHECK (kind IN ('general', 'comfort', 'practical', 'space')),
    message         TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    acknowledged_at TEXT
);

CREATE INDEX idx_support_link ON support_requests(link_id, created_at);

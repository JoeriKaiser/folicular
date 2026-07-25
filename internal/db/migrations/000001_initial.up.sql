-- folicular initial schema.
-- Rationale and research citations: docs/data-model.md, docs/research/SOURCES.md.
-- Dates are ISO-8601 TEXT; instants are RFC 3339 UTC TEXT; booleans are INTEGER 0/1.

-- Identity -----------------------------------------------------------------

CREATE TABLE accounts (
    id         TEXT PRIMARY KEY,
    code_hash  BLOB NOT NULL UNIQUE,
    status     TEXT NOT NULL DEFAULT 'active'
               CHECK (status IN ('active', 'disabled', 'deleted')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE devices (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name         TEXT NOT NULL DEFAULT '',
    token_hash   BLOB NOT NULL UNIQUE,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT,
    revoked_at   TEXT
);

CREATE INDEX idx_devices_account ON devices(account_id);

CREATE TABLE account_settings (
    account_id     TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    locale         TEXT NOT NULL DEFAULT 'fr',
    time_zone      TEXT NOT NULL DEFAULT 'Europe/Paris',
    -- STRAW+10-aligned life stages (S11). User-selected, never inferred.
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
    -- JSON array of user-selected tracking focuses (S20-S25), never diagnoses:
    -- pms | pmdd | endometriosis | pcos | custom
    tracking_focus TEXT NOT NULL DEFAULT '[]',
    updated_at     TEXT NOT NULL
);

-- Synchronized records ------------------------------------------------------
-- Shared envelope: id (client UUIDv7), client_rev, created_at, updated_at,
-- deleted_at (tombstone). See docs/data-model.md.

-- Cycle day 1 = first day of full menstrual flow (S01). Bounds are
-- permissive plausibility, not normative ranges (S03, S06).
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

-- Daily bleeding self-observation. FIGO-aligned vocabulary (S01, S02):
-- spotting is flagged as intermenstrual bleeding; no cause taxonomy.
CREATE TABLE bleeding_observations (
    id             TEXT PRIMARY KEY,
    account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    observed_date  TEXT NOT NULL,
    flow           TEXT NOT NULL
                   CHECK (flow IN ('none', 'spotting', 'light', 'medium', 'heavy')),
    intermenstrual INTEGER NOT NULL DEFAULT 0 CHECK (intermenstrual IN (0, 1)),
    product_count  INTEGER CHECK (product_count IS NULL OR product_count BETWEEN 0 AND 60),
    notes          TEXT NOT NULL DEFAULT '',
    client_rev     TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    deleted_at     TEXT
);

CREATE UNIQUE INDEX uq_bleeding_account_date_live
    ON bleeding_observations(account_id, observed_date) WHERE deleted_at IS NULL;

-- Prospective daily charting (S25): severity scales 1-5, nullable when not
-- recorded. Condition-aware tracking uses this without encoding conditions.
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

CREATE UNIQUE INDEX uq_daily_account_date_live
    ON daily_entries(account_id, entry_date) WHERE deleted_at IS NULL;

-- Per-account symptom catalog (built-in seed + user customs).
CREATE TABLE symptom_definitions (
    id         TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    label      TEXT NOT NULL,
    category   TEXT NOT NULL
               CHECK (category IN ('mood', 'physical', 'energy', 'pain', 'cervical_fluid', 'other')),
    builtin    INTEGER NOT NULL DEFAULT 0 CHECK (builtin IN (0, 1)),
    active     INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    client_rev TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE UNIQUE INDEX uq_symptomdef_account_key_live
    ON symptom_definitions(account_id, key) WHERE deleted_at IS NULL;

-- Point symptom observations. symptom_key references definitions loosely so
-- logs survive definition edits.
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

CREATE INDEX idx_symptomlog_account_date ON symptom_logs(account_id, log_date);

-- Self-observed biomarkers (S07, S08): stored as recorded; probabilistic
-- signals, never certainties. v1 estimate engine does not consume them.
CREATE TABLE biomarker_observations (
    id              TEXT PRIMARY KEY,
    account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    observed_date   TEXT NOT NULL,
    bbt_celsius     REAL CHECK (bbt_celsius IS NULL OR bbt_celsius BETWEEN 34.0 AND 43.0),
    bbt_time        TEXT,
    bbt_quality     TEXT NOT NULL DEFAULT 'normal'
                    CHECK (bbt_quality IN ('normal', 'disturbed')),
    cervical_fluid  TEXT CHECK (cervical_fluid IS NULL OR cervical_fluid IN
                    ('none', 'sticky', 'creamy', 'watery', 'egg_white', 'unresolved')),
    cervix_position TEXT CHECK (cervix_position IS NULL OR cervix_position IN
                    ('low', 'medium', 'high', 'unknown')),
    cervix_firmness TEXT CHECK (cervix_firmness IS NULL OR cervix_firmness IN
                    ('firm', 'soft', 'unknown')),
    notes           TEXT NOT NULL DEFAULT '',
    client_rev      TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    deleted_at      TEXT
);

CREATE UNIQUE INDEX uq_biomarker_account_date_live
    ON biomarker_observations(account_id, observed_date) WHERE deleted_at IS NULL;

-- Medication and contraception context. Hormonal contraception changes
-- bleeding patterns; recorded as context, never used to infer anything.
CREATE TABLE medication_logs (
    id         TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    log_date   TEXT NOT NULL,
    taken_at   TEXT,
    name       TEXT NOT NULL,
    dose       TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL DEFAULT 'other'
               CHECK (kind IN ('contraceptive_hormonal', 'emergency_contraception',
                               'pain_relief', 'supplement', 'other')),
    notes      TEXT NOT NULL DEFAULT '',
    client_rev TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE INDEX idx_medication_account_date ON medication_logs(account_id, log_date);

-- Sync change log: per-account ordered snapshots for delta pull.
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

-- Duo: purpose-designed partner surface. Sharing is private by default,
-- explicit, granular, visible, and reversible.
CREATE TABLE duo_links (
    id                 TEXT PRIMARY KEY,
    owner_account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    partner_account_id TEXT REFERENCES accounts(id) ON DELETE CASCADE,
    code_hash          BLOB UNIQUE,
    status             TEXT NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'active', 'revoked')),
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    revoked_at         TEXT
);

CREATE INDEX idx_duo_links_owner ON duo_links(owner_account_id);
CREATE INDEX idx_duo_links_partner ON duo_links(partner_account_id);

CREATE TABLE duo_grants (
    id         TEXT PRIMARY KEY,
    link_id    TEXT NOT NULL REFERENCES duo_links(id) ON DELETE CASCADE,
    field      TEXT NOT NULL
               CHECK (field IN ('cycle_day', 'period_estimate', 'mood', 'energy',
                                'support_requests')),
    granted_at TEXT NOT NULL,
    revoked_at TEXT,
    UNIQUE (link_id, field)
);

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

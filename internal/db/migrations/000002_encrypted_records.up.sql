-- End-to-end encryption migration.
--
-- Record content is sealed on the client (AES-256-GCM under a key derived from
-- the account code) and is opaque to this server. See the client repository's
-- docs/architecture/E2EE_DESIGN.md for the key hierarchy and threat model.
--
-- The seven typed record tables are replaced by a single `records` table
-- holding ciphertext plus the routing metadata the server still needs: it must
-- target the upsert, evaluate the last-write-wins guard, and append the change
-- log, none of which it can do by reading the payload any more.
--
-- DESTRUCTIVE: drops the plaintext record tables. Safe only because there is no
-- production deployment yet.

-- Sealed records ------------------------------------------------------------

CREATE TABLE records (
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    entity_id   TEXT NOT NULL,
    entity_type TEXT NOT NULL
                CHECK (entity_type IN ('cycle', 'bleeding_observation', 'daily_entry',
                                       'symptom_definition', 'symptom_log',
                                       'biomarker_observation', 'medication_log')),
    -- Fresh per local edit; the documented tiebreak after updated_at.
    client_rev  TEXT NOT NULL,
    -- 0x01 || nonce(12) || AES-256-GCM(record JSON) || tag(16).
    -- NULL for tombstones: a delete carries no content.
    ciphertext  BLOB,
    deleted     INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0, 1)),
    -- RFC 3339 UTC, truncated to the minute by the client so a pushed batch
    -- does not reveal when each observation was entered.
    updated_at  TEXT NOT NULL,
    recorded_at TEXT NOT NULL,
    PRIMARY KEY (account_id, entity_id),
    CHECK (deleted = 1 OR ciphertext IS NOT NULL)
);

CREATE INDEX idx_records_account_type ON records(account_id, entity_type);

-- Drop plaintext record storage ---------------------------------------------

DROP TABLE IF EXISTS cycles;
DROP TABLE IF EXISTS bleeding_observations;
DROP TABLE IF EXISTS daily_entries;
DROP TABLE IF EXISTS symptom_definitions;
DROP TABLE IF EXISTS symptom_logs;
DROP TABLE IF EXISTS biomarker_observations;
DROP TABLE IF EXISTS medication_logs;

-- Change log now carries ciphertext ------------------------------------------
-- Rebuilt rather than altered: the payload column changes meaning (plaintext
-- JSON -> sealed bytes) and client_rev becomes required so a pulling client can
-- reconstruct the AEAD associated data.

DROP TABLE IF EXISTS sync_changes;

CREATE TABLE sync_changes (
    seq         INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL
                CHECK (entity_type IN ('cycle', 'bleeding_observation', 'daily_entry',
                                       'symptom_definition', 'symptom_log',
                                       'biomarker_observation', 'medication_log')),
    entity_id   TEXT NOT NULL,
    client_rev  TEXT NOT NULL,
    deleted     INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0, 1)),
    ciphertext  BLOB,
    updated_at  TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);

CREATE INDEX idx_sync_changes_pull ON sync_changes(account_id, seq);

-- Account settings ------------------------------------------------------------
-- life_stage and tracking_focus (pms, pmdd, endometriosis, pcos) are Art. 9
-- health data. Leaving them readable while sealing everything else would be an
-- obvious hole, and nothing server-side computes on them, so they are sealed
-- into one blob alongside the rest of the user's settings.

DROP TABLE IF EXISTS account_settings;

CREATE TABLE account_settings (
    account_id          TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    settings_ciphertext BLOB,
    updated_at          TEXT NOT NULL
);

-- Duo under end-to-end encryption ---------------------------------------------
--
-- The server can no longer compose a partner view: it cannot read cycle starts
-- or daily entries. Instead the tracker's device composes the shared payload,
-- applies the grants locally, seals it under the Duo link key, and pushes it.
-- The server relays ciphertext.
--
-- This is a stronger model than server-side filtering, not merely an equivalent
-- one: a grant now decides what is ever encrypted and transmitted, so the
-- server cannot leak what it never received.

-- No key material is stored here. The Duo link key is generated on the
-- tracker's device and travels to the partner in the pairing URL fragment,
-- which is never sent to a server (see the client's E2EE_DESIGN.md section 5).
-- A server-brokered key exchange was considered and rejected: it would let a
-- malicious operator substitute its own keys, and closing that needs the same
-- out-of-band channel the pairing link already provides.

CREATE TABLE duo_payloads (
    link_id    TEXT PRIMARY KEY REFERENCES duo_links(id) ON DELETE CASCADE,
    ciphertext BLOB NOT NULL,
    updated_at TEXT NOT NULL
);

-- Support messages are user-written text between partners and are sealed under
-- the same link key. kind and acknowledgement stay plaintext: they are routing
-- and UI state, not content.
DROP TABLE IF EXISTS support_requests;

CREATE TABLE support_requests (
    id                 TEXT PRIMARY KEY,
    link_id            TEXT NOT NULL REFERENCES duo_links(id) ON DELETE CASCADE,
    author_role        TEXT NOT NULL CHECK (author_role IN ('tracker', 'partner')),
    kind               TEXT NOT NULL DEFAULT 'general'
                       CHECK (kind IN ('general', 'comfort', 'practical', 'space')),
    message_ciphertext BLOB,
    created_at         TEXT NOT NULL,
    acknowledged_at    TEXT
);

CREATE INDEX idx_support_link ON support_requests(link_id, created_at);

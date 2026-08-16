# Data Model

The schema is defined by `internal/db/migrations/*.up.sql` and documented here with its research rationale (source IDs refer to `research/SOURCES.md`).

Storage notes: SQLite via `modernc.org/sqlite` (pure Go). Dates are ISO-8601 `TEXT` (`2026-07-21`); instants are RFC 3339 UTC `TEXT`. Booleans are `INTEGER` 0/1. Foreign keys and WAL journal mode are enforced by default.

---

## Identity & Settings

### accounts

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT PK` | Server-generated UUIDv7 |
| `code_hash` | `BLOB UNIQUE` | SHA-256 hash of the normalized 100-bit account code. The plaintext code is shown once and never stored |
| `status` | `TEXT` | `active | disabled | deleted` |
| `created_at` | `TEXT` | RFC 3339 UTC |
| `updated_at` | `TEXT` | RFC 3339 UTC |

### devices

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT PK` | UUIDv7 |
| `account_id` | `TEXT NOT NULL` | REFERENCES `accounts(id)` ON DELETE CASCADE |
| `name` | `TEXT NOT NULL` | Client-provided device label (e.g. "Pixel 9") |
| `token_hash` | `BLOB UNIQUE` | SHA-256 hash of the 256-bit bearer token |
| `created_at` | `TEXT` | RFC 3339 UTC |
| `last_seen_at`| `TEXT` | Best-effort timestamp updated on authenticated requests |
| `revoked_at` | `TEXT` | Setting this disables the device token |

### account_settings

Settings contain Article 9 health data (life stage STRAW+10 enum [S11], tracking focuses [S20-S25]) and are sealed client-side:

| Column | Type | Notes |
|---|---|---|
| `account_id` | `TEXT PK` | REFERENCES `accounts(id)` ON DELETE CASCADE |
| `settings_ciphertext` | `BLOB` | AES-256-GCM sealed settings JSON |
| `updated_at` | `TEXT` | RFC 3339 UTC |

---

## Synchronized Sealed Records

Under end-to-end encryption, the server stores sealed record blobs alongside plaintext routing metadata.

### records

Current state of each synchronized entity per account:

| Column | Type | Notes |
|---|---|---|
| `account_id` | `TEXT NOT NULL` | REFERENCES `accounts(id)` ON DELETE CASCADE |
| `entity_id` | `TEXT NOT NULL` | Client-generated UUIDv7 |
| `entity_type` | `TEXT NOT NULL` | `cycle | bleeding_observation | daily_entry | symptom_definition | symptom_log | biomarker_observation | medication_log` |
| `client_rev` | `TEXT NOT NULL` | Fresh UUID per edit (LWW tiebreak) |
| `ciphertext` | `BLOB` | `0x01 \|\| nonce(12) \|\| AES-256-GCM \|\| tag(16)` (NULL for tombstones) |
| `deleted` | `INTEGER NOT NULL` | 0 for active records, 1 for tombstones |
| `updated_at` | `TEXT NOT NULL` | RFC 3339 UTC instant (Last-Write-Wins authority) |
| `recorded_at` | `TEXT NOT NULL` | Server-recorded timestamp |

PRIMARY KEY: `(account_id, entity_id)`

### sync_changes

Append-only change log for delta synchronization:

| Column | Type | Notes |
|---|---|---|
| `seq` | `INTEGER PK AUTOINCREMENT` | Monotonic ordering key per account |
| `account_id` | `TEXT NOT NULL` | REFERENCES `accounts(id)` ON DELETE CASCADE |
| `entity_type` | `TEXT NOT NULL` | Entity type enum |
| `entity_id` | `TEXT NOT NULL` | Entity UUID |
| `client_rev` | `TEXT NOT NULL` | Revision identifier |
| `deleted` | `INTEGER NOT NULL` | 0 or 1 |
| `ciphertext` | `BLOB` | Sealed record bytes (NULL for tombstones) |
| `updated_at` | `TEXT NOT NULL` | Client timestamp |
| `recorded_at` | `TEXT NOT NULL` | Server timestamp |

---

## Client Record Schemas (Sealed Payloads)

The plaintext format inside `ciphertext` is shared across client versions and specified in `openapi/openapi.yaml`:

- **`CycleData`:** Cycle starts (`start_date` = first day of full menstrual flow [S01]), `bleeding_days`, plausibility bounds (10-200 days [S03, S06]), certainty and source flags.
- **`BleedingObservationData`:** FIGO-aligned flow ratings (`none | spotting | light | medium | heavy`), intermenstrual bleeding flag [S01, S02].
- **`DailyEntryData`:** Prospective daily tracking: date-keyed `pain_level`, `mood_level`, `energy_level` (1-5), notes [S25].
- **`SymptomDefinitionData` & `SymptomLogData`:** Built-in and custom symptom catalog and timestamped point observations [S20-S22].
- **`BiomarkerObservationData`:** BBT in Celsius (34-43C plausibility), cervical fluid characteristics, cervix position/firmness [S07, S08].
- **`MedicationLogData`:** Contraceptives, pain relief, and supplements.

---

## Duo Companion

### duo_links

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT PK` | UUIDv7 |
| `owner_account_id` | `TEXT NOT NULL` | REFERENCES `accounts(id)` ON DELETE CASCADE |
| `partner_account_id` | `TEXT` | REFERENCES `accounts(id)` ON DELETE CASCADE (NULL while pending) |
| `code_hash` | `BLOB` | SHA-256 of the 50-bit pairing code (cleared on acceptance) |
| `status` | `TEXT NOT NULL` | `pending | active | revoked` |
| `created_at` | `TEXT` | RFC 3339 UTC |
| `updated_at` | `TEXT` | RFC 3339 UTC |
| `revoked_at` | `TEXT` | Timestamp when link was revoked |

### duo_grants

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT PK` | UUIDv7 |
| `link_id` | `TEXT NOT NULL` | REFERENCES `duo_links(id)` ON DELETE CASCADE |
| `field` | `TEXT NOT NULL` | `cycle_day | period_estimate | mood | energy | support_requests` |
| `granted_at` | `TEXT NOT NULL` | RFC 3339 UTC |
| `revoked_at` | `TEXT` | NULL when grant is active |

### duo_payloads

Stores the latest sealed partner projection published by the tracker:

| Column | Type | Notes |
|---|---|---|
| `link_id` | `TEXT PK` | REFERENCES `duo_links(id)` ON DELETE CASCADE |
| `ciphertext` | `BLOB NOT NULL` | Sealed under the Duo link key by the tracker's device |
| `updated_at` | `TEXT NOT NULL` | RFC 3339 UTC |

### support_requests

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT PK` | UUIDv7 |
| `link_id` | `TEXT NOT NULL` | REFERENCES `duo_links(id)` ON DELETE CASCADE |
| `author_role` | `TEXT NOT NULL` | `tracker | partner` |
| `kind` | `TEXT NOT NULL` | `general | comfort | practical | space` |
| `message_ciphertext` | `BLOB` | Sealed under the Duo link key |
| `created_at` | `TEXT NOT NULL` | RFC 3339 UTC |
| `acknowledged_at` | `TEXT` | RFC 3339 UTC |

# Data Model

The schema is the canonical contract. It is defined by
`internal/db/migrations/*.up.sql` and documented here with its research
rationale (source IDs refer to `research/SOURCES.md`).

Storage notes: SQLite via modernc.org/sqlite. Dates are ISO-8601 `TEXT`
(`2026-07-21`); instants are RFC 3339 UTC `TEXT`. Booleans are `INTEGER`
0/1. JSON arrays are `TEXT`. All tables use `TEXT` UUIDv7 primary keys;
synchronized records are client-ID'd so offline devices never coordinate ID
generation.

## Identity

### accounts

| Column    | Notes                                                        |
|-----------|--------------------------------------------------------------|
| id        | server-generated UUIDv7                                      |
| code_hash | SHA-256 of the normalized account code; UNIQUE. The code itself is never stored (Mullvad-style anonymous auth) |
| status    | `active | disabled | deleted`                                |

### devices

One row per registered device. `token_hash` is SHA-256 of the 256-bit bearer
token. `revoked_at` disables the token without deleting audit context.

### account_settings

Server-authoritative (not in the sync log; refreshed by clients on pull).

- `locale` default `fr`, `time_zone` default `Europe/Paris`.
- `life_stage`: STRAW+10-aligned enum (S11). User-selected, never inferred.
- `tracking_focus`: JSON array of `pms | pmdd | endometriosis | pcos |
  custom` - user-selected charting focuses, never diagnoses (S20-S25).

## Synchronized records

Every synchronized table shares the envelope:

```
id          TEXT PK          client-generated UUIDv7
account_id  TEXT NOT NULL    owner; CASCADE delete
client_rev  TEXT NOT NULL    new UUID per local edit (conflict tiebreak)
created_at  TEXT NOT NULL    client-declared RFC 3339
updated_at  TEXT NOT NULL    client-declared RFC 3339 (LWW authority)
deleted_at  TEXT             tombstone; NULL = live
```

Writes apply inside a transaction: entity upsert (LWW-guarded) + append to
`sync_changes` (JSON payload snapshot, per-account monotonic `seq`). Pulls
read the change log; structured tables serve validation, read models, and
estimates. This is why the server can be the source of truth while clients
sync offline-first.

### cycles

The backbone. `start_date` is **cycle day 1 = first day of full menstrual
flow**; preceding spotting does not start a cycle (S01).

- `end_date` nullable: the current cycle is open. Irregular, long, or skipped
  cycles are valid data, not errors (S05, S11).
- `length_days` bounds 10-200: permissive plausibility, not a normative
  24-38 filter; normative ranges belong to interpretation, not storage
  (S03, S06).
- `certainty` (`recorded | uncertain | estimated`) and `source`
  (`manual | import | estimated`) keep facts and their provenance explicit.
- Unique live `(account_id, start_date)`: two cycles cannot start the same
  day; tombstones are excluded from the constraint.

### bleeding_observations (one per day)

- `flow`: `none | spotting | light | medium | heavy` - self-rated; FIGO
  aligns "spotting" with intermenstrual bleeding, captured explicitly by the
  `intermenstrual` flag (S01, S02).
- `product_count` optional, bounded: supports personal patterns, not volume
  diagnosis (S23).
- No cause field, no PALM-COEIN taxonomy: bleeding is a neutral observation
  (S02). Unique live `(account_id, observed_date)`.

### daily_entries (one per day)

Prospective daily charting: `pain_level`, `mood_level`, `energy_level`
(1-5, nullable = not recorded), free-text `notes`. This date-keyed,
severity-scaled structure is what prospective charting requires (S25) and
what condition-aware tracking uses without encoding condition logic.
Unique live `(account_id, entry_date)`.

### symptom_definitions

Per-account symptom catalog: stable `key`, French-default `label`,
`category` (`mood | physical | energy | pain | cervical_fluid | other`),
`builtin` flag, `active` flag. Accounts are seeded with a built-in set
mirroring the Android client's defaults; users may add custom symptoms
(S20-S22 enable personalized tracking without server-side assertions).
Clients must adopt the server-seeded built-ins (matched by `key`) rather
than creating their own rows for them, so all devices converge on one
catalog; the unique live `(account_id, key)` index enforces this.

### symptom_logs

Point observations: `log_date`, `logged_at`, `symptom_key` (loose reference
to a definition key so logs survive definition edits), `severity` 1-5.

### biomarker_observations (one per day, optional)

Self-observed fertility-aware biomarkers, stored as recorded:

- `bbt_celsius` (34-43 plausibility), `bbt_time`, `bbt_quality`
  (`normal | disturbed` - sleep disruption etc. invalidates interpretation).
- `cervical_fluid`: `none | sticky | creamy | watery | egg_white | unresolved`.
- `cervix_position` (`low | medium | high | unknown`), `cervix_firmness`
  (`firm | soft | unknown`).

These are retrospective/probabilistic signals (S07, S08). v1 stores and
serves them; the estimate engine does not consume them. Any future use must
remain probabilistic and labeled. Unique live `(account_id, observed_date)`.

### medication_logs

`log_date`, optional `taken_at`, `name`, `dose`, `kind`
(`contraceptive_hormonal | emergency_contraception | pain_relief |
supplement | other`). Hormonal contraception is tracked because it changes
bleeding patterns and cycle interpretability - recorded as context, never
used to infer anything about the user.

### sync_changes

Per-account ordered change log: `seq` (AUTOINCREMENT), `entity_type`
(CHECK-constrained enum), `entity_id`, `deleted`, `payload` (JSON snapshot;
NULL when deleted), client `updated_at`, `recorded_at`. Pulls are
`seq`-range scans; retention/compaction is future work.

## Duo

### duo_links

`owner_account_id`, nullable `partner_account_id` (NULL while pending),
`code_hash` (SHA-256 of the short pairing code; NULL after acceptance),
`status` (`pending | active | revoked`).

### duo_grants

One row per `(link_id, field)`; `granted_at` / `revoked_at` make revocation
an explicit, observable event rather than a boolean flip. Fields:
`cycle_day | period_estimate | mood | energy | support_requests`.

### support_requests

`author_role` (`tracker | partner`), `kind` (`general | comfort | practical |
space`), optional `message`, `acknowledged_at`. Visible to link members only.

## Estimates: deliberately not stored (v1)

Predictions are computed on demand from live cycles (`cyclecalc`) and served
with method, basis, windows, and confidence. Storing them would risk stale
estimates masquerading as facts and would blur the observations/estimates
boundary. If persistence becomes necessary (audit, history), it gets a
dedicated `estimates` table with `generated_at`, `method`, `superseded_at`,
and is never joined into observation reads.

## Deletion semantics

- Record delete = tombstone (`deleted_at` set, replicated via the change
  log) so other devices converge.
- Account deletion cascades everything; a future compaction job can hard
  delete tombstones older than a retention window.
- Duo revocation stops data flow immediately; it never deletes the tracker's
  history and never retroactively exposes anything.

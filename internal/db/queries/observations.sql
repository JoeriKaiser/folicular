-- Bleeding observations -----------------------------------------------

-- name: UpsertBleedingObservation :execrows
INSERT INTO bleeding_observations (
    id, account_id, observed_date, flow, intermenstrual, product_count,
    notes, client_rev, created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    observed_date  = excluded.observed_date,
    flow           = excluded.flow,
    intermenstrual = excluded.intermenstrual,
    product_count  = excluded.product_count,
    notes          = excluded.notes,
    client_rev     = excluded.client_rev,
    created_at     = excluded.created_at,
    updated_at     = excluded.updated_at,
    deleted_at     = excluded.deleted_at
WHERE excluded.updated_at > bleeding_observations.updated_at
   OR (excluded.updated_at = bleeding_observations.updated_at
       AND excluded.client_rev > bleeding_observations.client_rev);

-- name: GetBleedingObservationByID :one
SELECT * FROM bleeding_observations WHERE id = ? AND account_id = ?;

-- name: ListBleedingObservationsByRange :many
SELECT * FROM bleeding_observations
WHERE account_id = ? AND deleted_at IS NULL
  AND observed_date >= ? AND observed_date <= ?
ORDER BY observed_date;

-- Daily entries --------------------------------------------------------

-- name: UpsertDailyEntry :execrows
INSERT INTO daily_entries (
    id, account_id, entry_date, pain_level, mood_level, energy_level,
    notes, client_rev, created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    entry_date   = excluded.entry_date,
    pain_level   = excluded.pain_level,
    mood_level   = excluded.mood_level,
    energy_level = excluded.energy_level,
    notes        = excluded.notes,
    client_rev   = excluded.client_rev,
    created_at   = excluded.created_at,
    updated_at   = excluded.updated_at,
    deleted_at   = excluded.deleted_at
WHERE excluded.updated_at > daily_entries.updated_at
   OR (excluded.updated_at = daily_entries.updated_at
       AND excluded.client_rev > daily_entries.client_rev);

-- name: GetDailyEntryByID :one
SELECT * FROM daily_entries WHERE id = ? AND account_id = ?;

-- name: ListDailyEntriesByRange :many
SELECT * FROM daily_entries
WHERE account_id = ? AND deleted_at IS NULL
  AND entry_date >= ? AND entry_date <= ?
ORDER BY entry_date;

-- Symptom definitions ----------------------------------------------------

-- name: UpsertSymptomDefinition :execrows
INSERT INTO symptom_definitions (
    id, account_id, key, label, category, builtin, active,
    client_rev, created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    key        = excluded.key,
    label      = excluded.label,
    category   = excluded.category,
    builtin    = excluded.builtin,
    active     = excluded.active,
    client_rev = excluded.client_rev,
    created_at = excluded.created_at,
    updated_at = excluded.updated_at,
    deleted_at = excluded.deleted_at
WHERE excluded.updated_at > symptom_definitions.updated_at
   OR (excluded.updated_at = symptom_definitions.updated_at
       AND excluded.client_rev > symptom_definitions.client_rev);

-- name: GetSymptomDefinitionByID :one
SELECT * FROM symptom_definitions WHERE id = ? AND account_id = ?;

-- name: ListSymptomDefinitions :many
SELECT * FROM symptom_definitions
WHERE account_id = ? AND deleted_at IS NULL
ORDER BY builtin DESC, key;

-- Symptom logs ------------------------------------------------------------

-- name: UpsertSymptomLog :execrows
INSERT INTO symptom_logs (
    id, account_id, log_date, logged_at, symptom_key, severity,
    notes, client_rev, created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    log_date    = excluded.log_date,
    logged_at   = excluded.logged_at,
    symptom_key = excluded.symptom_key,
    severity    = excluded.severity,
    notes       = excluded.notes,
    client_rev  = excluded.client_rev,
    created_at  = excluded.created_at,
    updated_at  = excluded.updated_at,
    deleted_at  = excluded.deleted_at
WHERE excluded.updated_at > symptom_logs.updated_at
   OR (excluded.updated_at = symptom_logs.updated_at
       AND excluded.client_rev > symptom_logs.client_rev);

-- name: GetSymptomLogByID :one
SELECT * FROM symptom_logs WHERE id = ? AND account_id = ?;

-- name: ListSymptomLogsByRange :many
SELECT * FROM symptom_logs
WHERE account_id = ? AND deleted_at IS NULL
  AND log_date >= ? AND log_date <= ?
ORDER BY log_date, logged_at;

-- Biomarker observations ----------------------------------------------------

-- name: UpsertBiomarkerObservation :execrows
INSERT INTO biomarker_observations (
    id, account_id, observed_date, bbt_celsius, bbt_time, bbt_quality,
    cervical_fluid, cervix_position, cervix_firmness,
    notes, client_rev, created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    observed_date   = excluded.observed_date,
    bbt_celsius     = excluded.bbt_celsius,
    bbt_time        = excluded.bbt_time,
    bbt_quality     = excluded.bbt_quality,
    cervical_fluid  = excluded.cervical_fluid,
    cervix_position = excluded.cervix_position,
    cervix_firmness = excluded.cervix_firmness,
    notes           = excluded.notes,
    client_rev      = excluded.client_rev,
    created_at      = excluded.created_at,
    updated_at      = excluded.updated_at,
    deleted_at      = excluded.deleted_at
WHERE excluded.updated_at > biomarker_observations.updated_at
   OR (excluded.updated_at = biomarker_observations.updated_at
       AND excluded.client_rev > biomarker_observations.client_rev);

-- name: GetBiomarkerObservationByID :one
SELECT * FROM biomarker_observations WHERE id = ? AND account_id = ?;

-- name: ListBiomarkerObservationsByRange :many
SELECT * FROM biomarker_observations
WHERE account_id = ? AND deleted_at IS NULL
  AND observed_date >= ? AND observed_date <= ?
ORDER BY observed_date;

-- Medication logs ---------------------------------------------------------

-- name: UpsertMedicationLog :execrows
INSERT INTO medication_logs (
    id, account_id, log_date, taken_at, name, dose, kind,
    notes, client_rev, created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    log_date   = excluded.log_date,
    taken_at   = excluded.taken_at,
    name       = excluded.name,
    dose       = excluded.dose,
    kind       = excluded.kind,
    notes      = excluded.notes,
    client_rev = excluded.client_rev,
    created_at = excluded.created_at,
    updated_at = excluded.updated_at,
    deleted_at = excluded.deleted_at
WHERE excluded.updated_at > medication_logs.updated_at
   OR (excluded.updated_at = medication_logs.updated_at
       AND excluded.client_rev > medication_logs.client_rev);

-- name: GetMedicationLogByID :one
SELECT * FROM medication_logs WHERE id = ? AND account_id = ?;

-- name: ListMedicationLogsByRange :many
SELECT * FROM medication_logs
WHERE account_id = ? AND deleted_at IS NULL
  AND log_date >= ? AND log_date <= ?
ORDER BY log_date, taken_at;

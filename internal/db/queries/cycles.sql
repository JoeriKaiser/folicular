-- Entity-level last-write-wins: the update applies only when the incoming
-- record is newer (updated_at, then client_rev). Affected rows = 0 means the
-- pushed record lost; the handler returns the server state as a conflict.

-- name: UpsertCycle :execrows
INSERT INTO cycles (
    id, account_id, start_date, end_date, length_days, bleeding_days,
    certainty, source, notes, client_rev, created_at, updated_at, deleted_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(id) DO UPDATE SET
    start_date    = excluded.start_date,
    end_date      = excluded.end_date,
    length_days   = excluded.length_days,
    bleeding_days = excluded.bleeding_days,
    certainty     = excluded.certainty,
    source        = excluded.source,
    notes         = excluded.notes,
    client_rev    = excluded.client_rev,
    created_at    = excluded.created_at,
    updated_at    = excluded.updated_at,
    deleted_at    = excluded.deleted_at
WHERE excluded.updated_at > cycles.updated_at
   OR (excluded.updated_at = cycles.updated_at AND excluded.client_rev > cycles.client_rev);

-- name: GetCycleByID :one
SELECT * FROM cycles WHERE id = ? AND account_id = ?;

-- name: ListCyclesByRange :many
SELECT * FROM cycles
WHERE account_id = ? AND deleted_at IS NULL
  AND start_date >= ? AND start_date <= ?
ORDER BY start_date DESC;

-- name: ListCycleStarts :many
SELECT start_date FROM cycles
WHERE account_id = ? AND deleted_at IS NULL
ORDER BY start_date;

-- name: GetLatestCycleStart :one
SELECT start_date FROM cycles
WHERE account_id = ? AND deleted_at IS NULL
ORDER BY start_date DESC
LIMIT 1;

-- Sealed record storage. Content is opaque to this server; only the routing
-- metadata below is readable.
--
-- Entity-level last-write-wins: the update applies only when the incoming
-- record is newer (updated_at, then client_rev). Affected rows = 0 means the
-- pushed record lost; the handler returns the server state as a conflict.

-- name: UpsertRecord :execrows
INSERT INTO records (
    account_id, entity_id, entity_type, client_rev, ciphertext,
    deleted, updated_at, recorded_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(account_id, entity_id) DO UPDATE SET
    entity_type = excluded.entity_type,
    client_rev  = excluded.client_rev,
    ciphertext  = excluded.ciphertext,
    deleted     = excluded.deleted,
    updated_at  = excluded.updated_at,
    recorded_at = excluded.recorded_at
WHERE excluded.updated_at > records.updated_at
   OR (excluded.updated_at = records.updated_at AND excluded.client_rev > records.client_rev);

-- name: GetRecord :one
SELECT * FROM records WHERE account_id = ? AND entity_id = ?;

-- name: ListRecordsForAccount :many
SELECT * FROM records
WHERE account_id = ?
ORDER BY entity_type, entity_id;

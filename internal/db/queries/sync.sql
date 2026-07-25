-- name: RecordChange :one
INSERT INTO sync_changes (
    account_id, entity_type, entity_id, client_rev, deleted, ciphertext,
    updated_at, recorded_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING seq;

-- name: PullChanges :many
SELECT seq, entity_type, entity_id, client_rev, deleted, ciphertext, updated_at, recorded_at
FROM sync_changes
WHERE account_id = ? AND seq > ?
ORDER BY seq
LIMIT ?;

-- name: LatestCursor :one
SELECT seq FROM sync_changes WHERE account_id = ? ORDER BY seq DESC LIMIT 1;

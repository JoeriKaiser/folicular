-- name: InsertDevice :exec
INSERT INTO devices (id, account_id, name, token_hash, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetDeviceByTokenHash :one
SELECT devices.*, accounts.status AS account_status
FROM devices
JOIN accounts ON accounts.id = devices.account_id
WHERE devices.token_hash = ?;

-- name: TouchDevice :exec
UPDATE devices SET last_seen_at = ? WHERE id = ?;

-- name: ListDevicesByAccount :many
SELECT * FROM devices
WHERE account_id = ?
ORDER BY created_at;

-- name: RevokeDevice :execrows
UPDATE devices SET revoked_at = ? WHERE id = ? AND account_id = ?;

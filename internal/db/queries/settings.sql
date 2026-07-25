-- name: InsertDefaultSettings :exec
INSERT INTO account_settings (account_id, updated_at)
VALUES (?, ?);

-- name: GetSettings :one
SELECT * FROM account_settings WHERE account_id = ?;

-- name: UpdateSettings :exec
UPDATE account_settings
SET locale = ?, time_zone = ?, life_stage = ?, tracking_focus = ?, updated_at = ?
WHERE account_id = ?;

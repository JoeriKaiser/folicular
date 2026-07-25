-- Settings content (life stage, tracking focuses) is Art. 9 health data and is
-- sealed client-side like every other record.

-- name: InsertDefaultSettings :exec
INSERT INTO account_settings (account_id, updated_at)
VALUES (?, ?);

-- name: GetSettings :one
SELECT * FROM account_settings WHERE account_id = ?;

-- name: UpdateSettings :exec
UPDATE account_settings
SET settings_ciphertext = ?, updated_at = ?
WHERE account_id = ?;

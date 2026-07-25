-- name: InsertAccount :exec
INSERT INTO accounts (id, code_hash, status, duo_public_key, created_at, updated_at)
VALUES (?, ?, 'active', ?, ?, ?);

-- name: SetDuoPublicKey :exec
UPDATE accounts SET duo_public_key = ?, updated_at = ? WHERE id = ?;

-- name: GetAccountByID :one
SELECT * FROM accounts WHERE id = ?;

-- name: GetAccountByCodeHash :one
SELECT * FROM accounts WHERE code_hash = ?;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = ?;

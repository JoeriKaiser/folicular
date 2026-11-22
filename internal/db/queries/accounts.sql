-- name: InsertAccount :exec
INSERT INTO accounts (id, code_hash, auth_version, status, created_at, updated_at)
VALUES (?, ?, ?, 'active', ?, ?);

-- name: GetAccountByID :one
SELECT * FROM accounts WHERE id = ?;

-- name: GetAccountByCodeHash :one
SELECT * FROM accounts WHERE code_hash = ?;

-- name: UpdateAccountAuth :exec
UPDATE accounts
SET code_hash = ?, auth_version = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = ?;

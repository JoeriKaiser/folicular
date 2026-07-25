-- Duo queries. Designed in docs/api.md; handlers land after the core sync
-- path is proven.

-- name: InsertDuoLink :exec
INSERT INTO duo_links (
    id, owner_account_id, partner_account_id, code_hash, status, created_at, updated_at
) VALUES (?, ?, ?, ?, 'pending', ?, ?);

-- name: GetDuoLinkByID :one
SELECT * FROM duo_links WHERE id = ?;

-- name: GetPendingDuoLinkByCodeHash :one
SELECT * FROM duo_links
WHERE code_hash = ? AND status = 'pending';

-- name: AcceptDuoLink :exec
UPDATE duo_links
SET partner_account_id = ?, status = 'active', code_hash = NULL, updated_at = ?
WHERE id = ? AND status = 'pending';

-- name: ListDuoLinksForAccount :many
SELECT * FROM duo_links
WHERE owner_account_id = ? OR partner_account_id = ?
ORDER BY created_at DESC;

-- name: GetActiveDuoLinkByOwner :one
SELECT * FROM duo_links
WHERE owner_account_id = ? AND status = 'active';

-- name: GetActiveDuoLinkByPartner :one
SELECT * FROM duo_links
WHERE partner_account_id = ? AND status = 'active';

-- name: RevokeDuoLink :execrows
UPDATE duo_links
SET status = 'revoked', revoked_at = ?, updated_at = ?
WHERE id = ? AND status IN ('active', 'pending');

-- name: UpsertGrant :exec
INSERT INTO duo_grants (id, link_id, field, granted_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(link_id, field) DO UPDATE SET
    granted_at = excluded.granted_at,
    revoked_at = NULL;

-- name: RevokeGrant :exec
UPDATE duo_grants SET revoked_at = ? WHERE link_id = ? AND field = ?;

-- name: ListActiveGrantsByLink :many
SELECT * FROM duo_grants
WHERE link_id = ? AND revoked_at IS NULL;

-- name: InsertSupportRequest :exec
INSERT INTO support_requests (id, link_id, author_role, kind, message, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetSupportRequestByID :one
SELECT * FROM support_requests WHERE id = ?;

-- name: AckSupportRequest :exec
UPDATE support_requests SET acknowledged_at = ? WHERE id = ?;

-- name: ListSupportRequestsByLink :many
SELECT * FROM support_requests
WHERE link_id = ?
ORDER BY created_at DESC
LIMIT ?;

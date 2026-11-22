-- Track authentication and encryption protocol version per account.
-- 1 = legacy server-minted code (LTL-..., envelope 0x01)
-- 2 = client-derived operator-blind key (LTL2-..., envelope 0x02)
ALTER TABLE accounts
    ADD COLUMN auth_version INTEGER NOT NULL DEFAULT 1
    CHECK (auth_version IN (1, 2));

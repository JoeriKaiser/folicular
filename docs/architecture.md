# Architecture

## Position

folicular provides **anonymous account identity, transport-ordered delta synchronization, and consensual Duo companion relay**. Under end-to-end encryption, health records and account settings are sealed on the client (AES-256-GCM); the server acts as an authoritative conflict arbiter and ordering service for opaque ciphertext envelopes.

The Android client (package `fr.luteal`) is offline-first for display and local writes (using Room as its local store), derives encryption keys from the account code, and computes cycle estimates entirely on-device.

## Runtime Shape

Single Go binary, single SQLite file. No external services, no queues, no third parties. SQLite via `modernc.org/sqlite` (pure Go, no CGO) is ideal for this product's operational model and makes self-hosting effortless. The binary embeds migrations and applies them automatically at boot.

```
Android client (Room, offline-first, local E2EE keys)
        |  HTTPS, JSON, Bearer device token
        v
   chi router -- middleware: recover, slog, auth, rate limit
        |
   api handlers -- parse --> envelope validation --> sqlc queries
        |
   SQLite (modernc.org/sqlite, WAL, foreign_keys ON)
```

## Authentication: Anonymous, Mullvad-Style

No email, OAuth, phone number, or password.

- **Account code:** 100 bits of `crypto/rand` entropy, Crockford base32, displayed as `LTL-XXXXX-XXXXX-XXXXX-XXXXX`. It is shown **once** at registration; only its SHA-256 hash is stored.
- **Device tokens:** each device registers against the account code and receives a 256-bit bearer token (stored hashed). Tokens are revocable individually.
- **Abuse control:** registration and code-based endpoints are rate limited per client IP using an in-memory token bucket keyed by HMAC-hashed client IPs with an ephemeral per-process random pepper. Client IPs never touch permanent storage or logs.

## Synchronization and End-to-End Encryption

Record content is sealed on the client and opaque to this server.

### Record Envelope
Every synchronized record carries an unsealed envelope for routing and conflict detection:
- `entity_type`: enum (`cycle`, `bleeding_observation`, `daily_entry`, `symptom_definition`, `symptom_log`, `biomarker_observation`, `medication_log`).
- `entity_id`: client-generated UUIDv7.
- `client_rev`: fresh UUID per local edit (tiebreak).
- `updated_at`: RFC 3339 UTC instant (authority for Last-Write-Wins).
- `deleted`: boolean tombstone flag.
- `ciphertext`: sealed payload bytes (`0x01 || nonce(12) || AES-256-GCM || tag(16)`).

### Conflict Resolution
- Writes apply inside an atomic transaction: record upsert with last-write-wins guard (`updated_at`, then `client_rev`) plus append to `sync_changes`.
- Losing records are returned to the client in `conflicts` carrying the server's current sealed state. The client decrypts both states and performs domain-level reconciliation.

## Duo Companion

Purpose-designed partner surface:
- **Pairing:** Short-lived, single-use pairing code (50 bits, 7-day TTL) transported as text, deep link, or QR code rendered by the client from `pairing_url`.
- **Granular Grants:** Tracker configures per-field grants (`cycle_day | period_estimate | mood | energy | support_requests`).
- **Sealed Projections:** The tracker's device generates the shared view, applies active grants locally, seals the projection under the Duo link key, and publishes it via `PUT /v1/duo/payload`. The server relays the ciphertext to the partner.
- **Support Requests:** Either partner may send a support message sealed under the Duo link key; the recipient acknowledges it.

## Estimates and Medical Posture

All cycle calculations and estimates are computed strictly on-device on the client. The backend never performs screening, inference, or clinical assertions.

## Storage Lifecycle and Compaction

- **Deletions:** Replicated via tombstones (`deleted = 1, ciphertext = NULL`) through `sync_changes`.
- **Account Deletion:** Cascades across all associated tables with `ON DELETE CASCADE` (GDPR Article 17).
- **Log Compaction:** `sync_changes` is an append-only change log. In high-frequency multi-device setups, historical changes can be pruned up to a safe client cursor horizon, keeping SQLite storage compact for homelab deployments.

## Observability and Ops

- Structured JSON logs via `slog` with request IDs; zero PII (no tokens, hashes, or bodies).
- Health probes: `/healthz` (liveness), `/readyz` (database connectivity), `/version`.
- Environment-only configuration with safe local defaults.

# Architecture

## Position

folicular is the **source of truth for synchronized Luteal data**: canonical
schema, validation, conflict resolution, and estimates. The Android client is
offline-first for display (Room is its local cache) but conforms to this
service's contract and accepts its resolved state on conflict.

## Runtime shape

Single Go binary, single SQLite file. No external services, no queues, no
third parties. SQLite is adequate for this product's scale (one row per user
per day at most) and keeps self-hosting trivial. The binary embeds migrations
and applies them at boot.

```
Android client (Room, offline-first)
        |  HTTPS, JSON, Bearer device token
        v
  chi router -- middleware: recover, slog, auth, rate limit
        |
   api handlers -- parse --> domain (validate) --> sqlc queries
        |
   SQLite (modernc.org/sqlite, WAL, foreign_keys ON)
```

## Authentication: anonymous, Mullvad-style

No email, OAuth, phone, or password.

- **Account code:** 100 bits of `crypto/rand`, Crockford base32, displayed as
  `LTL-XXXXX-XXXXX-XXXXX-XXXXX`. It is the account credential and is shown
  **once** at registration; only its SHA-256 hash is stored. Losing it means
  losing the account (recovery is a future, explicitly-designed feature).
- **Device tokens:** each device registers against the account code and
  receives a 256-bit bearer token (stored hashed). Tokens are revocable
  individually. The account code itself is never sent again after a device is
  registered, except to add more devices.
- **Abuse control:** registration and code-based endpoints are rate limited
  per client IP; token lookups are constant-work SHA-256 comparisons.

Security note: the account code is a bearer credential for a health-data
service. This is an accepted Mullvad-style trade-off (memorable-ish, no PII),
mitigated by high entropy, rate limiting, per-device revocation, and
encryption at rest in deployment. It is not anonymous against a compromised
server; see the encryption decision below.

## Synchronization model

Offline-first delta sync over a server-ordered change log:

- Every synchronized record carries an **envelope**: client-generated UUID
  `id`, `client_rev` (new per local edit), client-declared `created_at` /
  `updated_at` (RFC 3339), and `deleted_at` tombstone.
- **Push:** client sends changed records; the server validates each against
  `internal/domain`, applies it with a last-write-wins guard
  (`updated_at`, then `client_rev`), and appends to `sync_changes` with a JSON
  payload snapshot. Records that lose the LWW guard are returned to the client
  as `conflicts` carrying the server's current state - nothing is silently
  lost.
- **Pull:** client sends its cursor (last seen `seq`); the server returns
  ordered change rows (including tombstones) per account.
- Server `seq` is transport ordering only. Domain truth is the validated
  record content.

Known limitation (v1, documented in `api.md`): LWW is entity-level. Field
-level merging and vector-clock-style conflict detection are future work,
tracked against the client's `SYNC_BOUNDARY.md` requirements.

## Estimates, not predictions

`internal/cyclecalc` computes ranges from the account's own recorded cycle
starts: next-menstruation window, ovulation window anchored backward by a
luteal constant (S07), fertile window. Outputs always carry `method`,
`confidence` (`insufficient | low | moderate`), the cycle count and
variability they were based on, and French disclaimer copy. Estimates are
computed on demand from facts; they are never stored as facts. See
`research/03-phases-and-ovulation.md`.

## Duo

Purpose-designed partner surface, not a clone. Pairing via a short-lived,
single-use code (50 bits, 7-day expiry) transported as a typed value, a
shareable link, or a QR code rendered by the client from `pairing_url`;
per-field grants (`cycle_day | period_estimate | mood | energy |
support_requests`); the partner endpoint returns a projection built strictly
from active grants, and the tracker sees the support thread. Revocation is
immediately observable by the partner. Implemented in v1 and covered by the
smoke test.

## Encryption decision (v1: structured storage; E2EE is a future protocol project)

The client's `SYNC_BOUNDARY.md` forbids claiming E2EE before threat model,
key lifecycle, and rollback protection are designed. Two viable paths were
weighed:

1. **Structured storage (chosen for v1):** server validates and stores
   domain records; TLS in transit; encryption at rest is a deployment
   requirement (S30). Enables server-side validation, conflict resolution on
   content, and server-computed estimates - i.e. the "backend is the source
   of truth for data" principle in full.
2. **E2EE envelope storage:** server stores encrypted blobs per record;
   validation and estimates move client-side; "source of truth" reduces to
   ordering and access control; account recovery becomes a key-recovery
   ceremony; multi-device and Duo need key-distribution protocols.

Decision: **path 1 for v1.** Rationale: the source-of-truth principle
requires a server that can read and validate; true E2EE is a protocol
engineering project (threat model, key generation/storage, device
authorization, rotation, recovery, rollback protection, independent review -
the full `SYNC_BOUNDARY.md` checklist) where a bug is silent and
catastrophic; and anonymous accounts plus minimization already remove the
identity-linkage risk. If the threat model later requires "unreadable even
to the operator", E2EE becomes a deliberate, reviewed v2 protocol project.
The change is concentrated in the sync payload: `sync_changes.payload` and
entity columns become ciphertext, and domain validation shrinks to envelope
checks. Until then, no part of the product may claim end-to-end encryption.

## Observability and ops

- Structured JSON logs via `slog`; request IDs; no PII in logs (no codes,
  tokens, or record contents).
- `/healthz` (liveness), `/readyz` (DB ping), `/version`.
- Configuration via environment only; sane defaults for local development.

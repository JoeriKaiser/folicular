# AGENTS.md: folicular Backend Guidelines and Constraints

## Project Overview

**folicular** is the Go backend for **Luteal**, a French-first, private Android
menstrual cycle tracker and consensual Duo companion. It provides anonymous
account identity, offline-first delta synchronization, server-authoritative
validation of synchronized records, and clearly labeled cycle estimates.

The Android client lives at `~/AndroidStudioProjects/luteal` (package
`fr.luteal`). See its `PRODUCT.md` and `DESIGN.md` for product context.

---

## Source of Truth

**The backend is the source of truth when it comes to data.**

- The canonical schema, field validation rules, enum vocabularies, conflict
  resolution policy, and computed estimates are defined in this repository.
- When the Android client and the backend disagree, the backend's validated
  state wins. Clients re-derive their local caches from it.
- The Android client remains offline-first for display and local writes
  (see `luteal/docs/architecture/SYNC_BOUNDARY.md`), but its sync DTOs, enums,
  and validation must conform to `docs/api.md` and `docs/data-model.md` here.
- Schema changes start here: migrate this repository first, then adapt the
  client. Never let a client-side model silently diverge from `db/migrations`.
- Server sequence numbers and revision metadata are transport machinery, not
  domain truth; domain meaning lives in the validated record fields.

---

## Constraints & Requirements

### 1. Anonymous Authentication Only

- No email, no OAuth, no phone number, no password. An account is a
  high-entropy **account code** (Mullvad-style) shown once at creation, plus
  per-device bearer tokens. See `docs/api.md` for the flows.
- Collect no personally identifying information. Account codes and device
  tokens are stored only as SHA-256 hashes.
- Anonymous does not mean unguarded: all credential-checking endpoints are
  rate limited, and health data is sensitive data (CNIL "données de santé").
- "Anonymous" here describes the **authentication model**, not the data. Under
  GDPR the stored records are pseudonymous, not anonymous: they remain personal
  data and stay in scope of the regulation. Never let this section's wording
  leak into user-facing copy as a claim of anonymisation.
- Client addresses are HMAC'd under a per-process random pepper before use as
  rate-limit bucket keys (`internal/server/ratelimit.go`), so raw IPs are not
  held in memory. Request logging records no IPs, headers, tokens, or bodies.

### 2. Research-Backed Domain Model

- The data model is derived from published menstrual-health research and
  authoritative public-health sources, never invented. Every domain constant,
  range, and enum must trace to an entry in `docs/research/SOURCES.md`.
- New domain facts require: a source, a review date, the jurisdiction or scope
  note, and the exact schema/API decision the source informs.
- Prefer FIGO terminology for bleeding, STRAW+10 for reproductive life stages,
  and French or international public-health authorities (HAS, WHO, NHS) for
  condition-adjacent vocabulary.

### 3. No Medical Claims

- The API stores self-reported **observations** and serves computed
  **estimates**. These two categories must never be mixed in tables,
  endpoints, or response shapes.
- Estimates are always ranges with explicit uncertainty labels
  (`insufficient`, `low`, `moderate` - never "certain") and a method
  identifier. No endpoint may present menstruation, ovulation, or fertility
  timing as a fact or clinical conclusion.
- Condition tracking contexts (TDPM/PMDD, SPM/PMS, endometriosis, SOPK/PCOS)
  are user-selected tracking focuses only. The server must not infer, screen
  for, or announce a condition from patterns in the data.
- Server-authored copy is French, neutral, and inclusive. It does not assume
  gender identity, sexual activity, fertility goals, pregnancy intention,
  cycle regularity, or a gendered partner role.

### 4. Privacy and French/EU Data Protection

- Menstrual and symptom data are health data under GDPR/CNIL guidance. Apply
  data minimization, purpose limitation, and encryption at rest in deployment.
- Default locale is `fr`; default time zone is `Europe/Paris`.
- Deletion is a first-class operation: tombstone semantics for synchronized
  records, cascade deletion on account deletion.
- Do not claim end-to-end encryption anywhere until the threat model, key
  lifecycle, and rollback protection are implemented and reviewed. The threat
  model and key hierarchy now live in the client repo at
  `docs/architecture/E2EE_DESIGN.md`, which is the single design reference for
  both repositories.
- **E2EE migration is planned and will invert this server's authority.** A
  server that cannot read payloads cannot validate them or compute estimates.
  When it lands: `internal/domain` content validation and `internal/cyclecalc`
  are retired, `GET /v1/predictions/current` and the typed read endpoints
  `/v1/cycles` and `/v1/days` are removed, and records become opaque ciphertext
  with plaintext routing metadata only. Do not build new features that depend on
  the server reading record content.
- `internal/cyclecalc` has two known defects pending its retirement: it uses
  `minRangeRadiusDays = 2`, which the client has already corrected against the
  same Bull 2019 source and therefore disagrees with; and it computes ovulation
  and fertile windows, which conflicts with the client's explicit product
  non-goal. Do not extend it.

### 5. Duo Equality

- Tracker and partner experiences are equal product surfaces. The partner API
  is purpose-designed (grants-respecting projection), not a read-only clone.
- Sharing is private by default, explicit, granular (per-field grants),
  visible, and reversible. A partner never receives private notes or raw
  observations without a specific active grant.
- Revocation is a protocol event: a revoked grant or link must be observable
  by the partner on their next request.

---

## Technology Stack

- **Language:** Go 1.25+, standard library first (`net/http`, `log/slog`,
  `crypto/rand`, `encoding/json`).
- **Routing:** chi v5. **Queries:** sqlc with the SQLite engine.
- **Database:** SQLite via `modernc.org/sqlite` (pure Go). Migrations via
  golang-migrate, embedded and applied at boot.
- **Errors:** RFC 9457 `application/problem+json`.
- **Config:** environment variables only (see `internal/config`).

## Repository Layout

```
cmd/server/            entrypoint
internal/config/       env configuration
internal/db/           store (connection, pragmas, migrations), queries/, dbgen/ (sqlc output - do not edit)
internal/auth/         account codes, device tokens, middleware
internal/domain/       canonical types and validation (the contract)
internal/cyclecalc/    estimate engine
internal/api/          handlers
internal/server/       router and middleware wiring
docs/                  api.md, data-model.md, architecture.md, research/
```

## Developer & Subagent Rules

1. **Verification Gate:** `make ci` (vet + tests + build) must pass before
   yielding changes. Touching queries requires `make sqlc` first.
2. **OpenAPI is the wire contract:** `openapi/openapi.yaml` is the single
   source of truth for request/response shapes; the Android client generates
   its DTOs from it. Any API change updates `docs/api.md` and
   `openapi/openapi.yaml` together; `internal/contract` tests validate the
   spec, assert route/schema coverage, and (conformance tests) boot the real
   server and check its actual responses byte-for-byte against the spec.
3. **SQL is the storage contract:** change `internal/db/migrations` and
   `internal/db/queries`, regenerate, never edit `internal/db/dbgen`.
4. **Validation lives in `internal/domain`:** handlers parse, domain validates,
   sqlc persists. No raw SQL strings outside generated code.
5. **Cite the research:** any new range, enum value, or constant in domain or
   cyclecalc code must reference a `docs/research/SOURCES.md` entry in a
   comment, and the source register must be updated in the same change.
6. **Observations vs estimates:** keep the separation in every table, handler,
   and response shape.
7. **No PII, no tracking:** no analytics, no emails, no third-party services.
8. **No Emojis:** not in code, docs, commits, or API copy.
9. **French first** for any server-authored user-facing string.

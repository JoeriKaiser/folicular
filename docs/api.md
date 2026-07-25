# HTTP API Contract (v1)

Base URL: `/v1`. Content type: `application/json` (UTF-8). Errors use
`application/problem+json` (RFC 9457):

```json
{ "type": "about:blank", "title": "Validation failed", "status": 422,
  "detail": "changes[2]: flow: invalid value 'torrential'",
  "instance": "/v1/sync/push" }
```

Conventions:

- Dates are ISO-8601 calendar dates (`2026-07-21`) - user calendar
  observations. Instants are RFC 3339 UTC (`2026-07-21T14:03:00Z`).
- IDs are client-generated UUIDv7 strings for synchronized records.
- Authenticated routes require `Authorization: Bearer <device_token>`.
- The server is the source of truth: it validates every write and may reject
  or normalize; clients adopt the server's stored state.

## Authentication

### POST /v1/auth/register

Create an anonymous account and register the first device.

Request (`invite_code` is required only when registration is gated — see
`FOLICULAR_INVITE_CODES` in `deployment.md`; omit it when registration is open):

```json
{ "device_name": "Pixel 9", "invite_code": "LTL-BETA-0001" }
```

When registration is gated and `invite_code` is missing or invalid, the server
returns a generic `401` problem (no account is created and no detail leaks
which code is valid).

Response `201`:

```json
{
  "account": {
    "id": "019832e0-6c14-7000-8000-000000000001",
    "code": "LTL-8K3FQ-Z2WNT-7HJMC-4XRDB"
  },
  "device": {
    "id": "019832e0-6c15-7000-8000-000000000002",
    "name": "Pixel 9",
    "token": "ltok_Ab3-..."
  },
  "warning": "Le code de compte est affiche une seule fois. Conservez-le en lieu sur : il permet seul de retrouver votre compte."
}
```

The account `code` is shown **once**. Only its SHA-256 hash is stored. No
email recovery exists.

### POST /v1/auth/devices

Register an additional device using the account code. Rate limited per IP.

Request:

```json
{ "code": "LTL-8K3FQ-Z2WNT-7HJMC-4XRDB", "device_name": "Tablette" }
```

Response `201`: `{ "device": { "id": "...", "name": "Tablette", "token": "ltok_..." } }`

Errors: `401` invalid code (generic detail, no enumeration), `429` rate
limited.

### GET /v1/auth/devices

List devices for the account (id, name, created_at, last_seen_at, revoked,
current flag).

### DELETE /v1/auth/devices/{id}

Revoke a device token. `204`. Revoking the calling device is allowed.

## Account

### GET /v1/me

```json
{
  "account": { "id": "...", "status": "active", "created_at": "2026-07-21T14:03:00Z" },
  "device": { "id": "...", "name": "Pixel 9" },
  "settings": {
    "locale": "fr",
    "time_zone": "Europe/Paris",
    "life_stage": "reproductive_peak",
    "tracking_focus": ["pms"],
    "updated_at": "2026-07-21T14:03:00Z"
  }
}
```

`life_stage` values (STRAW+10-aligned, S11): `unknown | reproductive_early |
reproductive_peak | reproductive_late | menopause_transition_early |
menopause_transition_late | postmenopause_early | postmenopause_late`.
`tracking_focus` values: `pms | pmdd | endometriosis | pcos | custom`.
`life_stage` is user-selected, never inferred.

### PATCH /v1/me

Partial update of `settings` (same fields). Server-authoritative: not part of
the sync change log; devices refresh settings on pull. `200` returns the full
`/v1/me` body.

## Synchronization

Entity types: `cycle | bleeding_observation | daily_entry |
symptom_definition | symptom_log | biomarker_observation | medication_log`.

Record envelope (all types):

```json
{
  "id": "019832e1-0000-7000-8000-000000000010",
  "client_rev": "019832e1-1111-7000-8000-000000000011",
  "created_at": "2026-07-21T08:00:00Z",
  "updated_at": "2026-07-21T09:12:00Z",
  "deleted_at": null
}
```

`client_rev` changes on every local edit. Deletes carry `deleted_at` set and
the last known `data` (tombstone semantics).

### POST /v1/sync/push

Request:

```json
{
  "changes": [
    {
      "entity_type": "cycle",
      "data": {
        "id": "...", "client_rev": "...", "created_at": "...", "updated_at": "...",
        "deleted_at": null,
        "start_date": "2026-06-30",
        "end_date": null,
        "length_days": null,
        "bleeding_days": 5,
        "certainty": "recorded",
        "source": "manual",
        "notes": ""
      }
    }
  ]
}
```

Response `200`:

```json
{
  "applied":   [ { "entity_type": "cycle", "entity_id": "...", "seq": 412 } ],
  "conflicts": [ { "entity_type": "cycle", "entity_id": "...",
                   "reason": "superseded", "current": { "...server state..." } } ],
  "cursor": 412
}
```

Conflict policy (v1): entity-level last-write-wins ordered by
(`updated_at`, `client_rev`). A pushed record that does not win is not
applied; the server's current record is returned in `conflicts` so the client
can merge or surface the choice - silent loss is not permitted. Field-level
merging is future work.

Validation: every `data` payload is validated per type (see data-model.md);
invalid changes yield `422` with per-change detail and **no** partial apply
for that change (valid changes in the same batch still apply; the response
reports `rejected` entries):

```json
{ "applied": [...], "rejected": [ { "entity_type": "daily_entry", "entity_id": "...",
  "detail": "pain_level: must be between 1 and 5" } ], "conflicts": [], "cursor": 413 }
```

### GET /v1/sync/pull?since={cursor}&limit={n}

Response:

```json
{
  "changes": [
    { "seq": 410, "entity_type": "daily_entry", "entity_id": "...",
      "deleted": false, "updated_at": "2026-07-21T09:12:00Z",
      "data": { "...full record..." } }
  ],
  "cursor": 413,
  "has_more": false
}
```

Default `limit` 500, max 2000. Tombstones carry `deleted: true` and
`data: null`.

## Read models

Convenience reads over validated state (used by clients and the Duo
projection; the sync endpoints remain the replication path).

### GET /v1/cycles?from={date}&to={date}

Live cycles overlapping the range, newest first. Server fills `length_days`
from `start_date`/`end_date` when derivable.

### GET /v1/days?from={date}&to={date}

Merged daily view: for each date with any observation:

```json
{
  "date": "2026-07-21",
  "cycle_day": 22,
  "bleeding": { "flow": "none", "intermenstrual": false, "product_count": null },
  "entry": { "pain_level": 2, "mood_level": 3, "energy_level": 4, "notes": "" },
  "symptoms": [ { "key": "cramps", "severity": 2, "notes": "" } ],
  "biomarkers": { "bbt_celsius": 36.7, "cervical_fluid": "creamy",
                  "cervix_position": null, "cervix_firmness": null },
  "medications": [ { "name": "Ibuprofene 400 mg", "kind": "pain_relief" } ]
}
```

`cycle_day` is computed from the most recent cycle start; `null` when no
cycle covers the date.

### GET /v1/predictions/current

Estimates only. Computed on demand from the account's recorded cycle starts
by `cyclecalc` (method `cycle_length_median_v1`; luteal constant 13 days per
S07; windows widened by observed variability per S08).

```json
{
  "generated_at": "2026-07-21T14:03:00Z",
  "method": "cycle_length_median_v1",
  "basis": { "cycle_count": 6, "median_length_days": 29, "variability_days": 4 },
  "next_menstruation": {
    "window_start": "2026-07-27", "central_date": "2026-07-29",
    "window_end": "2026-07-31", "confidence": "moderate"
  },
  "ovulation_estimate": {
    "window_start": "2026-07-12", "central_date": "2026-07-16",
    "window_end": "2026-07-19", "confidence": "low"
  },
  "fertile_window_estimate": {
    "window_start": "2026-07-07", "window_end": "2026-07-17",
    "confidence": "low"
  },
  "disclaimer": "Estimations fondees sur vos cycles enregistres. Ce ne sont pas des previsions certaines ni un avis medical."
}
```

With fewer than 3 recorded cycle starts, every section is `null` and
`basis.cycle_count` reflects the shortage; `confidence` would be
`insufficient`. No population default is ever substituted (S04).

## Duo

Both members have their own anonymous accounts. Pairing uses a short-lived,
single-use code transported three equivalent ways: typed by hand, opened as
a shareable link, or scanned as a QR code. The client renders the QR from
`pairing_url` (e.g. ZXing); the server has no QR logic. The code embedded in
the URL is a bearer-like secret: 50 bits of entropy, 7-day expiry, single
use, rate-limited acceptance.

### POST /v1/duo/invitations

Tracker creates a pending link:

```json
{
  "link_id": "...",
  "pairing_code": "FQ7K2-WNT4H",
  "pairing_url": "https://luteal-duo.waldemar.site/accept?code=FQ7K2-WNT4H",
  "expires_at": "2026-07-28T14:03:00Z"
}
```

`pairing_url` base is deployment-configured (`FOLICULAR_PAIRING_BASE_URL`)
and doubles as the Android deep link / App Link target. The web target
should explain the flow for partners without the app installed.

### POST /v1/duo/links

Partner (authenticated with their own account) accepts:
`{ "pairing_code": "FQ7K2-WNT4H" }` -> `201`:

```json
{ "link": { "id": "...", "role": "partner", "status": "active", "created_at": "..." } }
```

Errors: `404` invalid/used/expired code (generic, no enumeration), `422`
self-pairing. No grants exist yet - sharing is private by default.

### GET /v1/duo/links

Both roles see their links (id, role, status, created_at, revoked_at).

### PATCH /v1/duo/links/{id}/grants

Tracker only: `{ "field": "mood", "granted": true }`. Fields: `cycle_day |
period_estimate | mood | energy | support_requests`. Revocation is a row
update (`revoked_at`), observable by the partner immediately.

### GET /v1/duo/view

Role-aware projection. For the **partner**, returns **only** fields with an
active grant:

```json
{
  "link_id": "...",
  "role": "partner",
  "as_of": "2026-07-21T14:03:00Z",
  "cycle_day": 22,
  "period_estimate": { "window_start": "2026-07-27", "window_end": "2026-07-31" },
  "mood": { "date": "2026-07-21", "level": 3 },
  "energy": null,
  "support_requests": [ { "id": "...", "author_role": "partner", "kind": "comfort",
                          "message": "...", "created_at": "...", "acknowledged_at": null } ]
}
```

For the **tracker**, data fields are `null` and `support_requests` is always
visible (it is their link). Ungranted fields are `null` - absence is
deliberately indistinguishable from no data. `period_estimate` shares only
the window edges, never the internal basis. Private notes and raw
observations are never exposed through this endpoint. `404` when no active
link exists (including immediately after revocation).

### POST /v1/duo/support-requests / PATCH /v1/duo/support-requests/{id}/ack

Either role may request support (`kind`: `general | comfort | practical |
space`); the recipient acknowledges. Messages are visible to both members of
the link only.

### DELETE /v1/duo/links/{id}

Either role may end the link (also cancels a pending invitation).
Status becomes `revoked`; grants stop applying immediately; historical
private data is never transferred.

## Operations

- `GET /healthz` -> `200 {"status":"ok"}` (liveness, no DB).
- `GET /readyz` -> `200` when the DB answers, `503` otherwise.
- `GET /version` -> `{"version":"...","commit":"..."}`.

## Rate limits (v1 defaults)

| Route group        | Limit                    |
|--------------------|--------------------------|
| POST /v1/auth/*    | 10 req/min per client IP |
| Authenticated API  | 300 req/min per device   |

`429` responses include `Retry-After`.

## Versioning and compatibility

The contract is versioned in the path (`/v1`). Additive fields are
compatible; removals or meaning changes require `/v2` and a migration note.
Client and server schema conformance fixtures are planned (see the client's
pre-backend roadmap, Milestone 6).

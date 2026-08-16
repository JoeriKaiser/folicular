# HTTP API Contract (v1)

Base URL: `/v1`. Content type: `application/json` (UTF-8). Errors use
`application/problem+json` (RFC 9457):

```json
{
  "type": "about:blank",
  "title": "Validation échouée",
  "status": 422,
  "detail": "changes[0]: entity_id: UUID invalide",
  "instance": "/v1/sync/push"
}
```

Conventions:

- Dates are ISO-8601 calendar dates (`2026-07-21`). Instants are RFC 3339 UTC (`2026-07-21T14:03:00Z`).
- IDs are client-generated UUIDv7 strings for synchronized records.
- Authenticated routes require `Authorization: Bearer <device_token>`.
- Under end-to-end encryption, the server stores and relays sealed ciphertext blobs (`AES-256-GCM`). It validates routing envelopes (IDs, entity types, timestamps, tombstones) and never inspects or mutates payload contents.

## Authentication

### POST /v1/auth/register

Create an anonymous account and register the first device.

Request (`invite_code` is required only when registration is gated; omit it when registration is open):

```json
{
  "device_name": "Pixel 9",
  "invite_code": "LTL-BETA-0001"
}
```

When registration is gated and `invite_code` is missing or invalid, the server returns a generic `401` problem.

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
  "warning": "Le code de compte est affiché une seule fois. Conservez-le en lieu sûr : il permet seul de retrouver votre compte et d'ajouter des périphériques."
}
```

The account `code` is shown **once**. Only its SHA-256 hash is stored. No email recovery exists.

### POST /v1/auth/devices

Register an additional device using the account code. Rate limited per IP.

Request:

```json
{
  "code": "LTL-8K3FQ-Z2WNT-7HJMC-4XRDB",
  "device_name": "Tablette"
}
```

Response `201`:

```json
{
  "account_id": "019832e0-6c14-7000-8000-000000000001",
  "device": {
    "id": "019832e0-6c16-7000-8000-000000000003",
    "name": "Tablette",
    "token": "ltok_Cd4-..."
  }
}
```

Errors: `401` invalid code (generic detail, no enumeration), `429` rate limited.

### GET /v1/auth/devices

List devices for the account:

```json
{
  "devices": [
    {
      "id": "019832e0-6c15-7000-8000-000000000002",
      "name": "Pixel 9",
      "created_at": "2026-07-21T14:03:00Z",
      "last_seen_at": "2026-07-21T14:05:00Z",
      "revoked": false,
      "current": true
    }
  ]
}
```

### DELETE /v1/auth/devices/{id}

Revoke a device token. `204`. Revoking the calling device is allowed.

## Account

### GET /v1/me

Returns account metadata, calling device, and the sealed settings blob:

```json
{
  "account": {
    "id": "019832e0-6c14-7000-8000-000000000001",
    "status": "active",
    "created_at": "2026-07-21T14:03:00Z"
  },
  "device": {
    "id": "019832e0-6c15-7000-8000-000000000002",
    "name": "Pixel 9"
  },
  "settings": {
    "settings": "AQEX+x0f...",
    "updated_at": "2026-07-21T14:03:00Z"
  }
}
```

Settings (locale, time zone, life stage, tracking focus) carry Article 9 health data and are sealed on the client.

### PATCH /v1/me

Replace the sealed settings blob:

```json
{
  "settings": "AQEX+x0f..."
}
```

Response `200` returns the full `/v1/me` response body.

### DELETE /v1/me

Permanently deletes the account and cascades deletion across all records, changes, settings, and Duo links (GDPR Article 17). Response `204`.

### GET /v1/export

Returns a sealed export document of all server-held data for the account (GDPR Article 20, data portability):

```json
{
  "format": "folicular.export.v2.sealed",
  "generated_at": "2026-07-21T14:03:00Z",
  "note": "Les enregistrements sont chiffrés de bout en bout. Ce serveur ne peut pas les déchiffrer : utilisez votre code de compte dans l'application pour obtenir une version lisible.",
  "account": { "id": "...", "status": "active", "created_at": "..." },
  "settings": { "settings": "...", "updated_at": "..." },
  "records": [
    {
      "entity_id": "019832e1-0000-7000-8000-000000000010",
      "entity_type": "cycle",
      "client_rev": "019832e1-1111-7000-8000-000000000011",
      "deleted": false,
      "updated_at": "2026-07-21T09:12:00Z",
      "ciphertext": "AQEX+x0f..."
    }
  ]
}
```

## Synchronization

Entity types: `cycle | bleeding_observation | daily_entry | symptom_definition | symptom_log | biomarker_observation | medication_log`.

### POST /v1/sync/push

Push a batch of sealed mutations or deletions:

```json
{
  "changes": [
    {
      "entity_type": "cycle",
      "entity_id": "019832e1-0000-7000-8000-000000000010",
      "client_rev": "019832e1-1111-7000-8000-000000000011",
      "updated_at": "2026-07-21T09:12:00Z",
      "deleted": false,
      "ciphertext": "AQEX+x0f..."
    }
  ]
}
```

Response `200`:

```json
{
  "applied": [
    {
      "entity_type": "cycle",
      "entity_id": "019832e1-0000-7000-8000-000000000010",
      "seq": 412
    }
  ],
  "rejected": [],
  "conflicts": [],
  "cursor": 412
}
```

- **Conflict policy:** Entity-level last-write-wins ordered by `(updated_at, client_rev)`. When an update loses the LWW check, the server returns a `conflictRef` containing `current_ciphertext`. The client decrypts and reconciles locally.
- **Tombstones:** Deletions specify `deleted: true` and `ciphertext: null`.

### GET /v1/sync/pull?since={cursor}&limit={n}

Fetch ordered change rows since the client cursor:

```json
{
  "changes": [
    {
      "seq": 410,
      "entity_type": "daily_entry",
      "entity_id": "019832e1-0000-7000-8000-000000000020",
      "client_rev": "019832e1-2222-7000-8000-000000000022",
      "deleted": false,
      "updated_at": "2026-07-21T09:12:00Z",
      "ciphertext": "AQEX+x0f..."
    }
  ],
  "cursor": 412,
  "has_more": false
}
```

Default `limit` 500, max 2000. Tombstones carry `deleted: true` and omit `ciphertext`.

## Duo Companion

Both members have their own anonymous accounts. Pairing uses a short-lived, single-use code (50 bits, 7-day TTL) transported as text, deep link, or QR code.

### POST /v1/duo/invitations

Tracker creates a pending link:

```json
{
  "link_id": "019832e2-0000-7000-8000-000000000030",
  "pairing_code": "FQ7K2-WNT4H",
  "pairing_url": "https://luteal-duo.waldemar.site/accept?code=FQ7K2-WNT4H",
  "expires_at": "2026-07-28T14:03:00Z"
}
```

### POST /v1/duo/links

Partner accepts using the pairing code:

Request:

```json
{
  "pairing_code": "FQ7K2-WNT4H"
}
```

Response `201`:

```json
{
  "link": {
    "id": "019832e2-0000-7000-8000-000000000030",
    "role": "partner",
    "status": "active",
    "created_at": "2026-07-21T14:03:00Z"
  }
}
```

### GET /v1/duo/links

Lists the caller's Duo links in either role (id, role, status, created_at, revoked_at).

### PATCH /v1/duo/links/{id}/grants

Tracker toggles a sharing grant:

```json
{
  "field": "mood",
  "granted": true
}
```

Fields: `cycle_day | period_estimate | mood | energy | support_requests`.

### PUT /v1/duo/payload

Tracker publishes the sealed projection for their partner:

```json
{
  "payload": "AQEX+x0f..."
}
```

Response `204`. Only the tracker role may publish. The client generates this projection by applying active grants locally and encrypting with the Duo link key.

### GET /v1/duo/view

Returns the sealed projection and support requests:

```json
{
  "link_id": "019832e2-0000-7000-8000-000000000030",
  "role": "partner",
  "as_of": "2026-07-21T14:03:00Z",
  "payload": "AQEX+x0f...",
  "payload_updated_at": "2026-07-21T14:03:00Z",
  "grants": ["cycle_day", "period_estimate", "mood"],
  "support_requests": [
    {
      "id": "019832e3-0000-7000-8000-000000000040",
      "author_role": "partner",
      "kind": "comfort",
      "message_ciphertext": "AQEX+x0f...",
      "created_at": "2026-07-21T14:03:00Z",
      "acknowledged_at": null
    }
  ]
}
```

### POST /v1/duo/support-requests

Submit a support request with encrypted message:

```json
{
  "link_id": "019832e2-0000-7000-8000-000000000030",
  "kind": "comfort",
  "message": "AQEX+x0f..."
}
```

Kinds: `general | comfort | practical | space`.

### PATCH /v1/duo/support-requests/{id}/ack

Recipient acknowledges a request. Response `204`.

### DELETE /v1/duo/links/{id}

Either member may revoke the link. Response `204`. Sharing ends immediately.

## Operations

- `GET /healthz` -> `200 {"status":"ok"}`
- `GET /readyz` -> `200 {"status":"ready"}` (503 on database failure)
- `GET /version` -> `200 {"version":"dev"}`

## Rate Limits

| Route group | Limit |
|---|---|
| `POST /v1/auth/*` | 10 req/min per client IP |
| Authenticated API | 300 req/min per device |

`429` responses include `Retry-After`.

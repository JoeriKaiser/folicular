# Deployment

Production target: a single small VPS (2 vCPU / 2 GB RAM is far more than
enough) running **[Coolify](https://coolify.io)** as a self-hosted PaaS.
Coolify builds the container from this repository, terminates TLS with
Let's Encrypt through its proxy, and manages redeploys and health checks.
One container, one SQLite file on a persistent volume.

Distribution context: the Android client ships **F-Droid only** (no Google
Play). This matters for the security posture below — see
[Security posture for an F-Droid client](#security-posture-for-an-f-droid-client).

## Threat-aware baseline

Menstrual and symptom data are health data (S30, CNIL). **Record content is
end-to-end encrypted**: clients seal it with a key derived from the account
code, and this server stores ciphertext plus routing metadata only. It holds no
key and cannot decrypt anything, by construction. See the client repository's
`docs/architecture/E2EE_DESIGN.md`.

That changes the operational posture in three ways worth internalising before
you deploy:

- **A database breach yields ciphertext.** Under GDPR Art. 34 that generally
  makes a leak non-notifiable to users, because the data is unintelligible
  without keys you never held. This is the single biggest security property of
  the deployment, and it is a property of the design, not the hosting.
- **Backups are far less sensitive** than they used to be — they are encrypted
  at the record level before they ever reach the disk.
- **You cannot help a user recover data.** The account code is the only key.
  If a user loses it, their synced history is permanently unreadable and there
  is no support path. Say so in onboarding, not after the fact.

What the server still sees, and therefore what the deployment must still
protect: account existence, device count, the Duo link graph, request timing
and volume, and client IPs. Sync traffic volume alone reveals roughly *when*
someone logs, even though it cannot reveal *what*.

The deployment must:

- terminate TLS everywhere (never serve plain HTTP) — Coolify's proxy does this,
- encrypt the disk or volume at rest (LUKS, or a provider encrypted volume) —
  now defence in depth over the metadata rather than the last line of defence,
- host in the EU (e.g. OVH, Hetzner),
- log no codes or tokens, and no client IPs (the server already does this;
  rate-limit keys are HMAC'd under a per-process pepper),
- allow complete deletion (account cascade deletes exist),
- run exactly **one** instance (SQLite is single-writer; do not scale replicas).

## Prerequisites

1. A VPS with Coolify installed and its proxy (Traefik by default) running.
2. DNS records pointing at the VPS public IP for the API domain
   (`luteal-api.waldemar.site`). Coolify provisions the Let's Encrypt
   certificate once the domain resolves. The Duo pairing domain
   (`luteal-duo.waldemar.site`) is used only to build shareable pairing links;
   point it at an App Link / fallback host if you want those links to open the
   app directly.
3. (Recommended) An S3-compatible bucket for backups (OVH Object Storage,
   Hetzner, Backblaze).

## Deploying the application in Coolify

Create the app as **Project → + New Resource → Application → Git Repository**
(or a Docker Registry image if you prefer to push prebuilt images).

| Setting | Value |
|---|---|
| Build Pack | **Dockerfile** |
| Repository / Branch | this repo, `main` (or a release tag) |
| Base Directory | `/` |
| Dockerfile Location | `/Dockerfile` |
| Domain | `https://luteal-api.waldemar.site` |
| Ports Exposes | `8080` (the container's internal listening port) |

If you prefer Coolify's **Docker Compose** build pack instead, point it at
`docker-compose.prod.yml`. That file uses `expose` rather than `ports` (so the
service stays on the internal network behind the proxy instead of being
published on a host port), omits `container_name` so redeploys do not collide,
and fails fast when `FOLICULAR_INVITE_CODES` or `FOLICULAR_TRUSTED_PROXIES` are
unset. The root `docker-compose.yml` is for local development only and must not
be used on the VPS.

The multi-stage `Dockerfile` produces a static, CGO-free binary, runs as a
non-root user (uid 10001), and already declares `EXPOSE 8080` plus a
`HEALTHCHECK` on `/healthz`. Coolify reads these; you normally only need to
set the domain, the exposed port, the volume, and the environment variables.

### Persistent storage (critical)

Add a persistent volume so the SQLite database survives redeploys and image
rebuilds:

| Setting | Value |
|---|---|
| Mount Path | `/data` |
| Type | Volume |

The database (`folicular.db`) and its WAL sidecar files (`-wal`, `-shm`) all
live under `/data`, so a single volume covers them. **Without this volume,
every redeploy wipes all accounts and data.** Use a local volume, not a
network filesystem.

### Environment variables

Set these in the application's Environment Variables panel:

| Variable | Production value | Notes |
|---|---|---|
| `FOLICULAR_ADDR` | `:8080` | Container listens internally; the proxy routes to it. |
| `FOLICULAR_DB_PATH` | `/data/folicular.db` | Must be on the persistent volume. |
| `FOLICULAR_LOG_LEVEL` | `info` | Structured JSON logs; no PII by design. |
| `FOLICULAR_PAIRING_BASE_URL` | `https://luteal-duo.waldemar.site` | Public base for Duo pairing links/QR codes (`{base}/accept?code=…`). Must match the domain the app treats as its App Link target. |
| `FOLICULAR_TRUSTED_PROXIES` | e.g. `172.16.0.0/12` | Comma-separated IPs/CIDRs the reverse proxy connects from. Required for correct per-IP rate limiting behind Coolify (see below). Empty trusts no proxy. |
| `FOLICULAR_INVITE_CODES` | e.g. `LTL-BETA-0001,LTL-BETA-0002` | Comma-separated invite codes. When set, account registration requires a matching code (closed rollout); only SHA-256 hashes are held in memory. Empty leaves registration open (and logs a warning). |

### Health checks and zero-downtime deploy

- Point Coolify's HTTP health check at `GET /healthz` (liveness, no DB) or
  `GET /readyz` (DB ping) on port `8080`, or rely on the image `HEALTHCHECK`.
- Keep **one instance**. SQLite is not multi-instance safe; do not enable
  horizontal scaling for this service.
- Migrations are embedded and run at boot; a redeploy that ships a new binary
  applies pending migrations automatically.

### Client IP and rate limiting behind the proxy

The credential endpoints (`/v1/auth/register`, `/v1/auth/devices`,
`/v1/duo/links`) are rate limited per client IP in-process. Behind Coolify's
proxy the immediate peer is the proxy itself, so the limiter recovers the true
client IP from `X-Forwarded-For` / `X-Real-IP` — but **only when the peer is in
`FOLICULAR_TRUSTED_PROXIES`**. A direct (untrusted) client's forwarding headers
are ignored, so the IP cannot be spoofed to evade the limiter.

Set `FOLICULAR_TRUSTED_PROXIES` to the network the Coolify proxy connects from
(typically the Docker network, e.g. `172.16.0.0/12`; match your actual proxy
network). Traefik sets `X-Forwarded-For` by default. If you would rather rate
limit at the edge (Coolify proxy rules or a CDN/WAF), you can leave this empty
and treat the in-process limiter as a backstop.

### Registration gate (invite codes)

For the closed rollout, account registration is gated by invite codes. Set
`FOLICULAR_INVITE_CODES` to a comma-separated list of codes (e.g.
`LTL-BETA-0001,LTL-BETA-0002`); each is held only as a SHA-256 hash. When the
list is non-empty, `POST /v1/auth/register` requires a matching `invite_code`
and otherwise returns a generic `401` (no enumeration). When the list is empty,
registration is open and the server logs a warning at startup — so production
must set at least one code.

Distribute codes out of band (one per tester). The client collects the code in
Settings → Synchronisation and sends it on first registration. Codes are
**reusable** in this implementation; rotate or revoke them by editing the env
and restarting. If you later need single-use, revocable, auditable codes, move
them to a database table with a minting CLI (a contained backend change).

## Backups

SQLite + WAL makes online backups trivial, but note the runtime image is
minimal Alpine and ships **no `sqlite3` CLI** (the DB driver is pure Go).
Choose one:

1. **Litestream sidecar (recommended):** run `litestream/litestream` as a
   second Coolify resource that mounts the same `/data` volume and replicates
   continuously to S3. Sharing a volume between two resources requires a host
   mount or a named Docker volume referenced by both — configure this in
   Coolify's storage settings.
   ```yaml
   dbs:
     - path: /data/folicular.db
       replicas:
         - url: s3://folicular-backup/db
   ```
2. **Host-level cron:** on the VPS, back up the volume's host path with the
   system `sqlite3`:
   ```sh
   sqlite3 /var/lib/docker/volumes/<folicular-data>/_data/folicular.db \
     ".backup '/opt/folicular/backups/folicular-$(date +%F).db'"
   ```
3. **(Future, cleanest)** add a `folicular backup` subcommand that issues
   `VACUUM INTO` through the embedded pure-Go driver, then call it from a
   Coolify scheduled task inside the container.

**Test restores regularly.** A backup you have never restored is a hope, not a
backup.

## Security posture for an F-Droid client

The client is distributed via F-Droid only. That shapes what "locking the API
down" can and cannot mean.

**There are now two boundaries, and the stronger one is cryptographic.** Record
content is encrypted client-side, so even a stranger who obtained a valid token
— or the database itself — reads ciphertext. Authentication is the second
boundary: every data endpoint (`/me`, `/sync/*`, `/duo/*`) sits behind
`internal/auth/middleware.go` with a valid, non-revoked device bearer token
(SHA-256 hashed, indexed lookup, no user enumeration, revocation and
account-status checks). A stranger who discovers the domain and hits
`/v1/sync/pull` gets `401`. The plaintext read endpoints `/cycles` and `/days`
no longer exist; they required reading record content.

**"Only the app binary may call the API" is not enforceable.** The app runs on
a device the user controls; an APK is decompilable and its traffic observable.
Two consequences specific to F-Droid:

- **No Play Integrity / SafetyNet.** These require Google Play Services and a
  Play Store install; F-Droid builds have neither. Client attestation is not
  available as a layer.
- **No embedded app secrets.** F-Droid builds from public source, so any
  static API key would be published in the repository. Do not add one.

**The genuinely open surface is `/v1/auth/register`** (plus the harmless
`/healthz`, `/readyz`, `/version`). Registration is **gated by invite code**
for the closed rollout (see [Registration gate](#registration-gate-invite-codes)):
strangers cannot create accounts without a code you distribute. Per-IP rate
limiting on the credential endpoints (via `FOLICULAR_TRUSTED_PROXIES`, above)
is the abuse backstop.

Chosen posture and remaining levers, in priority order:

1. **Invite-code registration gate (primary access control, active).**
   `FOLICULAR_INVITE_CODES` restricts account creation to people you give a
   code to. Codes are matched by SHA-256 hash and live server-side only — never
   in plaintext, never in the app.
2. **Edge rate limiting (abuse backstop).** Per-IP rate limiting on the
   credential endpoints, reinforced at the edge (Coolify proxy rules or a
   CDN/WAF) for launch.
3. **Single-use invite codes (later hardening).** The env codes are reusable
   (a tester could share theirs). If you need single-use, revocable, auditable
   codes, move them to a database table with a minting CLI.
4. **Certificate pinning (later, with care).** Valuable for health data in
   transit, but with auto-renewed Let's Encrypt certs you must pin a *stable*
   key (the Let's Encrypt root/intermediate SPKI, or a private key you reuse
   across renewals) — never the leaf certificate, which rotates every ~90 days
   and would brick the app. Ship a remote kill-switch / remote-config fallback
   so a bad pin is recoverable. Treat as deliberate hardening, not a launch
   blocker.

## Operations

- **Health:** `GET /healthz` (liveness), `GET /readyz` (DB check). Point your
  uptime monitor at these.
- **Logs:** Coolify's application logs, or `journalctl`/`docker logs` on the
  host. Structured JSON; pipe to `jq`. No codes, tokens, or record contents.
- **Updates:** push to the deployed branch/tag; Coolify rebuilds and redeploys.
  Migrations are embedded and idempotent and run at boot.
- **Firewall:** keep it minimal — 22, 80, 443 only. Coolify's proxy binds the
  public ports; the application container does not need to be publicly
  reachable directly.

## Appendix: bare-metal alternative

If Coolify is unavailable (disaster recovery, or a minimal host), the service
runs as a plain binary behind Caddy.

```sh
GOOS=linux GOARCH=amd64 make build          # -> bin/folicular
scp bin/folicular vps:/opt/folicular/folicular
sudo useradd --system --home /opt/folicular folicular
sudo mkdir -p /opt/folicular/data && sudo chown -R folicular /opt/folicular
```

`/etc/systemd/system/folicular.service`:

```ini
[Unit]
Description=folicular (Luteal backend)
After=network.target

[Service]
Type=simple
User=folicular
WorkingDirectory=/opt/folicular
Environment=FOLICULAR_ADDR=127.0.0.1:8080
Environment=FOLICULAR_DB_PATH=/opt/folicular/data/folicular.db
Environment=FOLICULAR_LOG_LEVEL=info
Environment=FOLICULAR_PAIRING_BASE_URL=https://luteal-duo.waldemar.site
ExecStart=/opt/folicular/folicular
Restart=on-failure
RestartSec=2
# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/opt/folicular/data
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Caddy reverse proxy (automatic Let's Encrypt):

```
luteal-api.waldemar.site {
    reverse_proxy 127.0.0.1:8080
    encode zstd gzip
}
```

Bind folicular to `127.0.0.1` only so nothing can bypass the proxy. Backups:
Litestream or `sqlite3 ... ".backup ..."` as above.

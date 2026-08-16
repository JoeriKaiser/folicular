# Self-Hosting Guide

This guide covers self-hosting **folicular**, the backend service for the **Luteal** Android application (available on F-Droid).

folicular is designed for effortless self-hosting: a single lightweight Go binary with embedded SQLite storage, requiring zero external database services and minimal system resources.

---

## Overview and Architecture

- **Architecture Support**: Multi-arch container images are provided for `linux/amd64` (x86_64) and `linux/arm64` (Raspberry Pi 4/5, Apple Silicon, Oracle Cloud Ampere, and other ARM64 servers).
- **Resource Footprint**: Extremely lightweight (~20-30 MB RAM idle, negligible CPU usage). Suitable for a $4/month VPS, a Raspberry Pi, or a home server.
- **Database Engine**: Pure-Go SQLite via `modernc.org/sqlite` (compiled with `CGO_ENABLED=0`). All data, indexes, and migrations reside in a single SQLite database file (`folicular.db`) using WAL (Write-Ahead Logging) mode.
- **Privacy by Construction**:
  - All menstrual cycle observations and symptom records are end-to-end encrypted (E2EE) on the Android device prior to transmission.
  - The server stores only ciphertext blobs and routing metadata. It holds no cryptographic keys and cannot decrypt user records.
  - Accounts are anonymous, high-entropy random identifiers (Mullvad-style); no email, phone number, or OAuth identity is required.

---

## Deployment Options

Choose the setup that best matches your infrastructure:

- **Option A**: Quickstart with Docker Compose (standalone)
- **Option B**: Turnkey Automated HTTPS with Docker Compose + Caddy (recommended for public domains)
- **Option C**: Reverse Proxy Integration (Traefik, Nginx Proxy Manager, Caddy, Cloudflare Tunnels)
- **Option D**: Homelab / Private Network (Tailscale, WireGuard, LAN) with Android TLS guidance
- **Option E**: Standalone Linux Binary (systemd service)

---

### Option A: Standalone Docker Compose

Use this option if you want to run folicular in a standalone Docker container and handle reverse proxying or networking separately.

1. Download the standalone compose file and configuration template:

   ```sh
   mkdir -p folicular && cd folicular
   curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/docker-compose.selfhost.yml
   curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/.env.example
   cp .env.example .env
   ```

2. (Optional) Adjust variables in `.env`:
   - `FOLICULAR_PORT`: Host port to expose (default: `8080`).
   - `FOLICULAR_INVITE_CODES`: Comma-separated list of invite codes to restrict registrations.
   - `FOLICULAR_PAIRING_BASE_URL`: Public URL for Duo pairing links.

3. Start the container:

   ```sh
   docker compose -f docker-compose.selfhost.yml up -d
   ```

4. Verify health status:

   ```sh
   docker compose -f docker-compose.selfhost.yml ps
   curl http://127.0.0.1:8080/healthz
   ```

---

### Option B: Turnkey Automated HTTPS (Docker Compose + Caddy)

Recommended for public deployments. This two-container stack runs folicular alongside [Caddy](https://caddyserver.com/), which automatically requests and renews Let's Encrypt TLS certificates.

1. Download the required files:

   ```sh
   mkdir -p folicular && cd folicular
   curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/docker-compose.caddy.yml
   curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/Caddyfile
   curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/.env.example
   cp .env.example .env
   ```

2. Edit `.env` and set your public domain name:

   ```ini
   DOMAIN=folicular.example.com
   FOLICULAR_PAIRING_BASE_URL=https://folicular.example.com
   FOLICULAR_LOG_LEVEL=info
   ```

3. Ensure ports 80 and 443 are forwarded from your firewall/router to the server, and DNS for `DOMAIN` points to your public IP.

4. Start the stack:

   ```sh
   docker compose -f docker-compose.caddy.yml up -d
   ```

5. Caddy will automatically provision TLS certificates from Let's Encrypt. Verify connectivity:

   ```sh
   curl https://folicular.example.com/healthz
   ```

---

### Option C: Reverse Proxy Integration

If you already operate a reverse proxy in your environment, route traffic to folicular's internal port (`8080`) and forward proxy headers.

#### Header Requirements
Your reverse proxy must pass the following headers to folicular:
- `Host`
- `X-Real-IP`
- `X-Forwarded-For`
- `X-Forwarded-Proto`

Configure `FOLICULAR_TRUSTED_PROXIES` in folicular's environment with the IP or CIDR block of your reverse proxy (e.g. `172.16.0.0/12,127.0.0.1,10.0.0.0/8`). This allows folicular to safely recover the real client IP for rate limiting on authentication routes.

#### Example: Caddy
```caddy
folicular.example.com {
    encode zstd gzip
    reverse_proxy folicular:8080 {
        header_up Host {host}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Proto {scheme}
    }
}
```

#### Example: Nginx / Nginx Proxy Manager
```nginx
server {
    server_name folicular.example.com;
    listen 443 ssl http2;

    # SSL certificates configuration...

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

#### Example: Traefik (Docker Labels)
```yaml
services:
  folicular:
    image: ghcr.io/joerikaiser/folicular:latest
    restart: unless-stopped
    environment:
      FOLICULAR_ADDR: ":8080"
      FOLICULAR_DB_PATH: /data/folicular.db
      FOLICULAR_TRUSTED_PROXIES: "172.16.0.0/12"
    volumes:
      - folicular-data:/data
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.folicular.rule=Host(`folicular.example.com`)"
      - "traefik.http.routers.folicular.entrypoints=websecure"
      - "traefik.http.routers.folicular.tls.certresolver=letsencrypt"
      - "traefik.http.services.folicular.loadbalancer.server.port=8080"
```

#### Example: Cloudflare Tunnel (cloudflared)
In your `config.yml`:
```yaml
ingress:
  - hostname: folicular.example.com
    service: http://localhost:8080
  - service: http_status:404
```

---

### Option D: Homelab / Private Network (Tailscale, WireGuard, LAN)

You can host folicular purely on a private network or homelab mesh. However, please review the mobile operating system TLS requirements below.

#### Android Cleartext Traffic & TLS Constraints
- **Android Default Security Policy**: Android 9 (API 28) and newer blocks cleartext HTTP traffic (`http://`) by default for all applications.
- **Custom Certificate Authorities**: Android does not trust user-installed or self-signed Root CA certificates for application network connections unless explicitly configured in application source code.
- **Recommendation**: Even within a private homelab, use a publicly trusted TLS certificate.

#### Recommended Private Setup: Tailscale HTTPS (MagicDNS)
[Tailscale](https://tailscale.com/) provides valid public Let's Encrypt certificates for machine names on your tailnet:

1. Enable **MagicDNS** and **HTTPS Certificates** in your Tailscale admin console.
2. Run `tailscale serve` on your host machine to terminate TLS with automated public certificates:
   ```sh
   tailscale serve --https 443 http://127.0.0.1:8080
   ```
3. Your server is now reachable at `https://<node-name>.<tailnet-name>.ts.net` from any device connected to your Tailscale network, with a valid TLS certificate that Android natively trusts.

#### Alternative: WireGuard / Local DNS with Public Split-DNS TLS
If using WireGuard or local LAN DNS:
1. Assign a public domain name you own to the internal IP (e.g. `folicular.home.example.com` pointing to `192.168.1.50` or a WireGuard IP).
2. Use Caddy or certbot with a **DNS-01 ACME challenge** (via Cloudflare, DuckDNS, etc.) to issue a Let's Encrypt certificate without opening inbound port 80/443 to the internet.
3. Android will connect securely over TLS because the certificate is signed by Let's Encrypt.

---

### Option E: Standalone Linux Binary (systemd)

For bare-metal servers or minimal virtual machines without Docker.

1. Build the binary from source or download from releases:

   ```sh
   # Build static binary
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/folicular ./cmd/server
   ```

2. Create a dedicated system user and data directory:

   ```sh
   sudo useradd --system --shell /usr/sbin/nologin --home /opt/folicular folicular
   sudo install -d -m 0750 -o folicular -g folicular /opt/folicular
   sudo install -d -m 0750 -o folicular -g folicular /opt/folicular/data
   sudo install -m 0755 -o folicular -g folicular bin/folicular /opt/folicular/folicular
   ```

3. Create the systemd service unit `/etc/systemd/system/folicular.service`:

   ```ini
   [Unit]
   Description=folicular (Luteal backend)
   After=network.target

   [Service]
   Type=simple
   User=folicular
   Group=folicular
   WorkingDirectory=/opt/folicular
   Environment=FOLICULAR_ADDR=127.0.0.1:8080
   Environment=FOLICULAR_DB_PATH=/opt/folicular/data/folicular.db
   Environment=FOLICULAR_LOG_LEVEL=info
   Environment=FOLICULAR_PAIRING_BASE_URL=https://folicular.example.com
   ExecStart=/opt/folicular/folicular
   Restart=on-failure
   RestartSec=3

   # Systemd Hardening
   NoNewPrivileges=true
   ProtectSystem=strict
   ProtectHome=true
   PrivateTmp=true
   ReadWritePaths=/opt/folicular/data

   [Install]
   WantedBy=multi-user.target
   ```

4. Enable and start the service:

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl enable --now folicular
   sudo systemctl status folicular
   ```

5. Reverse-proxy `127.0.0.1:8080` using Caddy or Nginx with TLS.

---

## Configuration Reference

folicular is configured entirely through environment variables:

| Variable | Default | Description |
|---|---|---|
| `FOLICULAR_ADDR` | `:8080` | TCP network address to bind (e.g. `:8080` or `127.0.0.1:8080`). |
| `FOLICULAR_DB_PATH` | `./folicular.db` | Filesystem path for the SQLite database file and WAL logs. |
| `FOLICULAR_LOG_LEVEL` | `info` | Logging verbosity: `debug`, `info`, `warn`, `error`. Logs are structured JSON. |
| `FOLICULAR_INVITE_CODES` | *(empty)* | Comma-separated list of invite codes required for account registration. **Warning:** current Luteal builds cannot send an invite code; setting this will cause app registrations to fail with 401. Leave empty (registration open) until a client version supports the field. |
| `FOLICULAR_PAIRING_BASE_URL` | *(empty)* | Public base URL used to construct Duo partner invite links and QR codes. |
| `FOLICULAR_TRUSTED_PROXIES` | `127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16` | Comma-separated IP addresses or CIDR blocks of reverse proxies trusted for `X-Forwarded-For`. |

### Invite Codes (Registration Gate)
To prevent unauthorized users from registering accounts on your server:
1. Set `FOLICULAR_INVITE_CODES=CODE1,CODE2,CODE3` in your environment.
2. Only SHA-256 hashes of the codes are held in memory.
3. During registration (`POST /v1/auth/register`), clients must supply a matching invite code.
4. When left empty, registration is open to anyone with network access to the server.

> **Note on Android client:** Current Luteal builds cannot send an invite code. If you set `FOLICULAR_INVITE_CODES`, registrations from the mobile app will fail with HTTP 401. Leave this variable empty to keep registration open until a client release introduces the field.

---

## Backups & Maintenance

SQLite in WAL mode produces three files: `folicular.db`, `folicular.db-wal`, and `folicular.db-shm`. Do not copy raw files while the database is actively being written to without using SQLite backup tools.

### Option 1: Continuous S3 Replication with Litestream (Recommended)

[Litestream](https://litestream.io/) monitors SQLite WAL changes and replicates them continuously to any S3-compatible bucket (Backblaze B2, Cloudflare R2, MinIO, AWS S3, etc.).

Litestream configuration (`litestream.yml`):
```yaml
dbs:
  - path: /data/folicular.db
    replicas:
      - url: s3://my-backup-bucket/folicular-db
        sync-interval: 10s
```

Run Litestream as a sidecar container or background service on the same volume as `/data`.

### Option 2: Periodic SQLite Online Backup Script

You can use the `sqlite3` CLI tool on the host to take consistent live snapshots:

```sh
#!/usr/bin/env bash
set -euo pipefail

BACKUP_DIR="/opt/folicular/backups"
mkdir -p "$BACKUP_DIR"

TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
TARGET="$BACKUP_DIR/folicular_${TIMESTAMP}.db"

# Safe online backup via SQLite API
sqlite3 /opt/folicular/data/folicular.db ".backup '$TARGET'"

# Keep last 14 days of backups
find "$BACKUP_DIR" -name "folicular_*.db" -type f -mtime +14 -delete
```

Add this script to cron (`crontab -e`):
```cron
0 2 * * * /usr/local/bin/backup-folicular.sh > /dev/null 2>&1
```

### Upgrades and Migrations

folicular embeds all database migrations inside the Go binary.
- To upgrade, pull the latest container image (`ghcr.io/joerikaiser/folicular:latest`) or replace the binary, and restart the process.
- Database migrations are automatically applied idempotently on startup.

---

## Connecting the Luteal F-Droid Android App

Once your server is running and accessible over HTTPS, connect the Android application:

1. **Open Luteal**: Launch the Luteal app on your Android device.
2. **Navigate to Synchronisation Settings**:
   - Go to **Paramètres** → **Synchronisation** (*Settings* → *Synchronisation*).
3. **Configure Server URL**:
   - Set **URL du serveur** (*Server URL*) to your domain:
     `https://folicular.example.com` (or your Tailscale HTTPS URL).
4. **Connect / Register**:
   - Tap to create your account. Luteal will generate your random anonymous account code and device keys, then perform the initial sync.
   - *Note on invite codes:* Current Luteal builds cannot send an invite code; if you set `FOLICULAR_INVITE_CODES` on your server, app registrations will fail with 401. Leave it empty (registration open) until a client version supports the field.

### Verifying Server Connectivity

You can test that your server is reachable before configuring the app:

```sh
# Health liveness check
curl -i https://folicular.example.com/healthz
# Response: HTTP 200 {"status":"ok"}

# Database readiness check
curl -i https://folicular.example.com/readyz
# Response: HTTP 200 {"status":"ready"}

# Version check
curl -i https://folicular.example.com/version
# Response: HTTP 200 {"version":"..."}
```

### Duo Partner Pairing

Luteal supports Duo partner sharing and support requests:
- When a user generates an invitation link in the app, the link is constructed using `FOLICULAR_PAIRING_BASE_URL` (e.g. `https://folicular.example.com/accept?code=...`).
- When the partner opens this link or scans the pairing QR code, their Luteal app connects to the instance to establish the cryptographic Duo link.
- Ensure `FOLICULAR_PAIRING_BASE_URL` matches the public HTTPS domain configured in your reverse proxy.

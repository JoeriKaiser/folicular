# folicular

Backend service for **Luteal**, the French-first, private menstrual cycle tracker
and consensual Duo companion. folicular provides pseudonymised account identity,
offline-first delta synchronization with end-to-end encrypted records for the Android client,
and consensual Duo pairing and support requests.


## Principles

- **The backend is the source of truth for data.** Canonical schema, validation
  rules, conflict resolution, and computed estimates live here. Clients may
  cache for offline display but must conform to this contract.
- **Observations are not diagnoses.** The server stores self-reported
  observations and serves estimates with explicit uncertainty. It never
  screens for, infers, or announces a medical condition.
- **Anonymous by design.** No email, no OAuth, no phone number. An account is a
  high-entropy code (Mullvad-style) plus per-device tokens.
- **French first.** Server-authored copy and default locale are French.
- **Research-backed structure.** Every domain constant and enum traces to a
  source in `docs/research/SOURCES.md`.

## Stack

| Concern       | Choice                                             |
|---------------|----------------------------------------------------|
| Language      | Go 1.25+                                           |
| HTTP routing  | [chi](https://github.com/go-chi/chi) v5            |
| Queries       | [sqlc](https://sqlc.dev) (compile-time checked SQL)|
| Database      | SQLite via [modernc.org/sqlite](https://modernc.org/sqlite) (pure Go, no cgo) |
| Migrations    | [golang-migrate](https://github.com/golang-migrate/migrate) (embedded, runs at boot) |
| Logging       | `log/slog` (structured, JSON)                      |
| Rate limiting | `golang.org/x/time/rate`                           |
| Errors        | RFC 9457 `application/problem+json`                |

## Self-Hosting

folicular is packaged for effortless self-hosting on x86_64 and ARM64 (Raspberry Pi, VPS, homelab). Prebuilt multi-arch images are published to GitHub Container Registry (`ghcr.io/joerikaiser/folicular:latest`).

### Standalone Quickstart (Docker Compose)

```sh
curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/docker-compose.selfhost.yml
curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/.env.example
cp .env.example .env
docker compose -f docker-compose.selfhost.yml up -d
```

### Turnkey Automated HTTPS (Docker Compose + Caddy)

```sh
curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/docker-compose.caddy.yml
curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/Caddyfile
curl -sSL -O https://raw.githubusercontent.com/JoeriKaiser/folicular/main/.env.example
cp .env.example .env
# Set DOMAIN=folicular.example.com in .env
docker compose -f docker-compose.caddy.yml up -d
```

For reverse proxy integration (Traefik, Nginx Proxy Manager), homelab / Tailscale private networking, Android TLS requirements, backups (Litestream), and step-by-step Luteal F-Droid app configuration, see the [Self-Hosting Guide](docs/self-hosting.md).

## Development

```sh
make tidy        # fetch dependencies
make sqlc        # regenerate query code (requires sqlc on PATH)
make test        # includes the OpenAPI contract tests
make run         # boots on :8080 with ./folicular.db
```

Smoke test the core flow:

```sh
make smoke
```

### Local Docker Compose

A containerized local development setup is provided:

```sh
docker compose up -d --build        # or: make compose-up
docker compose logs -f folicular    # follow the structured JSON logs
docker compose ps                   # health status
docker compose down                 # stop, keep the database volume
docker compose down -v              # stop and wipe the database
```

The container listens on 8080 internally and is published to the host on
`${FOLICULAR_HOST_PORT:-8080}`. If 8080 is already taken on your machine:

```sh
FOLICULAR_HOST_PORT=18080 docker compose up -d --build
# or: make compose-up FOLICULAR_HOST_PORT=18080
```

For an Android device on the same host, reach the server via
`adb reverse tcp:8080 tcp:<host port>` (then `http://127.0.0.1:8080` on the
device) or the host's LAN IP; the emulator uses `http://10.0.2.2:8080`.

## Documentation

- [`docs/self-hosting.md`](docs/self-hosting.md) - comprehensive guide for self-hosters and F-Droid users (Docker Compose, Caddy, Tailscale, backups, app connection)
- [`docs/deployment.md`](docs/deployment.md) - production deployment on Coolify (with a bare-metal fallback), backups, and the F-Droid-aware security posture
- [`AGENTS.md`](AGENTS.md) - agent and contributor rules for this repository
- [`docs/architecture.md`](docs/architecture.md) - system design and decisions
- [`docs/api.md`](docs/api.md) - HTTP API contract prose (auth, sync, reads, Duo)
- [`openapi/openapi.yaml`](openapi/openapi.yaml) - machine-readable contract (client DTOs are generated from it; guarded by `internal/contract` tests)
- [`docs/data-model.md`](docs/data-model.md) - schema rationale, research mapping
- [`docs/research/SOURCES.md`](docs/research/SOURCES.md) - source register
- `docs/research/0*.md` - topic notes linking research to schema decisions
## Layout

```
cmd/server/            entrypoint: config, DB open + migrate, graceful shutdown
openapi/               OpenAPI 3.0 spec - the wire contract's single source of truth
internal/config/       environment configuration
internal/contract/     tests guarding the OpenAPI spec
internal/db/           connection, pragmas, embedded migrations, sqlc queries + generated code (dbgen/)
internal/auth/         account codes, device tokens, auth middleware
internal/domain/       canonical domain types and validation
internal/api/          HTTP handlers (RFC 9457 errors)
internal/server/       chi router wiring and middleware
scripts/               smoke test
```

## Related repositories

- Android client: [Luteal](https://github.com/JoeriKaiser/luteal) (package `fr.luteal`)

## Licence

GNU Affero General Public License v3.0 or later. See [LICENSE](LICENSE).

folicular is free software: you can redistribute it and/or modify it under the
terms of the GNU Affero General Public License as published by the Free Software
Foundation, either version 3 of the License, or (at your option) any later
version. It is distributed in the hope that it will be useful, but WITHOUT ANY
WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.


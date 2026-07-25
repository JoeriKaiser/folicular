# folicular backend image.
#
# Pure-Go build (modernc.org/sqlite, no CGO) -> fully static binary, so the
# runtime stage needs no libc. Multi-stage: compile with the Go toolchain,
# run on a minimal Alpine base as a non-root user.
#
#   docker compose up -d --build      # build + start
#   docker compose logs -f folicular  # follow the structured JSON logs

# --- build stage -----------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src
# Cache module downloads separately from source so code changes rebuild fast.
COPY go.mod go.sum ./
RUN go mod download
# Copy the module source. internal/db/migrations/*.sql is embedded via
# go:embed and must be present at build time (kept by .dockerignore).
COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -trimpath \
    -o /out/folicular ./cmd/server

# --- runtime stage ---------------------------------------------------------
FROM alpine:3.20
# Fixed uid/gid so the named volume ownership is deterministic.
RUN addgroup -g 10001 luteal && adduser -S -u 10001 -G luteal luteal \
    && mkdir -p /data && chown -R 10001:10001 /data
WORKDIR /data
COPY --from=build --chown=10001:10001 /out/folicular /usr/local/bin/folicular
USER luteal
ENV FOLICULAR_ADDR=:8080 \
    FOLICULAR_DB_PATH=/data/folicular.db \
    FOLICULAR_LOG_LEVEL=info
EXPOSE 8080
# busybox wget ships with Alpine; /healthz is a no-DB liveness probe.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/usr/local/bin/folicular"]

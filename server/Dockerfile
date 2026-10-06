# Sparkbase server: API + worker, plus the CLI.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/sparkbase-server ./cmd/sparkbase-server \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/sparkbase ./cmd/sparkbase

# The Postgres image ships pg_dump and pg_restore matching the data hosts,
# which backups need.
FROM postgres:18-alpine
RUN apk add --no-cache ca-certificates tzdata && mkdir -p /var/lib/sparkbase/backups /var/lib/sparkbase/storage \
 && chown -R postgres:postgres /var/lib/sparkbase
COPY --from=build /out/sparkbase-server /out/sparkbase /usr/local/bin/
USER postgres
ENV PORT=8787 BACKUP_DIR=/var/lib/sparkbase/backups
EXPOSE 8787
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD wget -qO- http://127.0.0.1:8787/health >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/sparkbase-server"]

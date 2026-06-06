#!/usr/bin/env sh
set -eu

BACKUP_DIR="${BACKUP_DIR:-./backups/postgres}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"
POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
POSTGRES_USER="${POSTGRES_USER:-brevyn}"
POSTGRES_DB="${POSTGRES_DB:-brevyn_cloud}"

mkdir -p "$BACKUP_DIR"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_file="$BACKUP_DIR/brevyn-cloud-$timestamp.dump"

compose_postgres_id=""
if command -v docker >/dev/null 2>&1; then
  compose_postgres_id="$(docker compose ps -q "$POSTGRES_SERVICE" 2>/dev/null || true)"
fi

if [ -n "${DATABASE_URL:-}" ] && command -v pg_dump >/dev/null 2>&1; then
  pg_dump --format=custom --no-owner --no-acl --file="$backup_file" "$DATABASE_URL"
elif [ -n "$compose_postgres_id" ]; then
  docker compose exec -T "$POSTGRES_SERVICE" \
    pg_dump --format=custom --no-owner --no-acl -U "$POSTGRES_USER" -d "$POSTGRES_DB" > "$backup_file"
else
  echo "pg_dump not found and docker compose postgres service is unavailable" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$backup_file" > "$backup_file.sha256"
elif command -v shasum >/dev/null 2>&1; then
  shasum -a 256 "$backup_file" > "$backup_file.sha256"
fi

find "$BACKUP_DIR" -type f \( -name 'brevyn-cloud-*.dump' -o -name 'brevyn-cloud-*.dump.sha256' \) \
  -mtime +"$BACKUP_RETENTION_DAYS" -delete

echo "$backup_file"

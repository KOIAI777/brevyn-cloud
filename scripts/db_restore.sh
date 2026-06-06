#!/usr/bin/env sh
set -eu

if [ "${ALLOW_RESTORE:-}" != "1" ]; then
  echo "Refusing to restore. Set ALLOW_RESTORE=1 to confirm this destructive operation." >&2
  exit 1
fi

BACKUP_FILE="${BACKUP_FILE:-}"
POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
POSTGRES_USER="${POSTGRES_USER:-brevyn}"
POSTGRES_DB="${POSTGRES_DB:-brevyn_cloud}"

if [ -z "$BACKUP_FILE" ]; then
  echo "BACKUP_FILE is required" >&2
  exit 1
fi

if [ ! -f "$BACKUP_FILE" ]; then
  echo "Backup file not found: $BACKUP_FILE" >&2
  exit 1
fi

compose_postgres_id=""
if command -v docker >/dev/null 2>&1; then
  compose_postgres_id="$(docker compose ps -q "$POSTGRES_SERVICE" 2>/dev/null || true)"
fi

if [ -n "${DATABASE_URL:-}" ] && command -v pg_restore >/dev/null 2>&1; then
  pg_restore --clean --if-exists --no-owner --no-acl --dbname="$DATABASE_URL" "$BACKUP_FILE"
elif [ -n "$compose_postgres_id" ]; then
  docker compose exec -T "$POSTGRES_SERVICE" \
    pg_restore --clean --if-exists --no-owner --no-acl -U "$POSTGRES_USER" -d "$POSTGRES_DB" < "$BACKUP_FILE"
else
  echo "pg_restore not found and docker compose postgres service is unavailable" >&2
  exit 1
fi

echo "restore complete"

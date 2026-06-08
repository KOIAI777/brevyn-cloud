#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
BRANCH="${BRANCH:-main}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:4000/readyz}"
LOCK_FILE="${LOCK_FILE:-/tmp/brevyn-cloud-update.lock}"
COMPOSE="${COMPOSE:-docker compose}"
UPDATE_MODE="${UPDATE_MODE:-image}"
IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-ghcr.io/koiai777/brevyn-cloud}"
POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
POSTGRES_USER="${POSTGRES_USER:-brevyn}"
POSTGRES_DB="${POSTGRES_DB:-brevyn_cloud}"

log() {
  printf '[brevyn-cloud-update] %s\n' "$*"
}

cd "$APP_DIR"

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  log "another update is already running"
  exit 1
fi

if [ ! -f ".env" ]; then
  log "missing .env in $APP_DIR"
  exit 1
fi

OLD_COMMIT="$(git rev-parse HEAD)"
log "current commit: $OLD_COMMIT"

image_for_commit() {
  printf '%s:sha-%s' "$IMAGE_REPOSITORY" "$(git rev-parse --short=7 "$1")"
}

OLD_IMAGE="$(image_for_commit "$OLD_COMMIT")"

backup_database() {
  if ! $COMPOSE ps --status running --services | grep -qx "$POSTGRES_SERVICE"; then
    log "postgres is not running yet; skip backup"
    return 0
  fi
  mkdir -p backups/manual
  local backup_path="backups/manual/brevyn_cloud_$(date +%Y%m%d_%H%M%S).sql"
  log "backup database to $backup_path"
  $COMPOSE exec -T "$POSTGRES_SERVICE" pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" > "$backup_path"
}

wait_for_health() {
  log "waiting for $HEALTH_URL"
  for _ in $(seq 1 60); do
    if curl -fsS "$HEALTH_URL" >/dev/null; then
      log "health check passed"
      return 0
    fi
    sleep 2
  done
  return 1
}

rollback() {
  log "rolling back to $OLD_COMMIT"
  git reset --hard "$OLD_COMMIT"
  if [ "$UPDATE_MODE" = "build" ]; then
    $COMPOSE up -d --build
  else
    export BREVYN_CLOUD_IMAGE="$OLD_IMAGE"
    log "restart previous image: $BREVYN_CLOUD_IMAGE"
    $COMPOSE pull api migrate worker || log "previous image pull failed; trying local cached image"
    $COMPOSE up -d
  fi
}

log "fetch latest code"
git fetch origin "$BRANCH"
git merge --ff-only "origin/$BRANCH"
NEW_COMMIT="$(git rev-parse HEAD)"

log "validate compose"
$COMPOSE config --quiet

backup_database

if [ "$UPDATE_MODE" = "build" ]; then
  log "build and restart services"
  $COMPOSE up -d --build
else
  export BREVYN_CLOUD_IMAGE="$(image_for_commit "$NEW_COMMIT")"
  log "pull GHCR image and restart services: $BREVYN_CLOUD_IMAGE"
  $COMPOSE pull api migrate worker
  $COMPOSE up -d
fi

if wait_for_health; then
  log "update succeeded: $(git rev-parse HEAD)"
  exit 0
fi

log "health check failed"
rollback
log "rollback finished; inspect logs with: $COMPOSE logs -f api worker"
exit 1

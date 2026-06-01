#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:4000}"
REQUESTED_TOTAL="${TOTAL:-}"
TOTAL="${REQUESTED_TOTAL:-20}"
CONCURRENCY="${CONCURRENCY:-10}"
MODE="${1:-help}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

run_parallel() {
  local label="$1"
  local command_file="$2"
  echo "== $label =="
  xargs -P "$CONCURRENCY" -I{} sh -c "$(cat "$command_file")" < <(seq 1 "$TOTAL") \
    | sort | uniq -c
}

case "$MODE" in
  register)
    stamp="$(date +%s)"
    password="${TEST_PASSWORD:-BrevynLoadTest@2026}"
    command_file="$tmpdir/register.sh"
    cat >"$command_file" <<EOF
email="loadtest+$stamp-{}@example.com"
curl -sS -o /dev/null -w "%{http_code}\n" \
  -H 'Content-Type: application/json' \
  -d "{\\"email\\":\\"\$email\\",\\"password\\":\\"$password\\",\\"displayName\\":\\"loadtest-{}\\"}" \
  "$BASE_URL/api/v1/auth/register"
EOF
    run_parallel "concurrent register TOTAL=$TOTAL CONCURRENCY=$CONCURRENCY" "$command_file"
    ;;

  admin-login-fail)
    : "${ADMIN_EMAIL:?Set ADMIN_EMAIL for admin-login-fail}"
    command_file="$tmpdir/admin_login_fail.sh"
    cat >"$command_file" <<EOF
curl -sS -o /dev/null -w "%{http_code}\n" \
  -H 'Content-Type: application/json' \
  -d "{\\"email\\":\\"$ADMIN_EMAIL\\",\\"password\\":\\"wrong-password-{}\\"}" \
  "$BASE_URL/api/v1/admin/auth/login"
EOF
    run_parallel "admin failed login limiter TOTAL=$TOTAL CONCURRENCY=$CONCURRENCY" "$command_file"
    ;;

  admin-sync-lock)
    : "${ADMIN_EMAIL:?Set ADMIN_EMAIL for admin-sync-lock}"
    : "${ADMIN_PASSWORD:?Set ADMIN_PASSWORD for admin-sync-lock}"
    cookie="$tmpdir/admin.cookie"
    curl -sS -c "$cookie" -o /dev/null \
      -H 'Content-Type: application/json' \
      -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" \
      "$BASE_URL/api/v1/admin/auth/login"
    TOTAL="${REQUESTED_TOTAL:-2}"
    command_file="$tmpdir/admin_sync_models.sh"
    cat >"$command_file" <<EOF
curl -sS -b "$cookie" -o /dev/null -w "%{http_code}\n" \
  -H 'Content-Type: application/json' \
  -d '{"auditReason":"concurrency smoke test"}' \
  "$BASE_URL/api/v1/admin/sub2api/sync-models"
EOF
    run_parallel "admin sync-models lock TOTAL=$TOTAL CONCURRENCY=$CONCURRENCY" "$command_file"
    ;;

  *)
    cat <<EOF
Usage:
  BASE_URL=http://127.0.0.1:4000 TOTAL=20 CONCURRENCY=10 $0 register
  ADMIN_EMAIL=... TOTAL=12 CONCURRENCY=12 $0 admin-login-fail
  ADMIN_EMAIL=... ADMIN_PASSWORD=... TOTAL=2 CONCURRENCY=2 $0 admin-sync-lock

这个 smoke 脚本只统计 HTTP 状态码。register 这类会写数据的模式，
建议只在一次性本地库里跑。
EOF
    ;;
esac

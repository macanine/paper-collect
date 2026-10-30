#!/usr/bin/env bash
set -Eeuo pipefail

: "${APP_IMAGE:?APP_IMAGE is required}"
: "${RAPIDOCR_IMAGE:?RAPIDOCR_IMAGE is required}"

ENV_FILE="${ENV_FILE:-.env}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"

command -v docker >/dev/null
docker compose version >/dev/null
test -f "$ENV_FILE"
test -f "$COMPOSE_FILE"

compose=(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE")

read_env() {
  local key="$1"
  awk -v key="$key" 'index($0, key "=") == 1 { print substr($0, length(key) + 2); exit }' "$ENV_FILE"
}

write_env() {
  local key="$1"
  local value="$2"
  local tmp
  tmp="$(mktemp "${ENV_FILE}.XXXXXX")"
  awk -v key="$key" -v value="$value" '
    BEGIN { replaced = 0 }
    index($0, key "=") == 1 { print key "=" value; replaced = 1; next }
    { print }
    END { if (!replaced) print key "=" value }
  ' "$ENV_FILE" > "$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$ENV_FILE"
}

old_app_image="$(read_env APP_IMAGE)"
old_rapidocr_image="$(read_env RAPIDOCR_IMAGE)"

rollback() {
  local status=$?
  trap - ERR
  echo "Deployment failed; restoring the previous images." >&2
  if [[ -n "$old_app_image" ]]; then
    write_env APP_IMAGE "$old_app_image"
  fi
  if [[ -n "$old_rapidocr_image" ]]; then
    write_env RAPIDOCR_IMAGE "$old_rapidocr_image"
  fi
  if [[ -n "$old_app_image" && -n "$old_rapidocr_image" ]]; then
    "${compose[@]}" up -d --remove-orphans --no-build || true
  fi
  exit "$status"
}
trap rollback ERR

write_env APP_IMAGE "$APP_IMAGE"
write_env RAPIDOCR_IMAGE "$RAPIDOCR_IMAGE"

"${compose[@]}" pull app-1 app-2 migrate rapidocr
"${compose[@]}" up -d --remove-orphans --no-build

healthy=0
for _ in $(seq 1 30); do
  if "${compose[@]}" exec -T gateway wget -q -O /dev/null http://127.0.0.1:8080/healthz; then
    healthy=1
    break
  fi
  sleep 2
done

if [[ "$healthy" -ne 1 ]]; then
  echo "Health check did not recover within 60 seconds." >&2
  "${compose[@]}" ps >&2 || true
  "${compose[@]}" logs --tail=80 migrate app-1 app-2 gateway >&2 || true
  false
fi

trap - ERR
echo "Deployment healthy: APP_IMAGE=${APP_IMAGE} RAPIDOCR_IMAGE=${RAPIDOCR_IMAGE}"
"${compose[@]}" ps

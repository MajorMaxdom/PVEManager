#!/usr/bin/env bash
# Runs the browser tests against the real pve-manager binary and a mock PVE host.
#   web/e2e/run.sh [playwright args...]     e.g.  web/e2e/run.sh tests/guests.spec.ts --project=desktop
# Needs: go, node/npm, and a Chromium for Playwright (npx playwright install chromium).
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d)
PIDS=()
cleanup() { for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done; rm -rf "$WORK"; }
trap cleanup EXIT

cd "$ROOT"
[ -f web/dist/index.html ] && grep -q "assets/" web/dist/index.html || { echo "==> building frontend"; (cd web/app && npm ci >/dev/null && npm run build >/dev/null); }
echo "==> building pve-manager and mockpve"
go build -o "$WORK/pve-manager" ./cmd/pve-manager
go build -o "$WORK/mockpve" ./cmd/mockpve
go build -o "$WORK/mockidp" ./cmd/mockidp

TOKEN='e2e@pve!t=secret'
"$WORK/mockpve" -addr 127.0.0.1:0 -token "$TOKEN" -dir "$WORK" > "$WORK/mock.out" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 50); do [ "$(wc -l < "$WORK/mock.out")" -ge 2 ] && break; sleep 0.1; done
FP=$(sed -n 1p "$WORK/mock.out"); MOCK=$(sed -n 2p "$WORK/mock.out")
[ -n "$FP" ] && [ -n "$MOCK" ] || { echo "mock did not start"; cat "$WORK/mock.out"; exit 1; }

"$WORK/mockidp" -addr 127.0.0.1:0 -client-id pvm-e2e > "$WORK/idp.out" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 50); do [ -s "$WORK/idp.out" ] && break; sleep 0.1; done
IDP=$(sed -n 1p "$WORK/idp.out"); [ -n "$IDP" ] || { echo "mock IdP did not start"; exit 1; }

PORT=$((20000 + RANDOM % 20000))
mkdir -p "$WORK/data" "$WORK/servers.d"
cat > "$WORK/servers.d/mock.yaml" <<YAML
name: mock
url: https://$MOCK
node: mock1
token: "$TOKEN"
fingerprint: "$FP"
YAML
cat > "$WORK/config.yaml" <<YAML
listen: 127.0.0.1:$PORT
data_dir: $WORK/data
secret_file: $WORK/secret.key
servers_dir: $WORK/servers.d
hostname: localhost
tls: {mode: selfsigned}
oidc:
  issuer: $IDP
  redirect_url: https://127.0.0.1:$PORT/api/oidc/callback
  client_id: pvm-e2e
  client_secret_file: $WORK/oidc.secret
  admin_group: pve-admins
  operator_group: pve-ops
  default_role: none
  button_label: "Mit Test-SSO anmelden"
YAML
printf 'e2e-secret\n' > "$WORK/oidc.secret"
export PVM_E2E_IDP="$IDP"
export PVM_E2E_PASSWORD=${PVM_E2E_PASSWORD:-e2e-password-123}
printf '%s\n' "$PVM_E2E_PASSWORD" | "$WORK/pve-manager" user add e2e-admin -role admin -config "$WORK/config.yaml" >/dev/null
"$WORK/pve-manager" serve -config "$WORK/config.yaml" > "$WORK/manager.out" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 50); do curl -sk -o /dev/null "https://127.0.0.1:$PORT/api/me" && break; sleep 0.2; done
curl -sk -o /dev/null "https://127.0.0.1:$PORT/api/me" || { echo "manager did not start"; cat "$WORK/manager.out"; exit 1; }

export PVM_E2E_URL="https://127.0.0.1:$PORT"
echo "==> running tests against $PVM_E2E_URL (mock PVE at $MOCK)"
cd web/e2e
[ -d node_modules ] || npm ci >/dev/null 2>&1 || npm install >/dev/null
npx playwright test "$@" || { rc=$?; echo "--- manager log (tail)"; tail -n 15 "$WORK/manager.out"; exit $rc; }

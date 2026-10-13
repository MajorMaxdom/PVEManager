#!/usr/bin/env bash
# Starts pve-manager against three MOCK Proxmox hosts with made-up data - no Proxmox needed.
#   web/e2e/demo.sh              serve the demo until Ctrl+C, then clean up
#   web/e2e/demo.sh screenshots  take the screenshots for docs/screenshots and exit
# Demo login: admin / demo-password-123
set -euo pipefail
MODE=${1:-serve}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d)
PIDS=()
cleanup() { for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done; rm -rf "$WORK"; }
trap cleanup EXIT

cd "$ROOT"
[ -f web/dist/index.html ] || { echo "==> building frontend"; (cd web/app && npm ci >/dev/null && npm run build >/dev/null); }
echo "==> building pve-manager and the mock host"
go build -o "$WORK/pve-manager" ./cmd/pve-manager
go build -o "$WORK/mockpve" ./cmd/mockpve

TOKEN='demo@pve!t=demo-secret'
mkdir -p "$WORK/data" "$WORK/servers.d"
for n in 1 2 3; do
  mkdir -p "$WORK/m$n"
  "$WORK/mockpve" -addr 127.0.0.1:0 -node "pve$n" -demo "$n" -token "$TOKEN" -dir "$WORK/m$n" > "$WORK/m$n.out" 2>&1 &
  PIDS+=($!)
done
for n in 1 2 3; do
  for _ in $(seq 1 50); do [ "$(wc -l < "$WORK/m$n.out")" -ge 2 ] && break; sleep 0.1; done
  FP=$(sed -n 1p "$WORK/m$n.out"); ADDR=$(sed -n 2p "$WORK/m$n.out")
  [ -n "$FP" ] && [ -n "$ADDR" ] || { echo "mock host $n did not start"; cat "$WORK/m$n.out"; exit 1; }
  cat > "$WORK/servers.d/pve$n.yaml" <<YAML
name: pve$n
url: https://$ADDR
node: pve$n
token: "$TOKEN"
fingerprint: "$FP"
YAML
done

PORT=${PVM_DEMO_PORT:-$((20000 + RANDOM % 20000))}
cat > "$WORK/config.yaml" <<YAML
listen: 127.0.0.1:$PORT
data_dir: $WORK/data
secret_file: $WORK/secret.key
servers_dir: $WORK/servers.d
hostname: localhost
tls: {mode: selfsigned}
YAML
printf 'demo-password-123\n' | "$WORK/pve-manager" user add admin -role admin -config "$WORK/config.yaml" >/dev/null
"$WORK/pve-manager" serve -config "$WORK/config.yaml" > "$WORK/manager.out" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 50); do curl -sk -o /dev/null "https://127.0.0.1:$PORT/api/me" && break; sleep 0.2; done
URL="https://127.0.0.1:$PORT"

if [ "$MODE" = serve ]; then
  echo
  echo "Demo is running (three mock hosts, nothing real is touched):"
  echo "  $URL   (self-signed certificate: accept the browser warning)"
  echo "  login: admin / demo-password-123"
  echo "Press Ctrl+C to stop."
  wait "${PIDS[-1]}"
else
  cd web/e2e
  [ -d node_modules ] || npm ci >/dev/null
  PVM_DEMO_URL="$URL" PVM_SHOT_DIR="$ROOT/docs/screenshots" node screenshots.mjs
fi

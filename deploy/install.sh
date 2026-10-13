#!/usr/bin/env bash
# install.sh - install (or upgrade) pve-manager as a systemd service.
#
#   sudo ./install.sh                         interactive
#   sudo ./install.sh --config install.conf   unattended (see install.example.conf)
#
# Options:
#   --config FILE   KEY=VALUE settings (see install.example.conf)
#   --binary FILE   pve-manager binary to install (default: ./pve-manager or ../bin/pve-manager)
#   --no-metrics    do not enable the Prometheus endpoint /metrics (bearer token in /etc/pve-manager/metrics.token)
#   --reconfigure   rewrite /etc/pve-manager/config.yaml even if it exists
#   --yes           never prompt; use config values and defaults
#   --dry-run       show what would be done, change nothing
#   -h, --help
# Re-running upgrades the binary and unit and keeps config, data and the secret key.
set -euo pipefail

PREFIX_BIN=/usr/local/bin/pve-manager
ETC=/etc/pve-manager
DATA=/var/lib/pve-manager
UNIT=/etc/systemd/system/pve-manager.service
SVC_USER=pvemanager

die() { echo "error: $*" >&2; exit 1; }
log() { echo "==> $*"; }
run() { if [ "$DRY" = 1 ]; then echo "[dry-run] $*"; else "$@"; fi; }

CONFIG="" BINARY="" RECONF=0 YES=0 DRY=0 METRICS=1
while [ $# -gt 0 ]; do
  case $1 in
    --config) CONFIG=${2:?}; shift 2 ;;
    --binary) BINARY=${2:?}; shift 2 ;;
    --no-metrics) METRICS=0; shift ;;
    --reconfigure) RECONF=1; shift ;;
    --yes|-y) YES=1; shift ;;
    --dry-run) DRY=1; shift ;;
    -h|--help) sed -n '2,/^set -e/p' "$0" | sed '$d;s/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

[ "$DRY" = 1 ] || [ "$(id -u)" = 0 ] || die "run as root (sudo ./install.sh)"
command -v systemctl >/dev/null || die "systemd is required"

# ---- settings
LISTEN="" HOSTNAME_FQDN="" TLS_MODE="" TLS_CERT="" TLS_KEY="" ADMIN_USER="" ADMIN_PASSWORD_FILE="" SERVER_FILES=""
if [ -n "$CONFIG" ]; then
  [ -r "$CONFIG" ] || die "cannot read $CONFIG"
  # only accept known KEY=VALUE lines; never source the file as code
  while IFS= read -r line; do
    line=${line%%#*}; [[ $line =~ ^[[:space:]]*([A-Z_]+)=(.*)$ ]] || continue
    k=${BASH_REMATCH[1]}; v=${BASH_REMATCH[2]}; v=${v#\"}; v=${v%\"}
    case $k in LISTEN|HOSTNAME_FQDN|TLS_MODE|TLS_CERT|TLS_KEY|ADMIN_USER|ADMIN_PASSWORD_FILE|SERVER_FILES) printf -v "$k" '%s' "$v" ;;
      *) die "unknown key in $CONFIG: $k" ;; esac
  done < "$CONFIG"
fi

ask() { # ask VAR "prompt" default
  local var=$1 prompt=$2 def=$3 cur=${!1:-}
  [ -n "$cur" ] && return
  if [ "$YES" = 1 ] || [ ! -t 0 ]; then printf -v "$var" '%s' "$def"; return; fi
  read -r -p "$prompt [$def]: " cur; printf -v "$var" '%s' "${cur:-$def}"
}

EXISTING_CFG=0; [ -f "$ETC/config.yaml" ] && [ "$RECONF" = 0 ] && EXISTING_CFG=1
if [ "$EXISTING_CFG" = 1 ]; then # upgrade: take the effective values from the config on disk
  LISTEN=$(sed -n 's/^listen: "\(.*\)"/\1/p' "$ETC/config.yaml")
  [ -n "$LISTEN" ] || die "cannot read listen address from $ETC/config.yaml (use --reconfigure)"
fi
if [ "$EXISTING_CFG" = 0 ]; then
  ask LISTEN "Listen address" "0.0.0.0:8443"
  ask HOSTNAME_FQDN "Hostname (for the certificate)" "$(hostname -f 2>/dev/null || hostname)"
  ask TLS_MODE "TLS mode (selfsigned|files|off)" "selfsigned"
  case $TLS_MODE in selfsigned|off) ;; files) ask TLS_CERT "Certificate file" ""; ask TLS_KEY "Key file" "";
      [ -r "$TLS_CERT" ] && [ -r "$TLS_KEY" ] || die "TLS cert/key not readable" ;;
    *) die "invalid TLS_MODE: $TLS_MODE" ;; esac
  listen_re='^(\[[0-9a-fA-F:]+\]|[A-Za-z0-9._-]*):[0-9]+$'
  [[ $LISTEN =~ $listen_re ]] || die "invalid LISTEN: $LISTEN (expected host:port)"
  [[ $HOSTNAME_FQDN =~ ^[A-Za-z0-9.-]+$ ]] || die "invalid hostname: $HOSTNAME_FQDN"
fi
ask ADMIN_USER "Admin user name" "admin"
[[ $ADMIN_USER =~ ^[A-Za-z0-9._-]+$ ]] || die "invalid admin user"
if [ -z "$SERVER_FILES" ] && [ "$YES" = 0 ] && [ -t 0 ] && [ "$EXISTING_CFG" = 0 ]; then
  read -r -p "PVE server definition files to import (space separated, empty = later): " SERVER_FILES
fi
for f in $SERVER_FILES; do [ -r "$f" ] || die "server file not readable: $f"; done

# ---- binary
if [ -z "$BINARY" ]; then
  here=$(cd "$(dirname "$0")" && pwd)
  for c in ./pve-manager "$here/pve-manager" "$here/../bin/pve-manager"; do [ -x "$c" ] && { BINARY=$c; break; }; done
fi
[ -n "$BINARY" ] && [ -x "$BINARY" ] || die "binary not found; pass --binary FILE"
VER=$("$BINARY" version) || die "$BINARY does not run on this system"
PORT=${LISTEN##*:}

# ---- port check (skipped when upgrading an already running service)
if [ "$EXISTING_CFG" = 0 ] && ! systemctl is-active --quiet pve-manager 2>/dev/null && command -v ss >/dev/null; then
  if ss -tlnH 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]${PORT}\$"; then die "port $PORT is already in use; choose another LISTEN"; fi
fi

log "pve-manager $VER -> $PREFIX_BIN"
[ "$EXISTING_CFG" = 1 ] && log "keeping existing $ETC/config.yaml" || log "listen $LISTEN, tls $TLS_MODE, host $HOSTNAME_FQDN"

# ---- user + dirs
id "$SVC_USER" >/dev/null 2>&1 || run useradd --system --home-dir "$DATA" --shell /usr/sbin/nologin "$SVC_USER"
run install -d -m 0750 -o root -g "$SVC_USER" "$ETC" "$ETC/servers.d" "$ETC/ssh"
run install -d -m 0750 -o "$SVC_USER" -g "$SVC_USER" "$DATA"
run install -m 0755 "$BINARY" "$PREFIX_BIN"

# ---- secret key (never regenerated: it decrypts the stored API tokens)
if [ ! -f "$ETC/secret.key" ]; then
  log "generating master key"
  if [ "$DRY" = 0 ]; then head -c 32 /dev/urandom > "$ETC/secret.key"; chown root:"$SVC_USER" "$ETC/secret.key"; chmod 0640 "$ETC/secret.key"; fi
fi
# ssh key used for optional upgrades over SSH (public part is handed to onboard-pve-host.sh)
if [ ! -f "$ETC/ssh/id_ed25519" ] && command -v ssh-keygen >/dev/null; then
  log "generating SSH key for host upgrades"
  if [ "$DRY" = 0 ]; then ssh-keygen -q -t ed25519 -N '' -C pve-manager -f "$ETC/ssh/id_ed25519"
    chown root:"$SVC_USER" "$ETC/ssh/id_ed25519" "$ETC/ssh/id_ed25519.pub"; chmod 0640 "$ETC/ssh/id_ed25519"; chmod 0644 "$ETC/ssh/id_ed25519.pub"; fi
fi

# ---- config
if [ "$EXISTING_CFG" = 0 ] && [ "$DRY" = 0 ]; then
  umask 027
  {
    echo "# written by install.sh"
    echo "listen: \"$LISTEN\""
    echo "hostname: \"$HOSTNAME_FQDN\""
    echo "data_dir: $DATA"
    echo "secret_file: $ETC/secret.key"
    echo "servers_dir: $ETC/servers.d"
    echo "tls:"; echo "  mode: $TLS_MODE"
    [ "$TLS_MODE" = files ] && { echo "  cert: $TLS_CERT"; echo "  key: $TLS_KEY"; }
  } > "$ETC/config.yaml"
  chown root:"$SVC_USER" "$ETC/config.yaml"; chmod 0640 "$ETC/config.yaml"
fi
# ---- Prometheus token (created once; added to an existing config on upgrade)
METRICS_NEW=""
if [ "$METRICS" = 1 ] && [ "$DRY" = 0 ]; then
  if [ ! -s "$ETC/metrics.token" ]; then
    head -c 32 /dev/urandom | base64 | tr -d '/+=\n' | head -c 40 > "$ETC/metrics.token"
    chown root:"$SVC_USER" "$ETC/metrics.token"; chmod 0640 "$ETC/metrics.token"; METRICS_NEW=$(cat "$ETC/metrics.token")
  fi
  grep -q '^metrics_token_file:' "$ETC/config.yaml" || echo "metrics_token_file: $ETC/metrics.token" >> "$ETC/config.yaml"
elif [ "$METRICS" = 1 ]; then echo "[dry-run] create $ETC/metrics.token and enable /metrics"; fi
for f in $SERVER_FILES; do
  log "importing server $(basename "$f")"
  run install -m 0640 -o root -g "$SVC_USER" "$f" "$ETC/servers.d/$(basename "$f")"
done

# ---- systemd unit
CAP=""; [ "$PORT" -lt 1024 ] && CAP=$'AmbientCapabilities=CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE'
[ -n "$CAP" ] || CAP="CapabilityBoundingSet="
if [ "$DRY" = 0 ]; then
  cat > "$UNIT" <<UNITEOF
[Unit]
Description=PVE Manager
After=network-online.target
Wants=network-online.target

[Service]
User=$SVC_USER
Group=$SVC_USER
ExecStart=$PREFIX_BIN serve -config $ETC/config.yaml
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
ReadWritePaths=$DATA
$CAP

[Install]
WantedBy=multi-user.target
UNITEOF
else echo "[dry-run] write $UNIT"; fi
run systemctl daemon-reload

# ---- admin user (only if there is none yet; re-running never resets a password)
PW_SHOWN=""
as_svc() { runuser -u "$SVC_USER" -- "$@"; }
if [ "$DRY" = 0 ]; then
  if [ -z "$(as_svc "$PREFIX_BIN" user list -config "$ETC/config.yaml")" ]; then
    if [ -n "$ADMIN_PASSWORD_FILE" ]; then PW=$(head -n1 "$ADMIN_PASSWORD_FILE")
    else PW=$(head -c 24 /dev/urandom | base64 | tr -d '/+=' | head -c 20); PW_SHOWN=$PW; fi
    # the binary reads the password from stdin when it is not a terminal
    printf '%s\n' "$PW" | as_svc "$PREFIX_BIN" user add "$ADMIN_USER" -role admin -config "$ETC/config.yaml" >/dev/null \
      || die "could not create admin user (password needs >= 10 characters)"
  else
    log "users already exist - not touching accounts"
  fi
else echo "[dry-run] create admin user $ADMIN_USER if no user exists"; fi

run systemctl enable pve-manager >/dev/null 2>&1
run systemctl restart pve-manager

# ---- health check
if [ "$DRY" = 0 ]; then
  scheme=https; [ "$(sed -n 's/^  mode: //p' "$ETC/config.yaml")" = off ] && scheme=http
  ok=0; for _ in $(seq 1 20); do
    code=$(curl -sk -o /dev/null -w '%{http_code}' "$scheme://127.0.0.1:$PORT/api/me" 2>/dev/null || true)
    [ "$code" = 401 ] && { ok=1; break; }; sleep 0.5
  done
  [ "$ok" = 1 ] || { journalctl -u pve-manager -n 20 --no-pager >&2; die "service did not come up (see log above)"; }
  host=$(sed -n 's/^hostname: "\(.*\)"/\1/p' "$ETC/config.yaml")
  log "pve-manager is running: $scheme://${host:-localhost}:$PORT/"
  if [ -n "$PW_SHOWN" ]; then
    echo; echo "    admin user:     $ADMIN_USER"; echo "    admin password: $PW_SHOWN   (shown once; change later: pve-manager user add $ADMIN_USER -config $ETC/config.yaml)"; echo
  fi
  if [ -n "$METRICS_NEW" ]; then
    echo "    Prometheus:     $scheme://${host:-localhost}:$PORT/metrics   (Authorization: Bearer $METRICS_NEW)"; echo "                    token file: $ETC/metrics.token"; echo
  fi
  echo "Onboard further PVE hosts:  deploy/onboard-pve-host.sh HOST --with-updates   (SSH key: $ETC/ssh/id_ed25519.pub)"
  echo "Uninstall:                  sudo ./uninstall.sh"
fi

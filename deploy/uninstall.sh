#!/usr/bin/env bash
# uninstall.sh - remove pve-manager from this host.
#
#   sudo ./uninstall.sh              removes service + binary; asks before deleting config and data
#   sudo ./uninstall.sh --purge      also deletes /etc/pve-manager, /var/lib/pve-manager and the service user
#   sudo ./uninstall.sh --keep-data  never delete config/data (no prompt)
#
# Options: --purge | --keep-data | --yes | --dry-run | -h
# The PVE hosts are NOT touched. Revoke the access you granted there with:
#   deploy/onboard-pve-host.sh HOST --revoke
set -euo pipefail

BIN=/usr/local/bin/pve-manager
ETC=/etc/pve-manager
DATA=/var/lib/pve-manager
UNIT=/etc/systemd/system/pve-manager.service
SVC_USER=pvemanager

die() { echo "error: $*" >&2; exit 1; }
log() { echo "==> $*"; }
PURGE=0 KEEP=0 YES=0 DRY=0
while [ $# -gt 0 ]; do
  case $1 in
    --purge) PURGE=1; shift ;; --keep-data) KEEP=1; shift ;; --yes|-y) YES=1; shift ;; --dry-run) DRY=1; shift ;;
    -h|--help) sed -n '2,/^set -e/p' "$0" | sed '$d;s/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done
[ "$PURGE" = 1 ] && [ "$KEEP" = 1 ] && die "--purge and --keep-data contradict each other"
run() { if [ "$DRY" = 1 ]; then echo "[dry-run] $*"; else "$@"; fi; }
[ "$DRY" = 1 ] || [ "$(id -u)" = 0 ] || die "run as root (sudo ./uninstall.sh)"

if [ ! -e "$UNIT" ] && [ ! -x "$BIN" ] && [ ! -d "$ETC" ] && [ ! -d "$DATA" ]; then log "pve-manager is not installed"; exit 0; fi

if [ "$PURGE" = 0 ] && [ "$KEEP" = 0 ]; then
  if [ "$YES" = 1 ] || [ ! -t 0 ]; then KEEP=1
  else
    read -r -p "Also delete config and data ($ETC, $DATA: users, audit log, stored PVE tokens)? [y/N] " a
    [[ $a =~ ^[Yy]$ ]] && PURGE=1 || KEEP=1
  fi
fi

if systemctl list-unit-files pve-manager.service >/dev/null 2>&1; then
  log "stopping service"
  run systemctl disable --now pve-manager 2>/dev/null || true
fi
run rm -f "$UNIT" "$BIN"
run systemctl daemon-reload

if [ "$PURGE" = 1 ]; then
  log "deleting $ETC and $DATA"
  run rm -rf "$ETC" "$DATA"
  if id "$SVC_USER" >/dev/null 2>&1; then run userdel "$SVC_USER" 2>/dev/null || true; fi
else
  log "kept $ETC and $DATA (re-installing picks them up again)"
fi

log "pve-manager removed"
echo "The PVE hosts still have the 'pvemanager@pve' user, role ManagerSvc and API token."
echo "Remove them per host with:  deploy/onboard-pve-host.sh HOST --revoke"

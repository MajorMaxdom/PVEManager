#!/usr/bin/env bash
# onboard-pve-host.sh - prepare a Proxmox VE host for pve-manager.
#
# Creates (idempotently) on the target PVE host:
#   * role  ManagerSvc            (least-privilege set, see PRIVS below)
#   * user  pvemanager@pve        + ACL on /
#   * API token  pvemanager@pve!manager  (privilege separation off)
#   * optional (--with-updates): unix user "pvemanager", an SSH key restricted to a
#     single sudo-able upgrade wrapper (the PVE API cannot run apt dist-upgrade)
# and writes a server definition (name, url, token, TLS fingerprint) that
# pve-manager can import.  Nothing is changed on the manager host.
#
# Usage:
#   onboard-pve-host.sh HOST [options]
#   onboard-pve-host.sh --local [options]     # run directly on the PVE host
#
# Options:
#   --name NAME          display name (default: short hostname of HOST)
#   --ssh-user USER      SSH login for onboarding (default: root)
#   --port PORT          PVE API port (default: 8006)
#   --with-updates       enable remote upgrades via restricted SSH user
#   --pubkey FILE        manager SSH public key for --with-updates
#                        (default: /etc/pve-manager/ssh/id_ed25519.pub, else ~/.ssh/id_ed25519.pub)
#   --rotate             replace an existing token (new secret)
#   --update-role        only refresh the privileges of role ManagerSvc (after upgrading pve-manager)
#   --revoke             remove everything this script created on HOST
#   --out FILE           output file (default: ./servers.d/NAME.yaml, mode 0600)
#   --dry-run            print the remote script, change nothing
#   -y, --yes            do not ask for confirmation
#   -h, --help
set -euo pipefail

ROLE=ManagerSvc
PVE_USER=pvemanager@pve
TOKEN_ID=manager
UNIX_USER=pvemanager
WRAPPER=/usr/local/sbin/pve-manager-upgrade
SUDOERS=/etc/sudoers.d/pve-manager
PRIVS="Sys.Audit Sys.Modify Sys.Console VM.Audit VM.PowerMgmt VM.Console VM.Allocate VM.Clone VM.Snapshot VM.Migrate VM.Config.CPU VM.Config.Memory VM.Config.Disk VM.Config.Network VM.Config.Options VM.Config.CDROM VM.Config.HWType VM.Config.Cloudinit VM.Backup VM.Snapshot.Rollback Datastore.Audit Datastore.AllocateSpace Datastore.AllocateTemplate Pool.Audit SDN.Use"

die() { echo "error: $*" >&2; exit 1; }
log() { echo "==> $*" >&2; }

HOST="" NAME="" SSH_USER=root PORT=8006 UPDATES=0 PUBKEY="" ROTATE=0 REVOKE=0 UPDATE_ROLE=0 OUT="" DRY=0 YES=0 LOCAL=0
while [ $# -gt 0 ]; do
  case $1 in
    --name) NAME=${2:?}; shift 2 ;;
    --ssh-user) SSH_USER=${2:?}; shift 2 ;;
    --port) PORT=${2:?}; shift 2 ;;
    --with-updates) UPDATES=1; shift ;;
    --pubkey) PUBKEY=${2:?}; shift 2 ;;
    --rotate) ROTATE=1; shift ;;
    --revoke) REVOKE=1; shift ;;
    --update-role) UPDATE_ROLE=1; shift ;;
    --out) OUT=${2:?}; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    -y|--yes) YES=1; shift ;;
    --local) LOCAL=1; shift ;;
    -h|--help) sed -n '2,/^set -e/p' "$0" | sed '$d;s/^# \{0,1\}//'; exit 0 ;;
    -*) die "unknown option: $1" ;;
    *) [ -z "$HOST" ] || die "unexpected argument: $1"; HOST=$1; shift ;;
  esac
done
[ "$LOCAL" = 1 ] && HOST=${HOST:-$(hostname -f)}
[ -n "$HOST" ] || die "HOST required (or --local); see --help"
[[ $HOST =~ ^[A-Za-z0-9._:-]+$ ]] || die "invalid host: $HOST"
[[ $PORT =~ ^[0-9]+$ ]] || die "invalid port"
NAME=${NAME:-${HOST%%.*}}
[[ $NAME =~ ^[A-Za-z0-9._-]+$ ]] || die "invalid name: $NAME"
OUT=${OUT:-./servers.d/$NAME.yaml}

PUBKEY_TEXT=""
if [ "$UPDATES" = 1 ] && [ "$REVOKE" = 0 ]; then
  if [ -z "$PUBKEY" ]; then
    for f in /etc/pve-manager/ssh/id_ed25519.pub "$HOME/.ssh/id_ed25519.pub"; do
      [ -r "$f" ] && { PUBKEY=$f; break; }
    done
  fi
  [ -n "$PUBKEY" ] && [ -r "$PUBKEY" ] || die "--with-updates needs a readable --pubkey"
  PUBKEY_TEXT=$(head -n1 "$PUBKEY")
  [[ $PUBKEY_TEXT =~ ^(ssh-ed25519|ssh-rsa|ecdsa-sha2-[a-z0-9]+)\ [A-Za-z0-9+/=]+(\ .*)?$ ]] || die "not a valid SSH public key: $PUBKEY"
fi

# ---------------------------------------------------------------- remote part
# Runs as root on the PVE host. Prints machine-readable lines "KEY=value".
remote_script() {
cat <<REMOTE
set -euo pipefail
export LC_ALL=C LANG=C
ROLE='$ROLE'; PVE_USER='$PVE_USER'; TOKEN_ID='$TOKEN_ID'; UNIX_USER='$UNIX_USER'
WRAPPER='$WRAPPER'; SUDOERS='$SUDOERS'; PRIVS='$PRIVS'
UPDATES=$UPDATES; ROTATE=$ROTATE; REVOKE=$REVOKE; UPDATE_ROLE=$UPDATE_ROLE; PUBKEY_TEXT='$PUBKEY_TEXT'
REMOTE
cat <<'REMOTE'
command -v pveum >/dev/null || { echo "ERR=not a Proxmox VE host (pveum missing)"; exit 3; }
if [ "$REVOKE" = 1 ]; then
  pveum user token remove "$PVE_USER" "$TOKEN_ID" 2>/dev/null || true
  pveum acl delete / --users "$PVE_USER" --roles "$ROLE" 2>/dev/null || true
  pveum user delete "$PVE_USER" 2>/dev/null || true
  pveum role delete "$ROLE" 2>/dev/null || true
  rm -f "$SUDOERS" "$WRAPPER"
  if id "$UNIX_USER" >/dev/null 2>&1; then userdel -r "$UNIX_USER" 2>/dev/null || userdel "$UNIX_USER" || true; fi
  echo "REVOKED=1"; exit 0
fi

if [ "$UPDATE_ROLE" = 1 ]; then
  pveum role modify "$ROLE" -privs "$PRIVS" >/dev/null || { echo "ERR=role $ROLE does not exist; onboard the host first"; exit 7; }
  echo "ROLE_UPDATED=1"; exit 0
fi
# role (create or update)
if pveum role list --output-format json | grep -q "\"roleid\" *: *\"$ROLE\""; then
  pveum role modify "$ROLE" -privs "$PRIVS" >/dev/null
else
  pveum role add "$ROLE" -privs "$PRIVS" >/dev/null
fi
# user + ACL
pveum user list --output-format json | grep -q "\"userid\" *: *\"$PVE_USER\"" \
  || pveum user add "$PVE_USER" --comment "pve-manager service account" >/dev/null
pveum acl modify / --users "$PVE_USER" --roles "$ROLE" >/dev/null
# token
if pveum user token list "$PVE_USER" --output-format json | grep -q "\"tokenid\" *: *\"$TOKEN_ID\""; then
  if [ "$ROTATE" = 1 ]; then
    pveum user token remove "$PVE_USER" "$TOKEN_ID" >/dev/null
  else
    echo "ERR=token $PVE_USER!$TOKEN_ID already exists; re-run with --rotate to issue a new secret"; exit 4
  fi
fi
SECRET=$(pveum user token add "$PVE_USER" "$TOKEN_ID" --privsep 0 --output-format json \
  | sed -n 's/.*"value" *: *"\([^"]*\)".*/\1/p')
[ -n "$SECRET" ] || { echo "ERR=could not read token secret"; exit 5; }
echo "TOKEN=$PVE_USER!$TOKEN_ID=$SECRET"
echo "NODE=$(hostname)"
echo "PVE_VERSION=$(pveversion | cut -d/ -f2)"
echo "FINGERPRINT=$(openssl x509 -in /etc/pve/local/pve-ssl.pem -noout -fingerprint -sha256 | cut -d= -f2)"

# optional: restricted upgrade path over SSH
if [ "$UPDATES" = 1 ]; then
  id "$UNIX_USER" >/dev/null 2>&1 || useradd -m -s /bin/bash "$UNIX_USER"
  install -d -m 700 -o "$UNIX_USER" -g "$UNIX_USER" "/home/$UNIX_USER/.ssh"
  printf 'restrict,pty %s\n' "$PUBKEY_TEXT" > "/home/$UNIX_USER/.ssh/authorized_keys"
  chown "$UNIX_USER:$UNIX_USER" "/home/$UNIX_USER/.ssh/authorized_keys"; chmod 600 "/home/$UNIX_USER/.ssh/authorized_keys"
  cat > "$WRAPPER" <<'W'
#!/bin/sh
# Installed by pve-manager onboarding. Only entry point sudo allows for the manager.
export DEBIAN_FRONTEND=noninteractive
case "$1" in
  update)  exec apt-get update ;;
  upgrade)
    apt-get -y -o Dpkg::Options::=--force-confold dist-upgrade; rc=$?
    run=$(uname -r); new=$(ls /boot/vmlinuz-* 2>/dev/null | sed 's|/boot/vmlinuz-||' | sort -V | tail -n1)
    if [ -f /var/run/reboot-required ] || { [ -n "$new" ] && [ "$new" != "$run" ]; }; then echo "== REBOOT REQUIRED (running $run, installed ${new:-?}) =="; fi
    exit $rc ;;
  list)    exec apt list --upgradable ;;
  *) echo "usage: $0 update|upgrade|list" >&2; exit 2 ;;
esac
W
  chmod 755 "$WRAPPER"
  printf '%s ALL=(root) NOPASSWD: %s update, %s upgrade, %s list\n' "$UNIX_USER" "$WRAPPER" "$WRAPPER" "$WRAPPER" > "$SUDOERS"
  chmod 440 "$SUDOERS"; visudo -cf "$SUDOERS" >/dev/null || { rm -f "$SUDOERS"; echo "ERR=sudoers validation failed"; exit 6; }
  echo "SSH_USER=$UNIX_USER"
  echo "SSH_HOSTKEY=$(ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub 2>/dev/null | awk '{print $2}')"
fi
REMOTE
}

run_remote() {
  if [ "$LOCAL" = 1 ]; then bash -s
  else ssh -o BatchMode=yes -o ConnectTimeout=10 "$SSH_USER@$HOST" bash -s; fi
}

if [ "$DRY" = 1 ]; then remote_script; exit 0; fi

if [ "$YES" = 0 ]; then
  what=$([ "$REVOKE" = 1 ] && echo "REMOVE pve-manager access from" || echo "create role/user/token on")
  [ "$UPDATES" = 1 ] && [ "$REVOKE" = 0 ] && what="$what (+ restricted SSH upgrade user)"
  read -r -p "This will $what $HOST. Continue? [y/N] " a
  [[ $a =~ ^[Yy]$ ]] || die "aborted"
fi

log "connecting to $HOST"
RESULT=$(remote_script | run_remote 2> >(grep -v -i 'locale\|LC_\|LANG\|are supported' >&2)) || {
  echo "$RESULT" | grep '^ERR=' | sed 's/^ERR=/error: /' >&2
  die "onboarding failed on $HOST"
}

if [ "$REVOKE" = 1 ]; then log "access removed from $HOST (remove the server in pve-manager too)"; exit 0; fi
if [ "$UPDATE_ROLE" = 1 ]; then log "role $ROLE updated on $HOST"; exit 0; fi

val() { printf '%s\n' "$RESULT" | sed -n "s/^$1=//p" | head -n1; }
TOKEN=$(val TOKEN); FP=$(val FINGERPRINT); NODE=$(val NODE); VER=$(val PVE_VERSION); SSHU=$(val SSH_USER); SSHK=$(val SSH_HOSTKEY)
[ -n "$TOKEN" ] && [ -n "$FP" ] || die "incomplete response from $HOST"
[ -z "$SSHU" ] || [ -n "$SSHK" ] || die "$HOST has no ed25519 SSH host key (/etc/ssh/ssh_host_ed25519_key.pub); needed to pin the host"

umask 077
mkdir -p "$(dirname "$OUT")"
{
  echo "# generated by onboard-pve-host.sh on $(date -u +%FT%TZ) - contains a secret, keep 0600"
  echo "name: $NAME"
  echo "url: https://$HOST:$PORT"
  echo "node: $NODE"
  echo "pve_version: \"$VER\""
  echo "token: \"$TOKEN\""
  echo "fingerprint: \"$FP\""
  if [ -n "$SSHU" ]; then
    echo "ssh:"
    echo "  user: $SSHU"
    echo "  host: $HOST"
    echo "  upgrade_command: sudo $WRAPPER upgrade"
    echo "  host_key: \"$SSHK\""
  fi
} > "$OUT"
chmod 600 "$OUT"

log "done: $NAME ($NODE, PVE $VER)"
log "definition written to $OUT (0600)"
log "import:  pve-manager server add --file $OUT     (or copy into /etc/pve-manager/servers.d/)"
log "revoke:  $0 $HOST --revoke"

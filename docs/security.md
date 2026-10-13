# Security model

pve-manager is a web application that can control your hypervisors, so it is worth knowing exactly what it can do and
what protects it. This page describes the design; read the code if you need certainty.

## What the manager can do on a PVE host

Each host is connected with an **API token** of the service user `pvemanager@pve`, created by
`deploy/onboard-pve-host.sh`. The token has privilege separation off, so it carries the rights of the role
`ManagerSvc` on the path `/`:

```
Sys.Audit Sys.Modify Sys.Console
VM.Audit VM.PowerMgmt VM.Console VM.Allocate VM.Clone VM.Snapshot VM.Snapshot.Rollback VM.Migrate VM.Backup
VM.Config.CPU VM.Config.Memory VM.Config.Disk VM.Config.Network VM.Config.Options VM.Config.CDROM VM.Config.HWType VM.Config.Cloudinit
Datastore.Audit Datastore.AllocateSpace Datastore.AllocateTemplate Pool.Audit SDN.Use
```

It is **not** `root@pam`: it cannot change users, permissions, storage definitions, cluster or firewall settings, and it
cannot delete ISO images or templates (`Datastore.Allocate` is deliberately left out). Anyone who controls the manager can,
however, start/stop/create/delete guests and read their consoles on every connected host. Treat the manager like a
hypervisor admin console.

Re-run `onboard-pve-host.sh HOST --update-role` after upgrading if the privilege list grew, and
`--revoke` to remove everything the script created.

## Upgrades over SSH (optional)

Package upgrades cannot be done through the PVE API, so `--with-updates` creates a Linux user `pvemanager` on the host:

- The manager's public key is installed with the options `restrict,pty`: no port or agent forwarding, no X11.
- `sudo` allows exactly three commands for that user: `/usr/local/sbin/pve-manager-upgrade update|upgrade|list`.
  The wrapper runs `apt-get` with fixed arguments and takes no user input.
- The SSH host key of the target is **pinned** (SHA-256 fingerprint stored with the server, ed25519 only). A changed key
  stops the connection instead of asking.

Without `--with-updates` no SSH access exists at all.

## Authentication and sessions

- Passwords: Argon2id (64 MiB, 3 passes), minimum length 10. Failed logins are throttled per client address (8 failures
  in 10 minutes). Unknown users take the same time as wrong passwords.
- Sessions: random 256-bit token in an `HttpOnly`, `SameSite=Strict` cookie (`Secure` when TLS is on). Only a SHA-256
  hash of the token is stored. They last 12 hours, end on logout, and end when the password is changed or the user is
  deleted. A role change applies to live sessions immediately.
- CSRF: every state-changing request with a cookie must carry the custom `X-PVM` header, which browsers will not add
  cross-site without a CORS pre-flight; the server sends no CORS headers.
- Roles: `viewer` (read only), `operator` (control and create guests, back up, upload media), `admin` (everything,
  including users, keys, schedules, notification channels, restores, deletions and configuration changes).
- The last admin cannot be deleted or demoted.
- Response headers: strict `Content-Security-Policy` (`default-src 'self'`, no inline scripts), `X-Frame-Options: DENY`,
  `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`. Fonts and scripts are bundled; the page makes no
  third-party requests.

## API keys

Bearer tokens (`pvm_…`) for scripts, created by an admin. They are shown once and stored as a SHA-256 hash. A key has a
role like a user, needs no CSRF header (it is not sent automatically by browsers), and appears in the audit log as
`key:<name>`. Keys **cannot** reach account administration (users, keys, password change, manager backup): those
endpoints accept a browser session only, so a leaked key cannot create new admins or exfiltrate the master key.

## Single sign-on (OpenID Connect)

Optional. Authorization code flow with PKCE; the ID token is verified by the `go-oidc` library (signature, issuer,
audience, expiry) and the nonce is checked. The `state`/nonce/verifier travel in a short-lived `HttpOnly` cookie that is
HMAC-signed and single-use. The role is derived from the groups claim on every login. An SSO identity can never take over
an existing local account of the same name, and SSO accounts have no usable password. Users without a mapped group are
denied by default (`default_role: none`).

## Secrets at rest

- PVE API tokens, notification credentials and similar secrets are stored **encrypted** (AES-256-GCM) in the SQLite
  database. The key is `/etc/pve-manager/secret.key` (32 random bytes, mode 0640 `root:pvemanager`). Losing the key means
  the stored tokens cannot be read; keep a manager backup.
- The browser never gets a stored secret back. Notification channels show only the host of their URL.
- The service runs as the unprivileged system user `pvemanager` with systemd hardening (`NoNewPrivileges`,
  `ProtectSystem=strict`, private `/tmp`, restricted address families, write access only to `/var/lib/pve-manager`).

## Backups of the manager

`pve-manager backup` (or the download in the UI) produces one file containing the database, the master key, the config,
server definitions, the SSH key and the TLS certificate. It is encrypted with a passphrase (Argon2id + AES-256-GCM, header
authenticated, minimum 12 characters). Restore only writes to the manager's own directories and ignores every archive entry
whose name is not on a fixed list, so a crafted archive cannot write elsewhere. It refuses to replace an existing
installation without `-force` and requires the service to be stopped.

## TLS

The manager serves HTTPS with a self-signed certificate by default, or your own files, or plain HTTP behind a reverse
proxy that terminates TLS. The connection to each PVE host verifies the host's certificate by **fingerprint pinning**
(captured during onboarding), not by trusting a CA and not by skipping verification.

## Audit log

Logins, power actions, creations, deletions, snapshot/backup/restore actions, configuration and account changes,
notification deliveries and scheduled runs are recorded with user, target and outcome (admin page *Audit log*).

## Prometheus endpoint

`/metrics` is off unless a token file is configured. It then requires `Authorization: Bearer <token>` (constant-time
comparison, failures are throttled). Session cookies and API keys do not work there.

## Known limits

- Anyone who can reach the web port can try to log in; put it on a trusted network or behind a VPN/reverse proxy.
- Rate limiting is per client address as seen by the manager; behind a reverse proxy all users share the proxy's address.
- The manager has no second factor of its own; use single sign-on with your identity provider's MFA if you need one.

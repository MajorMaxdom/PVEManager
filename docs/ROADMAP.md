# Roadmap

Status of pve-manager and ideas that are not built yet. Everything under "Done" is covered by Go tests, and the
user-facing parts by the browser tests in `web/e2e` (`make e2e`).

## Done

- Multi-host overview with live state (server-sent events), guest list with filters and bulk actions
- Power actions with confirmation for destructive ones, in-browser console (noVNC) for VMs and containers
- Create VMs and containers, clone, delete (typed-name confirmation), convert to template, cloud-init
- Guest detail: CPU/memory/network/disk graphs, snapshots, backups, restore, resource and disk changes
- Updates: pending packages, refresh, upgrade over a restricted SSH account with live log and reboot hint
- Storage, disk (SMART health, wear) and network overview
- Images and templates: upload (streamed), download by URL, template catalogue
- Backup overview (age per guest, guests without backup, orphaned backups), restore as new guest or overwrite
- Task history with logs across all hosts
- Schedules (cron) and notifications (ntfy, webhook, Telegram, SMTP)
- Users with roles, password change, API keys (bearer), optional OpenID Connect single sign-on
- Prometheus endpoint, audit log, encrypted backup/restore of the manager itself
- Installer, uninstaller, host onboarding script, German and English UI

## Decisions

- **No node migration.** The target setup is a set of standalone hosts; PVE only migrates within a cluster.
- **No deletion of ISOs/templates.** PVE requires `Datastore.Allocate` for it, which also allows changing or removing
  storage definitions. That is too much for the service token. Clean up in the PVE UI.
- **Host shell is not built.** An API token may open the node shell, but PVE then asks for a Linux login; only
  `root@pam` gets a root shell directly. Offering it would make the manager root-equivalent on the hosts, so it would
  only make sense as an explicit per-host opt-in.

## Ideas

- "Reboot required" marker per host and a host reboot action that lists the guests it would interrupt
- Host graphs (CPU, memory, I/O over time) and an over-commit view (allocated vs. physical memory/cores)
- More alerts: guest stopped unexpectedly, backup older than N days, any storage above a threshold
- Rolling update across hosts with pre-checks (fresh backup, no running guests) and abort on failure
- Tags and notes editable in the UI, filter by tag
- Create a VM from a cloud image in one step
- Read-only status view for a wall display
- ACME / reverse-proxy guide for a trusted TLS certificate
- Audit log export and retention

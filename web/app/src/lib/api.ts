import type { BackupOverview, ClusterTask, TaskLog, CloudInit, ApiKey, Appliance, UserRow, AuditEntry, Media, NodeInfra, Channel, Schedule, BackupInfo, BackupJob, GuestConfig, Point, Snapshot, CreateOptions, JobView, TaskInfo, Me, NodeUpdates, PowerAction, ServerState } from './types'

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: { 'X-PVM': '1', ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText)
  return data as T
}

const g = (p: string) => encodeURIComponent(p)
const gp = (server: string, type: string, vmid: number) => `/api/servers/${g(server)}/guests/${type}/${vmid}`

export const api = {
  authOptions: () => call<{ local: boolean; oidc: { enabled: boolean; label?: string } }>('GET', '/api/auth-options'),
  login: (user: string, password: string) => call<Me>('POST', '/api/login', { user, password }),
  logout: () => call<{ ok: boolean }>('POST', '/api/logout'),
  me: () => call<Me>('GET', '/api/me'),
  state: () => call<ServerState[]>('GET', '/api/state'),
  power: (server: string, type: string, vmid: number, action: PowerAction) =>
    call<{ task: string }>('POST', `/api/servers/${g(server)}/guests/${type}/${vmid}/${action}`),
  updates: (server: string) =>
    call<{ nodes: NodeUpdates[]; canUpgrade: boolean }>('GET', `/api/servers/${g(server)}/updates`),
  refreshUpdates: (server: string) => call<{ tasks: string[] }>('POST', `/api/servers/${g(server)}/updates/refresh`),
  upgrade: (server: string) => call<{ job: string }>('POST', `/api/servers/${g(server)}/updates/upgrade`),
  job: (id: string, from: number) => call<JobView>('GET', `/api/jobs/${g(id)}?from=${from}`),
  createOptions: (server: string, node: string) => call<CreateOptions>('GET', `/api/servers/${g(server)}/create-options?node=${g(node)}`),
  createVM: (server: string, node: string, spec: object) => call<{ task: string; node: string; vmid: number }>('POST', `/api/servers/${g(server)}/vms?node=${g(node)}`, spec),
  createCT: (server: string, node: string, spec: object) => call<{ task: string; node: string; vmid: number }>('POST', `/api/servers/${g(server)}/containers?node=${g(node)}`, spec),
  deleteGuest: (server: string, type: string, vmid: number, name: string) => call<{ task: string; node: string }>('DELETE', `/api/servers/${g(server)}/guests/${type}/${vmid}?confirm=${g(name)}`),
  task: (server: string, node: string, upid: string) => call<TaskInfo>('GET', `/api/servers/${g(server)}/tasks/${g(node)}?upid=${g(upid)}`),
  rrd: (server: string, type: string, vmid: number, frame: string) => call<Point[]>('GET', `${gp(server, type, vmid)}/rrd?frame=${frame}`),
  snapshots: (server: string, type: string, vmid: number) => call<Snapshot[]>('GET', `${gp(server, type, vmid)}/snapshots`),
  snapshotCreate: (server: string, type: string, vmid: number, name: string, description: string, vmstate: boolean) =>
    call<{ task: string; node: string }>('POST', `${gp(server, type, vmid)}/snapshots`, { name, description, vmstate }),
  snapshotRollback: (server: string, type: string, vmid: number, name: string) =>
    call<{ task: string; node: string }>('POST', `${gp(server, type, vmid)}/snapshots/${g(name)}/rollback`),
  snapshotDelete: (server: string, type: string, vmid: number, name: string) =>
    call<{ task: string; node: string }>('DELETE', `${gp(server, type, vmid)}/snapshots/${g(name)}`),
  clone: (server: string, type: string, vmid: number, newid: number, name: string, full: boolean) =>
    call<{ task: string; node: string }>('POST', `${gp(server, type, vmid)}/clone`, { newid, name, full }),
  config: (server: string, type: string, vmid: number) => call<GuestConfig>('GET', `${gp(server, type, vmid)}/config`),
  setResources: (server: string, type: string, vmid: number, cores: number, memory: number, swap: number) =>
    call<{ ok: boolean }>('PUT', `${gp(server, type, vmid)}/config`, { cores, memory, swap }),
  growDisk: (server: string, type: string, vmid: number, disk: string, addGiB: number) =>
    call<{ ok: boolean }>('POST', `${gp(server, type, vmid)}/disks/${g(disk)}/grow`, { addGiB }),
  backups: (server: string, type: string, vmid: number) => call<BackupInfo>('GET', `${gp(server, type, vmid)}/backups`),
  backupNow: (server: string, type: string, vmid: number, storage: string, mode: string) =>
    call<{ task: string; node: string }>('POST', `${gp(server, type, vmid)}/backups`, { storage, mode }),
  restore: (server: string, type: string, vmid: number, volid: string, force: boolean) =>
    call<{ task: string; node: string }>('POST', `${gp(server, type, vmid)}/restore`, { volid, force }),
  backupJobs: (server: string) => call<BackupJob[]>('GET', `/api/servers/${g(server)}/backup-jobs`),
  channels: () => call<{ channels: Channel[]; events: string[] }>('GET', '/api/channels'),
  channelCreate: (name: string, kind: string, config: Record<string, string>, events: string[]) =>
    call<{ id: number }>('POST', '/api/channels', { name, kind, config, events }),
  channelDelete: (id: number) => call<{ ok: boolean }>('DELETE', `/api/channels/${id}`),
  channelTest: (id: number) => call<{ ok: boolean }>('POST', `/api/channels/${id}/test`),
  schedules: () => call<Schedule[]>('GET', '/api/schedules'),
  scheduleCreate: (s: object) => call<{ id: number }>('POST', '/api/schedules', s),
  scheduleEnable: (id: number, enabled: boolean) => call<{ ok: boolean }>('POST', `/api/schedules/${id}/enabled`, { enabled }),
  scheduleDelete: (id: number) => call<{ ok: boolean }>('DELETE', `/api/schedules/${id}`),
  infra: (server: string) => call<NodeInfra[]>('GET', `/api/servers/${g(server)}/infra`),
  media: (server: string, node: string) => call<Media>('GET', `/api/servers/${g(server)}/media?node=${g(node)}`),
  appliances: (server: string, node: string) => call<Appliance[]>('GET', `/api/servers/${g(server)}/media/appliances?node=${g(node)}`),
  mediaDownload: (server: string, node: string, body: object) => call<{ task: string; node: string }>('POST', `/api/servers/${g(server)}/media/download?node=${g(node)}`, body),
  mediaAppliance: (server: string, node: string, storage: string, template: string) =>
    call<{ task: string; node: string }>('POST', `/api/servers/${g(server)}/media/appliance?node=${g(node)}`, { storage, template }),
  users: () => call<UserRow[]>('GET', '/api/users'),
  userCreate: (name: string, password: string, role: string) => call<{ ok: boolean }>('POST', '/api/users', { name, password, role }),
  userUpdate: (name: string, body: { role?: string; password?: string }) => call<{ ok: boolean }>('PUT', `/api/users/${g(name)}`, body),
  userDelete: (name: string) => call<{ ok: boolean }>('DELETE', `/api/users/${g(name)}`),
  changePassword: (current: string, next: string) => call<{ ok: boolean }>('POST', '/api/me/password', { current, new: next }),
  keys: () => call<ApiKey[]>('GET', '/api/keys'),
  keyCreate: (name: string, role: string) => call<{ id: number; token: string }>('POST', '/api/keys', { name, role }),
  keyDelete: (id: number) => call<{ ok: boolean }>('DELETE', `/api/keys/${id}`),
  cloudInit: (server: string, vmid: number, ci: CloudInit) => call<{ ok: boolean }>('PUT', `${gp(server, 'qemu', vmid)}/cloudinit`, ci),
  convertTemplate: (server: string, type: string, vmid: number) => call<{ task?: string; node?: string; ok?: boolean }>('POST', `${gp(server, type, vmid)}/template`),
  backupOverview: (server: string, node: string) => call<BackupOverview>('GET', `/api/servers/${g(server)}/backup-overview?node=${g(node)}`),
  restoreBackup: (server: string, node: string, volid: string, vmid: number, force: boolean) =>
    call<{ task: string; node: string; vmid: number }>('POST', `/api/servers/${g(server)}/restore?node=${g(node)}`, { volid, vmid, force }),
  taskList: (server: string, limit = 150) => call<ClusterTask[]>('GET', `/api/servers/${g(server)}/task-list?limit=${limit}`),
  taskLog: (server: string, node: string, upid: string, start: number) =>
    call<TaskLog>('GET', `/api/servers/${g(server)}/tasks/${g(node)}/log?upid=${g(upid)}&start=${start}`),
  managerBackup: async (passphrase: string): Promise<{ blob: Blob; name: string }> => {
    const res = await fetch('/api/backup', { method: 'POST', credentials: 'same-origin', headers: { 'X-PVM': '1', 'Content-Type': 'application/json' }, body: JSON.stringify({ passphrase }) })
    if (!res.ok) { const j = await res.json().catch(() => ({})); throw new ApiError(res.status, j.error ?? res.statusText) }
    const cd = res.headers.get('Content-Disposition') ?? ''
    return { blob: await res.blob(), name: /filename="([^"]+)"/.exec(cd)?.[1] ?? 'pve-manager-backup.pvmbak' }
  },
  console: (server: string, type: string, vmid: number) =>
    call<{ id: string; password: string }>('POST', `/api/servers/${g(server)}/guests/${type}/${vmid}/console`),
  audit: () => call<AuditEntry[]>('GET', '/api/audit'),
}

export interface Guest {
  vmid: number; name: string; type: 'qemu' | 'lxc'; node: string
  status: string; cpu: number; maxcpu: number; mem: number; maxmem: number
  disk: number; maxdisk: number; uptime: number; template: number; tags: string
}
export interface NodeInfo {
  node: string; status: string; cpu: number; maxcpu: number
  mem: number; maxmem: number; disk: number; maxdisk: number; uptime: number
}
export interface ServerState {
  name: string; url: string; online: boolean; error?: string; updated: number
  nodes: NodeInfo[] | null; guests: Guest[] | null; canUpgrade: boolean
}
export interface Me { user: string; role: 'admin' | 'operator' | 'viewer' }
export interface Pkg { Package: string; OldVersion: string; Version: string; Origin: string; Title: string }
export interface NodeUpdates { node: string; packages: Pkg[] | null; error: string }
export interface AuditEntry { ts: number; user: string; action: string; target: string; detail: string; ok: boolean }
export type PowerAction = 'start' | 'shutdown' | 'stop' | 'reboot' | 'reset' | 'suspend' | 'resume'
export interface JobView { id: string; server: string; kind: string; user: string; started: number; ended?: number; status: 'running' | 'ok' | 'failed'; error?: string; lines: string[]; next: number }
export interface Storage { storage: string; type: string; avail: number }
export interface ContentItem { volid: string; size: number }
export interface CreateOptions { nextid: number; storages: { images: Storage[] | null; rootdir: Storage[] | null }; isos: ContentItem[] | null; templates: ContentItem[] | null; bridges: string[] | null }
export interface TaskInfo { status: string; exitstatus?: string }
export interface Point { time: number; cpu: number | null; mem: number | null; maxmem: number | null; netin: number | null; netout: number | null; diskread: number | null; diskwrite: number | null }
export interface Snapshot { name: string; description: string; snaptime: number; vmstate: number; parent: string }
export interface GuestConfig { hasCloudInit: boolean; template: boolean; ciuser: string; ipconfig0: string; nameserver: string; searchdomain: string; cores: number; memory: number; swap: number; disks: { key: string; value: string }[] | null; nets: { key: string; value: string }[] | null; digest: string; name: string }
export interface BackupFile { volid: string; size: number; ctime: number; notes: string; vmid: number }
export interface BackupInfo { storages: string[] | null; files: BackupFile[] }
export interface BackupJob { id: string; schedule: string; storage: string; vmid: string; all: number; mode: string; enabled: number }
export interface Channel { id: number; name: string; kind: string; events: string[]; enabled: boolean; summary: string }
export interface Schedule { id: number; name: string; cron: string; action: string; server: string; gtype: string; vmid: number; params: Record<string, string> | null; enabled: boolean; lastRun: number; lastStatus: string }
export interface StorageInfo { storage: string; type: string; content: string; active: number; enabled: number; shared: number; total: number; used: number; avail: number; used_fraction: number }
export interface DiskInfo { devpath: string; model: string; vendor: string; serial: string; size: number; type: string; health: string; wearout: number | string; used: string }
export interface NetIface { iface: string; type: string; active: number; autostart: number; cidr: string; address: string; gateway: string; bridge_ports: string; slaves: string; 'vlan-id': number | string | null; 'vlan-raw-device': string; comments: string }
export interface NodeInfra { node: string; storages: StorageInfo[]; disks: DiskInfo[]; networks: NetIface[]; errors?: Record<string, string> }
export interface MediaStorage { storage: string; type: string; avail: number; content: string }
export interface MediaItem { volid: string; size: number; ctime: number; format: string; content: string; storage: string }
export interface Media { storages: MediaStorage[]; items: MediaItem[] }
export interface Appliance { template: string; package: string; description: string; os: string; version: string; headline: string }
export interface UserRow { name: string; role: 'admin' | 'operator' | 'viewer'; sso?: boolean }
export interface ApiKey { id: number; name: string; role: string; prefix: string; created: number; lastUsed: number; createdBy: string }
export interface CloudInit { user?: string; password?: string; sshKeys?: string; ip?: string; gateway?: string; nameserver?: string; searchdomain?: string; driveStorage?: string }
export interface BackupGuest { vmid: number; name: string; type: 'qemu' | 'lxc'; node: string; count: number; lastTime: number; lastSize: number; lastVolid: string; storage: string }
export interface BackupOverview { node: string; storages: StorageInfo[]; guests: BackupGuest[]; orphans: BackupGuest[] }
export interface ClusterTask { upid: string; type: string; node: string; id: string; status?: string; endtime: number; starttime: number; user: string }
export interface TaskLog { lines: { n: number; t: string }[]; status: string; exitstatus?: string }

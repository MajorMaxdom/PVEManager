<script lang="ts">
  const paths: Record<string, string> = {
    play: 'M8 5.5v13l11-6.5z',
    power: 'M12 3v8M6.6 6.6a8 8 0 1010.8 0',
    stop: 'M6 6h12v12H6z',
    reboot: 'M20 12a8 8 0 11-2.3-5.7M20 4v4h-4',
    console: 'M4 5h16v11H4zM8 20h8M12 16v4',
    overview: 'M4 4h7v7H4zM13 4h7v4h-7zM13 10h7v10h-7zM4 13h7v7H4z',
    guests: 'M4 6h16M4 12h16M4 18h16',
    updates: 'M12 4v11m0 0l-4-4m4 4l4-4M5 20h14',
    audit: 'M7 3h8l4 4v14H7zM15 3v4h4M10 12h6M10 16h6',
    search: 'M11 4a7 7 0 100 14 7 7 0 000-14zM20 20l-4-4',
    logout: 'M10 4H5v16h5M15 8l4 4-4 4M9 12h10',
    menu: 'M4 7h16M4 12h16M4 17h16',
    close: 'M6 6l12 12M18 6L6 18',
    refresh: 'M20 12a8 8 0 11-2.3-5.7M20 4v4h-4',
    pause: 'M8 5v14M16 5v14',
    expand: 'M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5',
    schedules: 'M12 4a8 8 0 100 16 8 8 0 000-16zM12 8v4l3 2',
    notifications: 'M6 17h12l-1.5-2V10a4.5 4.5 0 00-9 0v5zM10 20h4',
    infra: 'M4 6c0-1.1 3.6-2 8-2s8 .9 8 2-3.6 2-8 2-8-.9-8-2zM4 6v6c0 1.1 3.6 2 8 2s8-.9 8-2V6M4 12v6c0 1.1 3.6 2 8 2s8-.9 8-2v-6',
    media: 'M12 3a9 9 0 100 18 9 9 0 000-18zM12 9a3 3 0 100 6 3 3 0 000-6z',
    users: 'M9 11a3 3 0 100-6 3 3 0 000 6zM3 19c0-3 2.7-5 6-5s6 2 6 5M16 5.5a3 3 0 010 5.5M18 14c2 .6 3 2.2 3 4.5',
    key: 'M14 8a4 4 0 11-1.2 2.8L4 20v-3h2v-2h2v-2h2l2.8-2.8A4 4 0 0114 8zM16 8h.01',
    backups: 'M12 3l8 3v5c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V6zM9 12l2 2 4-4',
    tasks: 'M5 6h14M5 12h14M5 18h9M3 6h.01M3 12h.01M3 18h.01',
    kbd: 'M5 8h14v8H5zM8 12h.01M12 12h.01M16 12h.01',
  }
  let { name, size = 18 }: { name: string; size?: number } = $props()
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
  <path d={paths[name] ?? ''} />
</svg>

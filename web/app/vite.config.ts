import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  build: { outDir: '../dist', emptyOutDir: true, target: 'esnext', assetsInlineLimit: 0 }, // noVNC uses top-level await
  server: { proxy: { '/api': { target: 'https://127.0.0.1:18443', secure: false, ws: true } } },
})

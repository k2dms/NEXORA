import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { resolve } from 'node:path';

export default defineConfig({
  plugins: [svelte({ compilerOptions: { customElement: true } })],
  build: {
    outDir: 'dist/assets-build',
    rollupOptions: {
      input: { 'nexora-options': resolve('nexora-options.html'), 'nexora-background': resolve('src/background.ts') },
      output: { entryFileNames: '[name].js', chunkFileNames: 'nexora-assets/[name]-[hash].js', assetFileNames: 'nexora-assets/[name]-[hash][extname]' },
    },
  },
});

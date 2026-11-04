import { defineConfig } from 'vite';
export default defineConfig({
  base: '/app/fnterminal/',
  build: { outDir: '../internal/server/static', emptyOutDir: true },
  server: {
    proxy: {
      '/app/fnterminal/api': 'http://127.0.0.1:8088',
      '/app/fnterminal/ws': { target: 'ws://127.0.0.1:8088', ws: true },
    },
  },
});

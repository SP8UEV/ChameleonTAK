import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Panel jest wbudowywany w binarkę Go (internal/panel/dist, przez embed).
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    outDir: '../internal/panel/dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 900,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: false,
        ws: true,
      },
    },
  },
});

import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

// NIDAW Admin Console (Phase H Step 3).
// Dev server proxies /api to the backend gateway so cookies/JWT work locally.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': path.resolve(__dirname, 'src') },
  },
  server: {
    port: 5174,
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});

import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist-library',
    lib: { entry: 'src/index.ts', formats: ['es'], fileName: () => 'index.js', cssFileName: 'style' },
    rollupOptions: { external: id => /^(react|react-dom|react-router-dom|@tanstack\/react-query)(\/|$)/.test(id) },
  },
});

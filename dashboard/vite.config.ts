import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/v1": "http://localhost:8090" } },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './testing/setup.ts',
  },
})

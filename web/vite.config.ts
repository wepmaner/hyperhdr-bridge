import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Сборка кладётся прямо в каталог, который мост встраивает в exe (go:embed).
export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    outDir: '../internal/web/static',
    emptyOutDir: true,
  },
  server: {
    // npm run dev: панель с горячей перезагрузкой, данные — от запущенного моста.
    proxy: { '/api': 'http://127.0.0.1:7788' },
  },
});

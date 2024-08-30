import { PluginOption, defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import svgr from 'vite-plugin-svgr';
import { viteSingleFile } from 'vite-plugin-singlefile';

// https://vitejs.dev/config/
export default defineConfig(({ command }) => {
  const plugins: PluginOption[] = [react(), svgr()];

  if (command === 'build') {
    plugins.push(viteSingleFile());
  }

  return {
    plugins,
    server: {
      proxy: {
        '/api': {
          target: 'http://localhost:8765',
          changeOrigin: true,
          rewrite: (path: string) => path.replace(/^\/api/, ''),
        },
        '/proxy': {
          target: 'http://localhost:8765',
          changeOrigin: true,
          //rewrite: (path: string) => path.replace(/^\/api/, ''),
        },
      },
    },
  };
});

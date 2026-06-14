import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { VitePWA } from 'vite-plugin-pwa';

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      manifest: {
        name: 'AI Dev Team',
        short_name: 'AI Dev Team',
        description:
          'A multi‑agent AI development assistant – free trial, autonomous, collaborative.',
        start_url: '.',
        scope: '.',
        display: 'standalone',
        background_color: '#0b0c10',
        theme_color: '#1f2833',
        icons: [
          { src: 'pwa-96.png',   sizes: '96x96',   type: 'image/png' },
          { src: 'pwa-144.png',  sizes: '144x144', type: 'image/png' },
          { src: 'pwa-192.png',  sizes: '192x192', type: 'image/png' },
          { src: 'pwa-256.png',  sizes: '256x256', type: 'image/png' },
          { src: 'pwa-384.png',  sizes: '384x384', type: 'image/png' },
          { src: 'pwa-512.png',  sizes: '512x512', type: 'image/png' }
        ]
      },
      workbox: {
        runtimeCaching: [
          {
            urlPattern: ({ url }) => url.origin === self.location.origin,
            handler: 'CacheFirst',
            options: {
              cacheName: 'static-assets',
              expiration: { maxEntries: 200, maxAgeSeconds: 2592000 }
            }
          }
        ]
      }
    })
  ],
  base: '/synthos-collective/',
});

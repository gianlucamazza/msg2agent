import { defineConfig } from 'astro/config';
import preact from '@astrojs/preact';
import sitemap from '@astrojs/sitemap';
import mdx from '@astrojs/mdx';

export default defineConfig({
  output: 'static',
  site: 'https://msg2agent.xyz',
  integrations: [
    preact({ compat: false }),
    mdx(),
    sitemap({
      filter: (page) => !page.includes('/app/'),
    }),
  ],
  build: { format: 'file', inlineStylesheets: 'never' },
  vite: { build: { cssCodeSplit: true, modulePreload: { polyfill: false } } },
});

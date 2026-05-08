/**
 * Copies Astro build output into Go embed directories.
 *
 * dist/                           → (root)
 *   index.html                    → cmd/relay/web/index.html
 *   pricing.html                  → cmd/relay/web/pricing.html
 *   privacy.html                  → cmd/relay/web/privacy.html
 *   terms.html                    → cmd/relay/web/terms.html
 *   sitemap*.xml, robots.txt      → cmd/relay/web/
 *   _astro/                       → cmd/relay/web/_astro/
 *                                   cmd/dashboard/web/_astro/
 *   app/index.html                → cmd/dashboard/web/index.html
 *   style.css (if present)        → pkg/webui/assets/style.css
 *
 * Static assets (favicon, logos) come from web/public/ and are copied by
 * Astro automatically into dist/; they're then picked up here for relay.
 */

import { copyFile, mkdir, readdir, stat, rm } from 'node:fs/promises';
import { join, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = join(__dirname, '..');
const repoRoot = join(root, '..');
const dist = join(root, 'dist');
const relayWeb = join(repoRoot, 'cmd', 'relay', 'web');
const dashboardWeb = join(repoRoot, 'cmd', 'dashboard', 'web');
const webuiAssets = join(repoRoot, 'pkg', 'webui', 'assets');

async function copy(src, dest) {
  await mkdir(dirname(dest), { recursive: true });
  await copyFile(src, dest);
}

async function copyDir(src, dest) {
  const entries = await readdir(src, { withFileTypes: true });
  for (const entry of entries) {
    const srcPath = join(src, entry.name);
    const destPath = join(dest, entry.name);
    if (entry.isDirectory()) {
      await copyDir(srcPath, destPath);
    } else {
      await copy(srcPath, destPath);
    }
  }
}

async function exists(p) {
  return stat(p).then(() => true, () => false);
}

async function main() {
  if (!await exists(dist)) {
    console.error(`dist/ not found at ${dist}. Run 'pnpm build' first.`);
    process.exit(1);
  }

  // Clean generated subdirs (keep any manually placed assets not in _astro)
  for (const dir of [join(relayWeb, '_astro'), join(dashboardWeb, '_astro')]) {
    if (await exists(dir)) await rm(dir, { recursive: true });
  }

  const distEntries = await readdir(dist, { withFileTypes: true });

  for (const entry of distEntries) {
    const src = join(dist, entry.name);

    if (entry.isDirectory()) {
      if (entry.name === '_astro') {
        // Copy shared chunk dir to both embed roots
        await copyDir(src, join(relayWeb, '_astro'));
        await copyDir(src, join(dashboardWeb, '_astro'));
      }
      // Other subdirs: skip (no nested pages in current design)
    } else {
      const ext = entry.name.split('.').pop() ?? '';

      // app.html → dashboard SPA shell
      if (entry.name === 'app.html') {
        await copy(src, join(dashboardWeb, 'index.html'));
        console.log('  dashboard/web/index.html');
        continue;
      }

      // Tailwind output: copy to pkg/webui/assets/style.css
      if (entry.name === 'style.css') {
        await copy(src, join(webuiAssets, 'style.css'));
        console.log('  pkg/webui/assets/style.css (Tailwind output)');
        continue;
      }

      // Relay root files: .html, .xml, .txt, .svg, .png, .webp, .ico
      if (['html', 'xml', 'txt', 'svg', 'png', 'webp', 'ico'].includes(ext)) {
        await copy(src, join(relayWeb, entry.name));
        console.log(`  relay/web/${entry.name}`);
      }
    }
  }

  const astroDir = join(dist, '_astro');
  if (await exists(astroDir)) {
    const chunks = await readdir(astroDir);
    console.log(`  _astro/ (${chunks.length} chunks) → relay/web + dashboard/web`);
  }

  console.log('split-dist done');
}

main().catch(err => { console.error(err); process.exit(1); });

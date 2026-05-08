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

import { copyFile, mkdir, readdir, readFile, writeFile, stat, rm } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { join, dirname } from 'node:path';
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

// INLINE_SCRIPT_RE matches executable inline <script> tags (no src=, not JSON-LD).
const INLINE_SCRIPT_RE = /<script(?![^>]*\bsrc=)(?![^>]*type=["']application\/ld\+json["'])[^>]*>([\s\S]*?)<\/script>/gi;

// computeCSPHashes extracts SHA-256 hashes of Astro's inline hydration scripts
// from relay HTML files and returns them as CSP hash sources ('sha256-...').
// These hashes are deterministic for a given Astro version and component graph.
async function computeCSPHashes() {
  const hashes = [];
  const relayHtml = ['index.html', 'pricing.html', 'privacy.html', 'terms.html'];
  for (const name of relayHtml) {
    const file = join(dist, name);
    if (!await exists(file)) continue;
    const content = await readFile(file, 'utf8');
    INLINE_SCRIPT_RE.lastIndex = 0;
    let m;
    while ((m = INLINE_SCRIPT_RE.exec(content)) !== null) {
      const src = m[1];
      if (!src.trim()) continue;
      const hash = `'sha256-${createHash('sha256').update(src).digest('base64')}'`;
      if (!hashes.includes(hash)) hashes.push(hash);
    }
  }
  return hashes;
}

async function main() {
  if (!await exists(dist)) {
    console.error(`dist/ not found at ${dist}. Run 'pnpm build' first.`);
    process.exit(1);
  }

  const cspHashes = await computeCSPHashes();

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

      // Static assets also needed by the dashboard SPA (favicon, logos for apple-touch-icon).
      if (['svg', 'png', 'ico'].includes(ext)) {
        await copy(src, join(dashboardWeb, entry.name));
      }
    }
  }

  const astroDir = join(dist, '_astro');
  if (await exists(astroDir)) {
    const chunks = await readdir(astroDir);
    console.log(`  _astro/ (${chunks.length} chunks) → relay/web + dashboard/web`);
  }

  // Write CSP hashes for relay inline scripts to the relay embed dir.
  // cmd/relay/main.go reads this file at startup and appends hashes to script-src.
  const cspHashesPath = join(relayWeb, 'csp-hashes.json');
  await writeFile(cspHashesPath, JSON.stringify(cspHashes));
  console.log(`  csp-hashes.json (${cspHashes.length} hash(es))`);

  console.log('split-dist done');
}

main().catch(err => { console.error(err); process.exit(1); });

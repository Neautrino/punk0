#!/usr/bin/env bun
/**
 * scripts/build-web.js
 *
 * Bundles the frontend sources in `web/src/` into `web/app.js` and
 * `web/workbench.js` with Bun (`bun scripts/build-web.js`). Bun parses every
 * module while bundling, so a syntax error fails the build. Zero external
 * dependencies.
 */

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import zlib from 'node:zlib';

if (typeof Bun === 'undefined') {
  console.error('[build-web] Run this with Bun: bun scripts/build-web.js');
  process.exit(1);
}

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..');
const srcDir = path.join(rootDir, 'web', 'src');
// Each entry is bundled on its own: the editor app, and the agents page that
// a bare `punk0` serves (workbench.go), which shares only a few small modules.
const bundles = [
  { entry: path.join(srcDir, 'main.js'), out: path.join(rootDir, 'web', 'app.js') },
  { entry: path.join(srcDir, 'workbench.js'), out: path.join(rootDir, 'web', 'workbench.js') },
];

function bundle(entryFile, outFile) {
  console.log(`[build-web] Bundling ${path.basename(entryFile)} with Bun...`);
  execFileSync(process.execPath, [
    'build',
    entryFile,
    `--outfile=${outFile}`,
    '--target=browser',
    '--format=iife',
    '--minify'
  ], {
    cwd: rootDir,
    stdio: 'inherit'
  });
}

/**
 * Pre-compresses vendor minified JS assets with gzip to keep the compiled Go
 * binary small. Replaces uncompressed .min.js files with .min.js.gz.
 */
function compressVendorAssets() {
  const vendorDir = path.join(rootDir, 'web', 'vendor');
  if (!fs.existsSync(vendorDir)) return;
  const files = fs.readdirSync(vendorDir);
  for (const file of files) {
    if (file.endsWith('.min.js')) {
      const fullPath = path.join(vendorDir, file);
      const gzPath = fullPath + '.gz';
      const content = fs.readFileSync(fullPath);
      const compressed = zlib.gzipSync(content, { level: 9 });
      fs.writeFileSync(gzPath, compressed);
      fs.unlinkSync(fullPath);
      console.log(`[build-web] Compressed ${path.relative(rootDir, fullPath)} -> ${path.relative(rootDir, gzPath)} (${(content.length / 1024 / 1024).toFixed(2)} MB -> ${(compressed.length / 1024 / 1024).toFixed(2)} MB)`);
    }
  }
}

function run() {
  const start = Date.now();
  for (const { entry, out } of bundles) bundle(entry, out);
  compressVendorAssets();

  const duration = Date.now() - start;
  for (const { out } of bundles) {
    const sizeKb = (fs.statSync(out).size / 1024).toFixed(1);
    console.log(`[build-web] Successfully built ${path.relative(rootDir, out)} (${sizeKb} KB)`);
  }
  console.log(`[build-web] Done in ${duration}ms`);
}

run();

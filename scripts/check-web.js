#!/usr/bin/env bun
/**
 * scripts/check-web.js
 * Fast validator for web/src/*.js syntax, relative import integrity, and
 * frontend tests. Runs under Bun (`bun scripts/check-web.js`): syntax is
 * checked with Bun's parser, which never executes the file.
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';

if (typeof Bun === 'undefined') {
  console.error('[check-web] Run this with Bun: bun scripts/check-web.js');
  process.exit(1);
}

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..');
const srcDir = path.join(rootDir, 'web', 'src');
const parser = new Bun.Transpiler({ loader: 'js' });

// syntaxError is the parser's complaint about src, or '' when it parses.
function syntaxError(src) {
  try {
    parser.transformSync(src);
    return '';
  } catch (err) {
    return err.message;
  }
}

console.log('[check-web] Validating frontend modules...');
const files = fs.readdirSync(srcDir).filter(f => f.endsWith('.js'));
let hasError = false;

for (const file of files) {
  const filePath = path.join(srcDir, file);
  const content = fs.readFileSync(filePath, 'utf-8');
  const bad = syntaxError(content);
  if (bad) {
    console.error(`[check-web] Syntax error in ${file}:`, bad);
    hasError = true;
  }

  const importRegex = /(?:import|from)\s+['"](\.[^'"]+)['"]/g;
  let match;
  while ((match = importRegex.exec(content)) !== null) {
    const relTarget = match[1];
    const resolved = path.resolve(srcDir, relTarget.endsWith('.js') ? relTarget : relTarget + '.js');
    if (!fs.existsSync(resolved)) {
      console.error(`[check-web] Broken import in ${file}: cannot resolve "${relTarget}"`);
      hasError = true;
    }
  }
}

try {
  execFileSync(process.execPath, ['test', 'web/src/'], { cwd: rootDir, stdio: 'inherit' });
} catch (err) {
  // If bun test fails, propagate error
  if (err.status !== 0) {
    hasError = true;
  }
}

for (const bundle of ['web/app.js', 'web/workbench.js']) {
  const bundlePath = path.join(rootDir, bundle);
  if (fs.existsSync(bundlePath)) {
    const bad = syntaxError(fs.readFileSync(bundlePath, 'utf-8'));
    if (bad) {
      console.error(`[check-web] Syntax error in ${bundle}:`, bad);
      hasError = true;
    }
  }
}

if (hasError) {
  process.exit(1);
}
console.log(`[check-web] Checked ${files.length} modules: all syntax, imports, and tests OK.`);

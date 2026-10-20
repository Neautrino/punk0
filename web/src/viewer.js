// web/src/viewer.js
// Syntax-highlighted virtualized code viewer for the active satellite.
const LH = 20; // line height in px
const CHUNK = 1000;
const OVERSCAN = 30;

let currentDoc = null;
let currentSatId = 'default';
let vpEl = null;
let sizerEl = null;
let rowsEl = null;
let titleEl = null;
let metaEl = null;

export function initViewer(vp, sizer, rows, title, meta) {
  vpEl = vp;
  sizerEl = sizer;
  rowsEl = rows;
  titleEl = title;
  metaEl = meta;

  vpEl.addEventListener('scroll', () => render());
  window.addEventListener('resize', () => render());
}

export function setViewerSatellite(satId) {
  currentSatId = satId || 'default';
  currentDoc = null;
  if (titleEl) titleEl.textContent = 'Select a file';
  if (metaEl) metaEl.textContent = '';
  if (rowsEl) rowsEl.innerHTML = '';
  if (sizerEl) sizerEl.style.height = '0px';
}

export async function openFile(path) {
  if (titleEl) titleEl.textContent = path;
  if (metaEl) metaEl.textContent = 'Loading...';

  const from = currentSatId;
  const res = await fetch(`/api/satellites/${encodeURIComponent(from)}/file?path=${encodeURIComponent(path)}&start=0&count=${CHUNK}`);
  if (from !== currentSatId) return; // switched satellites while loading
  if (!res.ok) {
    if (metaEl) metaEl.textContent = 'Failed to load file';
    return;
  }
  const j = await res.json();

  currentDoc = {
    path: j.path,
    lang: j.lang,
    total: j.total,
    lines: new Array(j.total),
    chunks: new Set([0]),
    pending: new Set(),
  };

  for (let i = 0; i < j.lines.length; i++) {
    currentDoc.lines[i] = j.lines[i];
  }

  if (metaEl) {
    metaEl.textContent = `${j.lang} · ${j.total} lines`;
  }

  sizerEl.style.height = `${j.total * LH + 100}px`;
  vpEl.scrollTop = 0;
  render();
}

function ensureChunks(d, first, last) {
  const c0 = Math.floor(first / CHUNK);
  const c1 = Math.floor(Math.max(first, last - 1) / CHUNK);
  for (let c = c0; c <= c1; c++) {
    if (d.chunks.has(c) || d.pending.has(c)) continue;
    d.pending.add(c);
    fetch(`/api/satellites/${encodeURIComponent(currentSatId)}/file?path=${encodeURIComponent(d.path)}&start=${c * CHUNK}&count=${CHUNK}`)
      .then(r => r.json())
      .then(j => {
        for (let i = 0; i < j.lines.length; i++) {
          d.lines[j.start + i] = j.lines[i];
        }
        d.chunks.add(c);
        d.pending.delete(c);
        if (currentDoc === d) render();
      })
      .catch(() => d.pending.delete(c));
  }
}

function render() {
  if (!currentDoc || !vpEl) return;
  const d = currentDoc;

  const top = vpEl.scrollTop;
  const first = Math.max(0, Math.floor(top / LH) - OVERSCAN);
  const count = Math.ceil(vpEl.clientHeight / LH) + OVERSCAN * 2;
  const last = Math.min(d.total, first + count);

  ensureChunks(d, first, last);

  let html = '';
  for (let i = first; i < last; i++) {
    const lineNum = i + 1;
    const body = d.lines[i];
    html += `<div class="code-row" data-line="${lineNum}"><span class="line-gutter">${lineNum}</span><span class="line-code">${body !== undefined ? body : ''}</span></div>`;
  }

  rowsEl.style.transform = `translateY(${first * LH}px)`;
  rowsEl.innerHTML = html;
}

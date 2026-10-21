// web/src/viewer.js
// Syntax-highlighted virtualized code viewer. Each open file tab keeps its
// loaded lines and scroll position, so switching tabs does not refetch.
const LH = 20; // line height in px
const CHUNK = 1000;
const OVERSCAN = 30;

let vpEl, sizerEl, rowsEl, titleEl, metaEl;
let ws = '';
let current = null; // { path, doc, top } shown now
const opened = new Map(); // path -> { path, doc, top } for the workspace ws

export function initViewer(vp, sizer, rows, title, meta) {
  vpEl = vp;
  sizerEl = sizer;
  rowsEl = rows;
  titleEl = title;
  metaEl = meta;

  vpEl.addEventListener('scroll', () => {
    if (current) current.top = vpEl.scrollTop;
    render();
  });
  window.addEventListener('resize', () => render());
}

// showFile displays path of workspace wsId, loading it on first view.
export function showFile(wsId, path) {
  if (wsId !== ws) {
    ws = wsId;
    opened.clear();
  }
  let f = opened.get(path);
  if (!f) {
    f = { path, doc: null, top: 0 };
    opened.set(path, f);
  }
  current = f;
  titleEl.textContent = path;
  if (f.doc) return paint();
  metaEl.textContent = 'Loading…';
  rowsEl.innerHTML = '';
  sizerEl.style.height = '0px';
  load(f);
}

// forgetClosed drops files of wsId no longer open in a tab.
export function forgetClosed(wsId, paths) {
  if (wsId !== ws) return;
  for (const p of opened.keys()) if (!paths.includes(p)) opened.delete(p);
  if (current && !opened.has(current.path)) current = null;
}

async function load(f) {
  const from = ws;
  const res = await fetch(`/api/workspaces/${encodeURIComponent(from)}/file?path=${encodeURIComponent(f.path)}&start=0&count=${CHUNK}`);
  if (from !== ws || opened.get(f.path) !== f) return; // switched away or closed while loading
  if (!res.ok) {
    const err = await res.json().catch(() => null);
    if (current === f) metaEl.textContent = err?.error || 'Failed to load file';
    opened.delete(f.path);
    return;
  }
  const j = await res.json();
  f.doc = { path: j.path, lang: j.lang, total: j.total, lines: new Array(j.total), chunks: new Set([0]), pending: new Set() };
  for (let i = 0; i < j.lines.length; i++) f.doc.lines[i] = j.lines[i];
  if (current === f) paint();
}

function paint() {
  const d = current.doc;
  metaEl.textContent = `${d.lang} · ${d.total} lines`;
  sizerEl.style.height = `${d.total * LH + 100}px`;
  vpEl.scrollTop = current.top;
  render();
}

function ensureChunks(d, first, last) {
  const from = ws;
  const c0 = Math.floor(first / CHUNK);
  const c1 = Math.floor(Math.max(first, last - 1) / CHUNK);
  for (let c = c0; c <= c1; c++) {
    if (d.chunks.has(c) || d.pending.has(c)) continue;
    d.pending.add(c);
    fetch(`/api/workspaces/${encodeURIComponent(from)}/file?path=${encodeURIComponent(d.path)}&start=${c * CHUNK}&count=${CHUNK}`)
      .then((r) => r.json())
      .then((j) => {
        for (let i = 0; i < j.lines.length; i++) d.lines[j.start + i] = j.lines[i];
        d.chunks.add(c);
        d.pending.delete(c);
        if (current?.doc === d) render();
      })
      .catch(() => d.pending.delete(c));
  }
}

function render() {
  const d = current?.doc;
  if (!d || !vpEl.clientHeight) return;

  const first = Math.max(0, Math.floor(vpEl.scrollTop / LH) - OVERSCAN);
  const count = Math.ceil(vpEl.clientHeight / LH) + OVERSCAN * 2;
  const last = Math.min(d.total, first + count);

  ensureChunks(d, first, last);

  let html = '';
  for (let i = first; i < last; i++) {
    const body = d.lines[i];
    html += `<div class="code-row" data-line="${i + 1}"><span class="line-gutter">${i + 1}</span><span class="line-code">${body !== undefined ? body : ''}</span></div>`;
  }

  rowsEl.style.transform = `translateY(${first * LH}px)`;
  rowsEl.innerHTML = html;
}

// web/src/orch/viewer.js
// Read-only code viewer for file tabs: syntax-highlighted lines from the
// folder's editor backend (w/<id>/api/file), fetched in chunks and rendered
// virtually, so only the visible rows are in the DOM. Each open file keeps
// its loaded lines and scroll position, so switching tabs does not refetch.
// Rows use the app's .row/.g/.c classes and token colours (style.css).
import { $, esc } from '../state.js';
import { call, pageURL } from './api.js';

const VIEW_CHUNK = 1000;
const VIEW_OVERSCAN = 30;

let codeVp, codeSizer, codeRows, fileTitle, fileMeta;
let lineH = 21; // --lh, measured at init
let viewWs = ''; // workspace id of the open files below
let viewHref = ''; // its folder's backend, "w/<id>/"
let current = null; // { path, doc, top, image, error } shown now
const opened = new Map(); // path -> the same, for the workspace viewWs

export function initViewer() {
  codeVp = $('#o-code-vp');
  codeSizer = $('#o-code-sizer');
  codeRows = $('#o-code-rows');
  fileTitle = $('#o-file-title');
  fileMeta = $('#o-file-meta');
  lineH = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--lh')) || lineH;

  codeVp.addEventListener('scroll', () => {
    if (current) current.top = codeVp.scrollTop;
    renderRows();
  });
  new ResizeObserver(() => renderRows()).observe(codeVp);
}

// showFile displays path of workspace w, loading it on first view.
export function showFile(w, path) {
  if (w.id !== viewWs) {
    viewWs = w.id;
    viewHref = w.href;
    opened.clear();
  }
  let f = opened.get(path);
  if (!f) {
    f = { path, doc: null, top: 0, image: false, error: '' };
    opened.set(path, f);
    loadFile(f);
  }
  current = f;
  fileTitle.textContent = path;
  paint();
}

// forgetClosed drops the files of workspace wsId no longer open in a tab.
export function forgetClosed(wsId, paths) {
  if (wsId !== viewWs) return;
  for (const p of opened.keys()) if (!paths.includes(p)) opened.delete(p);
  if (current && !opened.has(current.path)) current = null;
}

const fileURL = (path, start) => `${viewHref}api/file?` + new URLSearchParams({ path, start: String(start), count: String(VIEW_CHUNK) });

async function loadFile(f) {
  const from = viewWs;
  const res = await call('GET', fileURL(f.path, 0));
  if (from !== viewWs || opened.get(f.path) !== f) return; // switched away or closed while loading
  if (!res.ok) {
    f.error = res.data.error;
  } else if (res.data.image) {
    f.image = true;
  } else {
    const j = res.data;
    f.doc = { lang: j.lang, total: j.total, lines: new Array(j.total), chunks: new Set(), pending: new Set() };
    takeChunk(f.doc, 0, j);
  }
  if (current === f) paint();
}

// takeChunk stores a chunk's lines. Lines highlighted by the fast first pass
// are not exact; when the backend says a better pass is coming, the chunk is
// fetched again shortly.
function takeChunk(d, c, j) {
  for (let i = 0; i < j.lines.length; i++) d.lines[j.start + i] = j.lines[i];
  d.pending.delete(c);
  if (j.exact || !j.refine) d.chunks.add(c);
  else setTimeout(() => current?.doc === d && renderRows(), 500);
}

function paint() {
  const f = current;
  codeRows.innerHTML = '';
  codeRows.style.transform = '';
  codeSizer.style.height = '0px';
  codeVp.classList.toggle('o-image', f.image);
  if (f.error) {
    fileMeta.textContent = '';
    codeRows.innerHTML = `<p class="o-note o-err">${esc(f.error)}</p>`;
    return;
  }
  if (f.image) {
    fileMeta.textContent = 'image';
    codeRows.innerHTML = `<img src="${esc(pageURL(viewHref + 'api/raw?' + new URLSearchParams({ path: f.path })))}" alt="${esc(f.path)}">`;
    return;
  }
  const d = f.doc;
  if (!d) {
    fileMeta.textContent = 'Loading…';
    return;
  }
  fileMeta.textContent = `${d.lang} · ${d.total} line${d.total === 1 ? '' : 's'}`;
  codeRows.style.setProperty('--gw', String(String(d.total).length));
  codeSizer.style.height = `${d.total * lineH + 100}px`;
  codeVp.scrollTop = f.top;
  renderRows();
}

function ensureChunks(d, first, last) {
  const from = viewWs;
  const c0 = Math.floor(first / VIEW_CHUNK);
  const c1 = Math.floor(Math.max(first, last - 1) / VIEW_CHUNK);
  for (let c = c0; c <= c1; c++) {
    if (d.chunks.has(c) || d.pending.has(c)) continue;
    d.pending.add(c);
    call('GET', fileURL(current.path, c * VIEW_CHUNK)).then(res => {
      if (from !== viewWs) return;
      if (!res.ok) return d.pending.delete(c);
      takeChunk(d, c, res.data);
      if (current?.doc === d) renderRows();
    });
  }
}

function renderRows() {
  const d = current?.doc;
  if (!d || !codeVp.clientHeight) return;

  const first = Math.max(0, Math.floor(codeVp.scrollTop / lineH) - VIEW_OVERSCAN);
  const count = Math.ceil(codeVp.clientHeight / lineH) + VIEW_OVERSCAN * 2;
  const last = Math.min(d.total, first + count);

  ensureChunks(d, first, last);

  let html = '';
  for (let i = first; i < last; i++) {
    html += `<div class="row"><span class="g">${i + 1}</span><span class="c">${d.lines[i] ?? ''}</span></div>`;
  }
  codeRows.style.transform = `translateY(${first * lineH}px)`;
  codeRows.innerHTML = html;
}

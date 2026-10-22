// web/src/orch/viewer.js
// Read-only code viewer for file tabs: syntax-highlighted lines from the
// folder's editor backend (w/<id>/api/file), fetched in chunks and rendered
// virtually, so only the visible rows are in the DOM. Each open file keeps
// its loaded lines, scroll position and cursor, so switching tabs does not
// refetch. Rows use the app's .row/.g/.c classes and token colours
// (style.css). While a text file is in front, the status bar's #st-file
// shows its cursor (a click places it; clicking Ln / Col or Mod+G opens Go
// to line), +/- lines against HEAD, tab size, encoding and line endings.
import { $, esc, MOD, withKeys } from '../state.js';
import { on } from '../bus.js';
import { call, pageURL } from './api.js';
import { getSetting } from './settings.js';

const VIEW_CHUNK = 1000;
const VIEW_OVERSCAN = 30;

let codeVp, codeSizer, codeRows, fileTitle, fileMeta;
let stFile, stPos, stDiff, stTabSize, stEnc, stEol, gotoEl, gotoIn;
let lineH = 21; // --lh, measured at init
let tabSize = 4; // editor.tabSize: how wide a tab renders and counts in Col
let viewWs = ''; // workspace id of the open files below
let viewHref = ''; // its folder's backend, "w/<id>/"
let current = null; // { path, doc, top, cur, image, error } shown now
const opened = new Map(); // path -> the same, for the workspace viewWs

export function initViewer() {
  codeVp = $('#o-code-vp');
  codeSizer = $('#o-code-sizer');
  codeRows = $('#o-code-rows');
  fileTitle = $('#o-file-title');
  fileMeta = $('#o-file-meta');
  stFile = $('#st-file');
  stPos = $('#st-pos');
  stDiff = $('#st-diff');
  stTabSize = $('#st-tabsize');
  stEnc = $('#st-enc');
  stEol = $('#st-eol');
  gotoEl = $('#o-goto');
  gotoIn = $('#o-goto-in');
  lineH = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--lh')) || lineH;

  codeVp.addEventListener('scroll', () => {
    if (current) current.top = codeVp.scrollTop;
    renderRows();
  });
  new ResizeObserver(() => renderRows()).observe(codeVp);
  codeRows.addEventListener('mouseup', placeCursor);

  on('setting:changed', key => {
    if (key !== 'editor.tabSize') return;
    tabSize = parseInt(getSetting(key), 10) || 4;
    document.documentElement.style.setProperty('--tab-size', String(tabSize));
    drawStatus();
  });

  stPos.title = withKeys('Go to line ({Mod+G})');
  // mousedown would blur the open box, which closes it, and the click would
  // then reopen it.
  stPos.addEventListener('mousedown', e => e.preventDefault());
  stPos.addEventListener('click', () => (gotoEl.hidden ? openGoto() : closeGoto()));
  gotoEl.addEventListener('submit', e => {
    e.preventDefault();
    goTo(gotoIn.value);
  });
  gotoIn.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeGoto();
  });
  gotoIn.addEventListener('blur', () => closeGoto());
  // Mod+G only while a file is in front; in a terminal the key is the shell's.
  addEventListener('keydown', e => {
    if (e.key.toLowerCase() !== 'g' || !e[MOD] || e.shiftKey || e.altKey) return;
    if (!current?.doc || e.target.closest?.('#o-term')) return;
    e.preventDefault();
    openGoto();
  });
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
    f = { path, doc: null, top: 0, cur: null, image: false, error: '' };
    opened.set(path, f);
    loadFile(f);
  }
  current = f;
  fileTitle.textContent = path;
  paint();
}

// hideFile is for a terminal tab, or none, in front: the status bar drops
// the file's details.
export function hideFile() {
  current = null;
  drawStatus();
}

// forgetClosed drops the files of workspace wsId no longer open in a tab.
export function forgetClosed(wsId, paths) {
  if (wsId !== viewWs) return;
  for (const p of opened.keys()) if (!paths.includes(p)) opened.delete(p);
  if (current && !opened.has(current.path)) hideFile();
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
    f.doc = {
      lang: j.lang, total: j.total, lines: new Array(j.total), chunks: new Set(), pending: new Set(),
      eol: j.eol || '', encoding: j.encoding || '', added: j.added || 0, deleted: j.deleted || 0,
    };
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
  drawStatus();
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

// drawStatus fills #st-file for a text file in front, and hides it for
// anything else: an image, an error, a file still loading, a terminal.
function drawStatus() {
  const d = current?.doc;
  stFile.hidden = !d;
  if (!d) return closeGoto();
  const { line, col } = current.cur || { line: 1, col: 1 };
  stPos.textContent = `Ln ${line}, Col ${col}`;
  stDiff.innerHTML = d.added || d.deleted ? `<span class="add">+${d.added}</span><span class="del">−${d.deleted}</span>` : '';
  stTabSize.textContent = `Tab Size: ${tabSize}`;
  stEnc.textContent = d.encoding;
  stEol.textContent = d.eol;
}

// placeCursor puts the cursor where a click (or the end of a drag) left the
// selection: its row's line, and its column with tabs counted to the next
// tab stop. On the line numbers it is column 1.
function placeCursor(e) {
  if (!current?.doc) return;
  const sel = getSelection();
  const node = sel.focusNode?.nodeType === Node.TEXT_NODE ? sel.focusNode.parentElement : sel.focusNode;
  const inCode = !e.target.closest('.g') && node?.closest?.('#o-code-rows .c');
  const row = inCode ? node.closest('.row') : e.target.closest('.row');
  if (!row) return;
  let col = 1;
  if (inCode) {
    const r = document.createRange();
    r.setStart(row.querySelector('.c'), 0);
    r.setEnd(sel.focusNode, sel.focusOffset);
    for (const ch of r.toString()) col += ch === '\t' ? tabSize - (col - 1) % tabSize : 1;
  }
  setCursor(Number(row.querySelector('.g').textContent), col);
}

function setCursor(line, col) {
  current.cur = { line, col };
  for (const row of codeRows.children) {
    row.classList.toggle('cur', Number(row.firstChild?.textContent) === line);
  }
  drawStatus();
}

// Go to line: a box above Ln / Col taking "line" or "line:column".
function openGoto() {
  if (!current?.doc) return;
  gotoIn.value = '';
  gotoIn.placeholder = `Line 1–${current.doc.total}, or line:column`;
  gotoEl.hidden = false;
  stPos.setAttribute('aria-expanded', 'true');
  const r = stPos.getBoundingClientRect();
  gotoEl.style.left = `${Math.max(4, Math.min(r.right - gotoEl.offsetWidth, innerWidth - gotoEl.offsetWidth - 4))}px`;
  gotoEl.style.top = `${r.top - gotoEl.offsetHeight - 4}px`;
  gotoIn.focus();
}

function closeGoto() {
  if (gotoEl.hidden) return;
  gotoEl.hidden = true;
  stPos.setAttribute('aria-expanded', 'false');
}

// goTo moves the cursor to the typed line, a third of the way down the view.
function goTo(text) {
  const m = /^\s*(\d+)\s*(?:[:,]\s*(\d+)\s*)?$/.exec(text);
  const d = current?.doc;
  if (!m || !d) return;
  const line = Math.min(Math.max(1, Number(m[1])), d.total);
  closeGoto();
  codeVp.scrollTop = Math.max(0, (line - 1) * lineH - codeVp.clientHeight / 3);
  current.top = codeVp.scrollTop;
  renderRows();
  setCursor(line, Math.max(1, Number(m[2]) || 1));
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
  const cur = current.cur?.line;
  let html = '';
  for (let i = first; i < last; i++) {
    html += `<div class="row${i + 1 === cur ? ' cur' : ''}"><span class="g">${i + 1}</span><span class="c">${d.lines[i] ?? ''}</span></div>`;
  }
  codeRows.style.transform = `translateY(${first * lineH}px)`;
  codeRows.innerHTML = html;
}

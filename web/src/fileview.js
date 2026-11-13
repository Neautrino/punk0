// web/src/fileview.js
// Read-only code view for file tabs: syntax-highlighted lines from a
// folder's editor backend (api/file), fetched in chunks and rendered
// virtually, so only the visible rows are in the DOM. Each open file keeps
// its loaded lines, scroll position and cursor, so switching tabs does not
// refetch. Rows use the app's .row/.g/.c classes and token colours
// (style.css).
//
// A page makes one view per place a file can show: the agents page one per
// tab group, the editor one for its Terminal side. One view at a time owns
// the page's file status (opts.ownsStatus): its cursor, line count, size,
// tab size, encoding and line endings in #st-file (statusparts.js), Go to
// line (initFileStatus) and 'file:shown' for the LSP item (lsp.js). A file
// tab switched to its diff keeps all of that but the cursor.
import { $, MOD, withKeys } from './state.js';
import { on, emit } from './bus.js';
import { drawFileStatus, tabCol } from './statusparts.js';

const VIEW_CHUNK = 1000;
const VIEW_OVERSCAN = 30;

const views = new Set();
let stPos, gotoEl, gotoIn;
const statusView = () => [...views].find(v => v.ownsStatus());

// initFileStatus binds the page's Go to line box to whichever view owns the
// status: clicking Ln / Col or Mod+G (outside a terminal) opens it, typing
// "line" or "line:column" and Enter goes there.
export function initFileStatus() {
  stPos = $('#st-pos');
  gotoEl = $('#o-goto');
  gotoIn = $('#o-goto-in');
  stPos.title = withKeys('Go to line ({Mod+G})');
  // mousedown would blur the open box, which closes it, and the click would
  // then reopen it.
  stPos.addEventListener('mousedown', e => e.preventDefault());
  stPos.addEventListener('click', () => (gotoEl.hidden ? openGoto() : closeGoto()));
  gotoEl.addEventListener('submit', e => {
    e.preventDefault();
    const m = /^\s*(\d+)\s*(?:[:,]\s*(\d+)\s*)?$/.exec(gotoIn.value);
    if (!m) return;
    closeGoto();
    statusView()?.goToLine(Number(m[1]), Number(m[2]));
  });
  gotoIn.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeGoto();
  });
  gotoIn.addEventListener('blur', () => closeGoto());
  // Mod+G only while a file is in front; in a terminal the key is the shell's.
  addEventListener('keydown', e => {
    if (e.key.toLowerCase() !== 'g' || !e[MOD] || e.shiftKey || e.altKey) return;
    if (!statusView()?.canGoto() || e.target.closest?.('.xterm')) return;
    e.preventDefault();
    openGoto();
  });
}

function openGoto() {
  const v = statusView();
  if (!v?.canGoto() || !gotoEl) return;
  gotoIn.value = '';
  gotoIn.placeholder = `Line 1–${v.total()}, or line:column`;
  gotoEl.hidden = false;
  stPos.setAttribute('aria-expanded', 'true');
  const r = stPos.getBoundingClientRect();
  gotoEl.style.left = `${Math.max(4, Math.min(r.right - gotoEl.offsetWidth, innerWidth - gotoEl.offsetWidth - 4))}px`;
  gotoEl.style.top = `${r.top - gotoEl.offsetHeight - 4}px`;
  gotoIn.focus();
}

function closeGoto() {
  if (!gotoEl || gotoEl.hidden) return;
  gotoEl.hidden = true;
  stPos.setAttribute('aria-expanded', 'false');
}

// createFileView shows files in els = {vp, sizer, rows, title, meta}. opts:
//   load(w, path, start, count)  Promise of {ok, data} from w's api/file
//   rawURL(w, path)              the URL of w's api/raw for an image
//   getSetting(key)              the page's settings (editor.tabSize)
//   ownsStatus()                 this view writes the page's file status now
export function createFileView(els, opts) {
  const { vp, sizer, rows, title, meta } = els;
  const { load, rawURL, getSetting, ownsStatus = () => false } = opts;
  // The rows' height (--lh) can change with the editor's typography settings
  // after the view is made, so it is read where it is used.
  const rowH = () => parseFloat(getComputedStyle(rows).getPropertyValue('--lh')) || 20;
  let tabSize = parseInt(getSetting('editor.tabSize'), 10) || 4;
  let viewW = null; // workspace of the open files below
  let current = null; // { path, doc, top, cur, image, error } shown now
  let diffView = false; // current is shown as its diff, not its text
  const opened = new Map(); // path -> the same, for the workspace viewW
  const wanted = new Map(); // path -> {line, col} a link asked for, applied once its text shows

  vp.addEventListener('scroll', () => {
    if (current) current.top = vp.scrollTop;
    renderRows();
  });
  new ResizeObserver(() => renderRows()).observe(vp);
  rows.addEventListener('mouseup', placeCursor);
  on('setting:changed', key => {
    if (key !== 'editor.tabSize') return;
    tabSize = parseInt(getSetting(key), 10) || 4;
    document.documentElement.style.setProperty('--tab-size', String(tabSize));
    drawStatus();
  });

  // announce tells lsp.js the text file in front, or that there is none, so
  // the LSP item can take its language's name and start its server.
  function announce() {
    if (!ownsStatus()) return;
    const d = current?.doc;
    emit('file:shown', d ? { ws: viewW?.id, path: current.path, lang: d.lang, missing: d.lspMissing } : null);
  }

  // drawStatus fills #st-file for a text file in front, and hides it for
  // anything else: an image, an error, a file still loading. A diff has no
  // cursor, so no Ln / Col.
  function drawStatus() {
    if (!ownsStatus()) return;
    const d = current?.doc;
    if (!d || diffView) closeGoto();
    if (!d) return drawFileStatus(null);
    const { line, col } = current.cur || { line: 1, col: 1 };
    drawFileStatus({
      pos: diffView ? '' : `Ln ${line}, Col ${col}`,
      total: d.total, size: d.size, tabSize, encoding: d.encoding, eol: d.eol,
    });
  }

  async function loadFile(f) {
    const from = viewW;
    const res = await load(viewW, f.path, 0, VIEW_CHUNK);
    if (from !== viewW || opened.get(f.path) !== f) return; // switched away or closed while loading
    if (!res.ok) {
      f.error = res.data?.error || 'Could not open the file';
    } else if (res.data.image) {
      f.image = true;
    } else {
      const j = res.data;
      f.doc = {
        lang: j.lang, total: j.total, lines: new Array(j.total), chunks: new Set(), pending: new Set(),
        eol: j.eol || '', encoding: j.encoding || '', size: j.size || 0,
        lspMissing: j.lsp?.missing || '',
      };
      takeChunk(f.doc, 0, j);
    }
    if (current === f) paint();
  }

  // takeChunk stores a chunk's lines. Lines highlighted by the fast first
  // pass are not exact; when the backend says a better pass is coming, the
  // chunk is fetched again shortly.
  function takeChunk(d, c, j) {
    for (let i = 0; i < j.lines.length; i++) d.lines[j.start + i] = j.lines[i];
    d.pending.delete(c);
    if (j.exact || !j.refine) d.chunks.add(c);
    else setTimeout(() => current?.doc === d && renderRows(), 500);
  }

  function paint() {
    const f = current;
    drawStatus();
    announce();
    if (diffView || !f) return;
    rows.innerHTML = '';
    rows.style.transform = '';
    sizer.style.height = '0px';
    vp.classList.toggle('o-image', f.image);
    if (f.error) {
      if (meta) meta.textContent = '';
      const p = document.createElement('p');
      p.className = 'o-note o-err';
      p.textContent = f.error;
      rows.replaceChildren(p);
      return;
    }
    if (f.image) {
      if (meta) meta.textContent = 'image';
      const img = document.createElement('img');
      img.src = rawURL(viewW, f.path);
      img.alt = f.path;
      rows.replaceChildren(img);
      return;
    }
    const d = f.doc;
    if (!d) {
      if (meta) meta.textContent = 'Loading…';
      return;
    }
    if (meta) meta.textContent = '';
    rows.style.setProperty('--gw', String(String(d.total).length));
    sizer.style.height = `${d.total * rowH() + 100}px`;
    vp.scrollTop = f.top;
    renderRows();
    takeWanted();
  }

  function takeWanted() {
    const w = current && wanted.get(current.path);
    if (!w) return;
    wanted.delete(current.path);
    goToLine(w.line, w.col);
  }

  // placeCursor puts the cursor where a click (or the end of a drag) left
  // the selection: its row's line, and its column with tabs counted to the
  // next tab stop. On the line numbers it is column 1.
  function placeCursor(e) {
    if (!current?.doc) return;
    const sel = getSelection();
    const node = sel.focusNode?.nodeType === Node.TEXT_NODE ? sel.focusNode.parentElement : sel.focusNode;
    const inCode = !e.target.closest('.g') && rows.contains(node) && node?.closest?.('.c');
    const row = inCode ? node.closest('.row') : e.target.closest('.row');
    if (!row) return;
    let col = 1;
    if (inCode) {
      const r = document.createRange();
      r.setStart(row.querySelector('.c'), 0);
      r.setEnd(sel.focusNode, sel.focusOffset);
      col = tabCol(r.toString(), tabSize);
    }
    setCursor(Number(row.querySelector('.g').textContent), col);
  }

  function setCursor(line, col) {
    current.cur = { line, col };
    for (const row of rows.children) {
      row.classList.toggle('cur', Number(row.firstChild?.textContent) === line);
    }
    drawStatus();
  }

  // goToLine moves the cursor to line, a third of the way down the view.
  function goToLine(line, col) {
    if (!current?.doc) return;
    line = Math.min(Math.max(1, line), current.doc.total);
    vp.scrollTop = Math.max(0, (line - 1) * rowH() - vp.clientHeight / 3);
    current.top = vp.scrollTop;
    renderRows();
    setCursor(line, Math.max(1, col || 1));
  }

  function ensureChunks(d, first, last) {
    const from = viewW;
    const c0 = Math.floor(first / VIEW_CHUNK);
    const c1 = Math.floor(Math.max(first, last - 1) / VIEW_CHUNK);
    for (let c = c0; c <= c1; c++) {
      if (d.chunks.has(c) || d.pending.has(c)) continue;
      d.pending.add(c);
      load(viewW, current.path, c * VIEW_CHUNK, VIEW_CHUNK).then(res => {
        if (from !== viewW) return;
        if (!res.ok) return d.pending.delete(c);
        takeChunk(d, c, res.data);
        if (current?.doc === d) renderRows();
      });
    }
  }

  function renderRows() {
    const d = current?.doc;
    if (!d || diffView || !vp.clientHeight) return;
    const lineH = rowH();
    const first = Math.max(0, Math.floor(vp.scrollTop / lineH) - VIEW_OVERSCAN);
    const count = Math.ceil(vp.clientHeight / lineH) + VIEW_OVERSCAN * 2;
    const last = Math.min(d.total, first + count);
    ensureChunks(d, first, last);
    const cur = current.cur?.line;
    let html = '';
    for (let i = first; i < last; i++) {
      html += `<div class="row${i + 1 === cur ? ' cur' : ''}"><span class="g">${i + 1}</span><span class="c">${d.lines[i] ?? ''}</span></div>`;
    }
    rows.style.transform = `translateY(${first * lineH}px)`;
    rows.innerHTML = html;
  }

  const view = {
    // show displays path of workspace w ({id, …}: what load and rawURL take),
    // loading it on first view. With diff, the tab shows the file's diff
    // instead (another module draws it): only the status is this view's.
    show(w, path, diff = false) {
      if (w.id !== viewW?.id) {
        opened.clear();
        wanted.clear();
      }
      viewW = w;
      let f = opened.get(path);
      if (!f) {
        f = { path, doc: null, top: 0, cur: null, image: false, error: '' };
        opened.set(path, f);
        loadFile(f);
      }
      current = f;
      diffView = diff;
      if (title) title.textContent = path;
      paint();
    },
    // hide is for something else in front: the status drops the file.
    hide() {
      current = null;
      drawStatus();
      announce();
    },
    // forget drops the files of workspace wsId no longer open in a tab.
    forget(wsId, paths) {
      if (wsId !== viewW?.id) return;
      for (const p of opened.keys()) if (!paths.includes(p)) opened.delete(p);
      if (current && !opened.has(current.path)) view.hide();
    },
    // revealLine puts path's cursor on line (and col) the next time its
    // text is in front, which is now if it already is.
    revealLine(path, line, col) {
      wanted.set(path, { line, col });
      if (current?.path === path && current.doc && !diffView) takeWanted();
    },
    // claimStatus redraws the page's file status from this view, for when
    // it has just become the view that owns it.
    claimStatus() {
      drawStatus();
      announce();
    },
    path: () => current?.path || '',
    ownsStatus,
    canGoto: () => !!current?.doc && !diffView,
    total: () => current?.doc?.total || 0,
    goToLine,
  };
  views.add(view);
  return view;
}

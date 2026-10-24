import { $, S, doc_, api, apiPost, withKeys } from './state.js';
import { previewing, previewKind } from './markdown.js';
import { layoutPref } from './diff.js';
import { lspServers, lspRows, lspAllButton } from './lspmenu.js';
import { fmtBytes } from './format.js';
import { createResPanel } from './respanel.js';

export function updateStatus() {
  const d = doc_();
  const sizeEl = $('#st-size');
  if (sizeEl) sizeEl.textContent = d ? fmtBytes(d.size) : '';

  if (d && d.isImage) {
    const posEl = $('#st-pos');
    if (posEl) {
      const zoomText = d.imageFit ? `Fit (${Math.round((d.imageScale || 1) * 100)}%)` : `${Math.round((d.imageScale || 1) * 100)}%`;
      posEl.textContent = d.imageMeta ? `${d.imageMeta.width} × ${d.imageMeta.height} px · ${zoomText}` : zoomText;
    }
  }

  // Markdown and CSV/TSV tabs share the preview switch; only its label differs.
  // Hidden while editing: the editor is the source, and the switch has nothing to show.
  const kind = d && d.isEditing ? '' : previewKind(d), isMd = !!kind, shown = previewing(d);
  const label = kind === 'table' ? 'Table' : 'Preview';
  const mdBtn = $('[data-action="md-preview"]');
  if (mdBtn) {
    mdBtn.hidden = !isMd;
    mdBtn.classList.toggle('active', shown);
    const l = $('.footer-btn-label', mdBtn);
    if (l && isMd) l.textContent = label;
  }
  const sw = $('#md-switch');
  if (sw) {
    sw.hidden = !isMd;
    const pb = $('[data-md="preview"]', sw);
    if (pb && isMd) pb.textContent = label;
    document.body.classList.toggle('md-tab', isMd);
    for (const b of sw.children) {
      if (b.dataset.md) b.classList.toggle('on', isMd && (b.dataset.md === 'preview') === shown);
    }
  }


  const isCode = d && !d.isImage;
  const inGit = !!S.meta?.git;
  const hasDiff = !!(d && d.diffAvailable);
  const isDiffOn = !!(d && d.diffMode);
  const currentLayout = (d && d.diffMode) || layoutPref();
  const dsw = $('#diff-switch');
  if (dsw) {
    const showSwitch = inGit && isCode;
    dsw.hidden = !showSwitch;
    document.body.classList.toggle('diff-tab', hasDiff);
    const btn = $('#diff-btn');
    if (btn) {
      btn.disabled = !hasDiff;
      btn.classList.toggle('disabled', !hasDiff);
      btn.classList.toggle('on', hasDiff && isDiffOn);
      btn.title = hasDiff
        ? withKeys(`Show changes against HEAD, ${currentLayout === 'unified' ? 'unified' : 'split'} ({Mod+D})`)
        : 'There are no git modified files.';
    }
    const srcBtn = $('#diff-source');
    if (srcBtn) {
      srcBtn.classList.toggle('on', !hasDiff || !isDiffOn);
      srcBtn.title = withKeys('Show the file ({Mod+D})');
    }
    const menuItems = dsw.querySelectorAll('.diff-menu-item');
    for (const item of menuItems) {
      item.classList.toggle('active', item.dataset.diffOpt === currentLayout);
    }
  }

  const verEl = $('#st-ver');
  if (verEl && S.meta?.version) {
    verEl.textContent = 'v' + S.meta.version;
    verEl.title = `px0 v${S.meta.version} (Click for shortcuts & help)`;
  }
  drawLspStatus();
}

let noteTimer = null;

export function setStatusNote(msg, timeoutMs = 0) {
  if (noteTimer) {
    clearTimeout(noteTimer);
    noteTimer = null;
  }
  const el = $('#st-pos');
  if (el) el.textContent = msg || '';
  if (msg && timeoutMs > 0) {
    noteTimer = setTimeout(() => {
      if (el && el.textContent === msg) el.textContent = '';
      noteTimer = null;
    }, timeoutMs);
  }
}


export function setLspState(j) {
  if (!j || !j.state) return;
  S.lsp.state = j.state;
  S.lsp.server = j.server || S.lsp.server;
  // Only file, warm and start replies say what is missing; any running server means nothing is.
  if ('missing' in j || j.state !== 'off') S.lsp.missing = j.missing || '';
  if ('anyRunning' in j) S.lsp.anyRunning = j.anyRunning;
  drawLspStatus();
}

export function drawLspStatus() {
  const el = $('#st-lsp');
  if (!el) return;
  const anyRunning = S.lsp.anyRunning || S.lsp.state === 'ready' || S.lsp.state === 'indexing';
  el.textContent = 'LSP';
  if (anyRunning) {
    el.dataset.state = S.lsp.state === 'indexing' ? 'indexing' : 'ready';
    el.title = 'Language Servers: Active (Click to manage)';
  } else if (S.lsp.state === 'failed') {
    el.dataset.state = 'failed';
    el.title = 'Language Server failed (Click to manage)';
  } else {
    el.dataset.state = 'off';
    el.title = 'Language Servers: Stopped (Click to manage)';
  }
}

const lspMenuEl = $('#lsp-menu');

// The menu's server list is lspmenu.js's, shared with the agents page.
const lspList = lspServers({
  request: (method, path, params) => (method === 'GET' ? api : apiPost)('/' + path, params),
  show: showLspData,
  open: () => !!lspMenuEl && !lspMenuEl.hidden,
});

// showLspData takes every servers reply: the LSP chip's state, and the menu
// while it is open.
function showLspData(data) {
  if (!data) return;
  S.lsp.anyRunning = data.anyRunning;
  const own = (data.servers || []).find(s => s.name === S.lsp.server);
  // Only a server the user stopped turns the chip off: one stopped after
  // being idle stays requestable, so the next hover or definition restarts it.
  if (own && !own.running && own.userStopped && (S.lsp.state === 'ready' || S.lsp.state === 'indexing')) S.lsp.state = 'off';
  drawLspStatus();
  if (lspMenuEl && !lspMenuEl.hidden) {
    renderLspMenu(data);
    placeLspMenu();
  }
}

function renderLspMenu(data) {
  lspMenuEl.innerHTML = `
    <div class="lsp-menu-head">
      <span>Language Servers</span>
      ${lspAllButton(data)}
    </div>
    <div class="lsp-server-list">${lspRows(data)}</div>`;
}

export function closeLspMenu() {
  if (lspMenuEl) lspMenuEl.hidden = true;
}

function placeLspMenu() {
  const contEl = $('#st-lsp');
  if (!contEl || !lspMenuEl) return;
  const r = contEl.getBoundingClientRect();
  lspMenuEl.style.bottom = (innerHeight - r.top + 6) + 'px';
  lspMenuEl.style.right = Math.max(8, innerWidth - r.right) + 'px';
  lspMenuEl.style.left = 'auto';
}

export async function refreshLspMenu() {
  try {
    await lspList.refresh();
  } catch {}
}

export async function openLspMenu() {
  if (!lspMenuEl) return;
  lspMenuEl.hidden = false;
  placeLspMenu();
  try {
    await lspList.refresh();
  } catch {
    lspMenuEl.innerHTML = `<div class="lsp-empty-note">Failed to load language servers</div>`;
  }
}

export async function toggleLspMenu() {
  if (!lspMenuEl) return;
  if (!lspMenuEl.hidden) {
    closeLspMenu();
    return;
  }
  await openLspMenu();
}

export function initLspMenu() {
  const contEl = $('#st-lsp');
  if (contEl) {
    contEl.addEventListener('click', (e) => {
      e.stopPropagation();
      toggleLspMenu();
    });
    contEl.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        toggleLspMenu();
      }
    });
  }

  if (lspMenuEl) {
    lspMenuEl.addEventListener('click', (e) => {
      const btn = e.target.closest('button[data-act]');
      if (!btn) return;
      e.stopPropagation();
      lspList.act(btn);
    });
  }

  addEventListener('click', (e) => {
    const target = /** @type {HTMLElement|null} */ (e.target);
    if (!target?.closest('#lsp-menu, #st-lsp')) closeLspMenu();
  });
  addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeLspMenu();
  });
  window.addEventListener('resize', () => {
    if (lspMenuEl && !lspMenuEl.hidden) placeLspMenu();
  });
}

// noteMetrics takes this editor's own metrics (/api/meta, the event stream's
// metrics frames) for what the LSP item shows: whether any server runs.
export function noteMetrics(m) {
  if (!m || m.lspEnabled === undefined) return;
  if (m.lspMemBytes > 0) {
    S.lsp.anyRunning = true;
  } else if (S.lsp.state !== 'indexing' && S.lsp.state !== 'starting') {
    S.lsp.anyRunning = false;
  }
  drawLspStatus();
}

/* The resource button and panel, as on the agents page (respanel.js). Opened
   from the agents page, the editor reads the workbench's /api/resources, so
   it shows all of punk0; a PR review run on its own is all of punk0 itself,
   so it shows its own /api/metrics. */
let resPanel = null;

async function loadResources() {
  if (S.meta?.workbench) {
    try {
      const r = await fetch(new URL(S.meta.workbench + 'api/resources', location.origin));
      return r.ok ? await r.json() : null;
    } catch {
      return null;
    }
  }
  try {
    const m = await api('/api/metrics');
    const own = { cpu: m.cpuUsage || 0, rss: m.rssBytes || 0, goroutines: m.goroutines || 0 };
    const lsp = m.lspMemBytes || 0;
    return {
      cpu: own.cpu, rss: own.rss + lsp, goroutines: own.goroutines,
      self: own, workspaces: [], reviews: [], standalone: true,
      lsp: lsp ? { cpu: 0, rss: lsp } : null,
    };
  } catch {
    return null;
  }
}

export function refreshResources() {
  return resPanel?.refresh();
}

// initResources polls every 3 s while the page is visible, like the agents page.
export function initResources() {
  resPanel = createResPanel($('#st-res'), loadResources);
  const tick = () => { if (!document.hidden) refreshResources(); };
  setInterval(tick, 3000);
  document.addEventListener('visibilitychange', tick);
  tick();
}

/* The status bar stays on one line. When its contents outgrow the width, it
   sheds detail in steps (see the fit-N rules in style.css), least useful first,
   stopping at the first step that fits. A step comes back only with FIT_SLACK px
   to spare, so a 1px wobble of a resizer cannot flip it on every move. */
const FIT_STEPS = 6, FIT_SLACK = 16;
const statusEl = $('#status');
const growEl = $('.grow', statusEl);
let fitLevel = 0;
let appliedFit = -1;
let fitRAF = 0;

/* Apply fit level n and return the room left: the .grow spacer's width, or the
   overflow as a negative number. */
function applyFit(n) {
  if (n !== appliedFit) {
    for (let i = 1; i <= FIT_STEPS; i++) statusEl.classList.toggle('fit-' + i, i <= n);
    appliedFit = n;
  }
  return growEl.offsetWidth - (statusEl.scrollWidth - statusEl.clientWidth);
}

export function fitStatus() {
  let room = applyFit(fitLevel);
  while (fitLevel < FIT_STEPS && room < 0) room = applyFit(++fitLevel);
  // Showing more only ever shrinks the room, so stop as soon as it is short.
  while (fitLevel > 0 && room >= FIT_SLACK) {
    room = applyFit(fitLevel - 1);
    if (room < FIT_SLACK) { applyFit(fitLevel); return; }
    fitLevel--;
  }
}

function scheduleFitStatus() {
  if (fitRAF) return;
  fitRAF = requestAnimationFrame(() => {
    fitRAF = 0;
    fitStatus();
  });
}

export function initStatusFit() {
  // Width changes come from the window and the sidebar resizers; content changes
  // from metrics, LSP state and the selection bar. Class changes are not observed,
  // so fitStatus() toggling them cannot re-trigger itself.
  new ResizeObserver(fitStatus).observe(statusEl);
  new MutationObserver(scheduleFitStatus).observe(statusEl, { childList: true, subtree: true, characterData: true });
  document.fonts?.ready.then(fitStatus);
}

// web/src/respanel.js
// The resource panel and the status bar button that opens it, shared by the
// agents page (orch/resources.js) and the editor (status.js). The button
// reads running terminals · running agents · total RAM; the panel is a table
// of CPU and RAM per workspace, its editor, its language servers and each
// terminal, pull request reviews and punk0 itself, with the goroutines of
// every punk0 process, as px0's metrics showed them. The figures and names
// come from the workbench's GET /api/resources (wbresources.go); each page
// says how to get them. Depends only on state.js and format.js, which both
// bundles carry.
import { esc } from './state.js';
import { fmtBytes } from './format.js';

const HISTORY = 40; // samples per sparkline: two minutes at the 3s poll

export const fmtRAM = n => (n >= 1 << 30 ? (n / (1 << 30)).toFixed(2) + ' GB' : fmtBytes(n));
const fmtCPU = p => p.toFixed(1) + '%';
const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`;

const svg = (body, sw = 2, size = 12) => `<svg viewBox="0 0 24 24" width="${size}" height="${size}" fill="none" stroke="currentColor" stroke-width="${sw}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${body}</svg>`;
const TERM = '<polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/>';
const AGENT = '<path d="M12 8V4H8"/><rect x="4" y="8" width="16" height="12" rx="2"/><path d="M2 14h2M20 14h2M15 13v2M9 13v2"/>';
const RAM = '<path d="M6 19v-3M10 19v-3M14 19v-3M18 19v-3M8 11V9M12 11V9M16 11V9M2 15h20"/><path d="M2 7a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v1.1a2 2 0 0 0 0 3.837V17a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-5.1a2 2 0 0 0 0-3.837Z"/>';
const ICON_EDITOR = svg('<path d="m16 18 6-6-6-6"/><path d="m8 6-6 6 6 6"/>');
const ICON_LSP = svg('<path d="M8 3H7a2 2 0 0 0-2 2v5a2 2 0 0 1-2 2 2 2 0 0 1 2 2v5a2 2 0 0 0 2 2h1"/><path d="M16 21h1a2 2 0 0 0 2-2v-5a2 2 0 0 1 2-2 2 2 0 0 1-2-2V5a2 2 0 0 0-2-2h-1"/>');
const ICON_PR = svg('<circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M13 6h3a2 2 0 0 1 2 2v7"/><line x1="6" y1="9" x2="6" y2="21"/>');
const CHEVRON = '<svg viewBox="0 0 10 10" width="9" height="9" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 2l3 3-3 3"/></svg>';

const BUTTON = `
  <span class="res-count">${svg(TERM, 2, 13)}<span data-res="terms">0</span></span>
  <span class="metric-divider">·</span>
  <span class="res-count">${svg(AGENT, 2, 13)}<span data-res="agents">0</span></span>
  <span class="metric-divider">·</span>
  <span class="res-count">${svg(RAM, 1.8, 13)}<span data-res="mem">-- MB</span></span>`;

const PANEL = `
  <header class="res-head">
    ${svg(RAM, 1.8, 13)}
    <span class="res-title">Resources</span>
    <button type="button" class="mini" data-res="refresh" title="Refresh" aria-label="Refresh">${svg('<path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/>', 2, 13)}</button>
  </header>
  <div class="res-sum" data-res="sum"></div>
  <div class="res-cols" aria-hidden="true"><span>Name</span><span>CPU</span><span>RAM</span></div>
  <div class="res-rows" data-res="rows"></div>`;

// goroutines is a punk0 process's count, faint after its name.
const goroutines = u => (u.goroutines ? `<span class="res-gor" title="Goroutines">${plural(u.goroutines, 'goroutine')}</span>` : '');
const figures = u => `<span class="res-num">${fmtCPU(u.cpu)}</span><span class="res-num">${fmtRAM(u.rss)}</span>`;

/**
 * createResPanel puts the button into slot and the panel into the page.
 * load() returns the /api/resources payload, or null when it failed; onData,
 * when given, sees every payload (the agents page's LSP item uses it).
 * The returned refresh() runs one poll: the sparklines need every sample, so
 * call it on the page's poll, not only while the panel is open.
 * @param {HTMLElement} slot
 * @param {() => Promise<any>} load
 * @param {(data: any) => void} [onData]
 */
export function createResPanel(slot, load, onData) {
  slot.innerHTML = `<button type="button" class="res-btn" aria-haspopup="dialog" aria-expanded="false" aria-controls="res-panel">${BUTTON}</button>`;
  const btn = /** @type {HTMLElement} */ (slot.firstElementChild);
  const panel = document.createElement('section');
  panel.id = 'res-panel';
  panel.className = 'res';
  panel.setAttribute('role', 'dialog');
  panel.setAttribute('aria-label', 'Resources');
  panel.hidden = true;
  panel.innerHTML = PANEL;
  document.body.append(panel);
  const part = name => /** @type {HTMLElement} */ (panel.querySelector(`[data-res="${name}"]`) || btn.querySelector(`[data-res="${name}"]`));
  const rowsEl = part('rows');

  let last = null;
  const history = new Map(); // row key -> recent CPU percentages
  const collapsed = new Set(); // workspace ids folded shut

  function remember(key, cpu, seen) {
    seen.add(key);
    const h = history.get(key) || [];
    h.push(cpu);
    if (h.length > HISTORY) h.shift();
    history.set(key, h);
  }

  // sparkline draws a row's recent CPU. The scale floors at 5% so an idle
  // process stays a flat line instead of magnifying noise.
  function sparkline(key) {
    const h = history.get(key) || [];
    if (h.length < 2) return '<span class="res-spark"></span>';
    const W = 56, H = 14;
    const top = Math.max(5, ...h);
    const pts = h.map((v, i) => `${(W * (i + HISTORY - h.length) / (HISTORY - 1)).toFixed(1)},${(H - 1 - (H - 2) * v / top).toFixed(1)}`).join(' ');
    return `<svg class="res-spark" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" aria-hidden="true"><polyline points="${pts}" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round"/></svg>`;
  }

  function render() {
    if (!last) return;
    part('sum').innerHTML = `<span class="res-big">${fmtCPU(last.cpu)}</span><span class="metric-divider">·</span><span class="res-big">${fmtRAM(last.rss)}</span><span class="res-faint">total</span>` +
      (last.goroutines ? `<span class="metric-divider">·</span><span class="res-big">${last.goroutines}</span><span class="res-faint">goroutines</span>` : '');
    const out = [];
    for (const ws of last.workspaces) {
      const open = !collapsed.has(ws.id);
      out.push(`<div class="res-row res-group" data-ws="${esc(ws.id)}" role="button" tabindex="0" aria-expanded="${open}">
        <span class="res-name"><span class="res-chev${open ? ' open' : ''}">${CHEVRON}</span><span class="res-label">${esc(ws.name || 'Workspace')}</span></span>
        ${sparkline('ws:' + ws.id)}${figures(ws)}</div>`);
      if (!open) continue;
      if (ws.editor) {
        out.push(`<div class="res-row res-sub" title="The folder's editor, without its language servers">
          <span class="res-name"><span class="res-ic">${ICON_EDITOR}</span><span class="res-label">Editor</span>${goroutines(ws.editor)}</span>
          <span></span>${figures(ws.editor)}</div>`);
      }
      if (ws.lsp?.rss > 0) {
        out.push(`<div class="res-row res-sub" title="The folder's language servers (and a git command now and then)">
          <span class="res-name"><span class="res-ic">${ICON_LSP}</span><span class="res-label">Language servers</span></span>
          <span></span>${figures(ws.lsp)}</div>`);
      }
      for (const t of ws.tabs) {
        const agent = t.kind !== 'shell';
        out.push(`<div class="res-row res-sub" title="${agent ? `Agent (${esc(t.kind)})` : 'Shell'}, and everything running in it">
          <span class="res-name"><span class="res-dot${t.status ? ' ' + esc(t.status) : ''}"></span><span class="res-ic">${svg(agent ? AGENT : TERM)}</span><span class="res-label">${esc(t.name || 'Terminal')}</span></span>
          <span></span>${figures(t)}</div>`);
      }
    }
    for (const r of last.reviews) {
      out.push(`<div class="res-row">
        <span class="res-name"><span class="res-ic">${ICON_PR}</span><span class="res-label">${esc(r.name)}</span>${goroutines(r)}</span>
        ${sparkline('item:' + r.id)}${figures(r)}</div>`);
    }
    // A PR review run on its own (standalone): its own process and its
    // language servers are all there is.
    if (last.lsp?.rss > 0) {
      out.push(`<div class="res-row" title="This editor's language servers">
        <span class="res-name"><span class="res-ic">${ICON_LSP}</span><span class="res-label">Language servers</span></span>
        <span></span>${figures(last.lsp)}</div>`);
    }
    if (!out.length && !last.standalone) out.push('<p class="res-note">No terminal or editor is running.</p>');
    out.push(`<div class="res-row res-self" title="${last.standalone ? 'This punk0, reviewing a pull request on its own' : 'The punk0 server, and short-lived helpers such as git'}">
      <span class="res-name"><span class="res-label">punk<span class="res-zero">[0]</span></span>${goroutines(last.self)}</span>
      ${sparkline('self')}${figures(last.self)}</div>`);
    rowsEl.innerHTML = out.join('');
  }

  // The button: what runs and what it all takes, from the same payload.
  function renderButton() {
    const tabs = last.workspaces.flatMap(w => w.tabs);
    const agents = tabs.filter(t => t.kind !== 'shell').length;
    const terms = tabs.length - agents;
    part('terms').textContent = String(terms);
    part('agents').textContent = String(agents);
    part('mem').textContent = fmtRAM(last.rss);
    btn.title = `${plural(terms, 'terminal')} and ${plural(agents, 'agent')} running. Click for what each uses`;
  }

  async function refresh() {
    const data = await load();
    if (!data) return null;
    last = data;
    const seen = new Set();
    remember('self', last.self.cpu, seen);
    for (const ws of last.workspaces) remember('ws:' + ws.id, ws.cpu, seen);
    for (const r of last.reviews) remember('item:' + r.id, r.cpu, seen);
    for (const k of history.keys()) if (!seen.has(k)) history.delete(k);
    renderButton();
    if (!panel.hidden) render();
    onData?.(last);
    return last;
  }

  function place() {
    const r = btn.getBoundingClientRect();
    panel.style.bottom = (innerHeight - r.top + 6) + 'px';
    panel.style.right = Math.max(8, innerWidth - r.right) + 'px';
  }

  function setOpen(open) {
    panel.hidden = !open;
    btn.setAttribute('aria-expanded', String(open));
    if (!open) return;
    render();
    place();
    refresh();
  }

  function toggleGroup(row) {
    const id = row.dataset.ws;
    if (collapsed.has(id)) collapsed.delete(id);
    else collapsed.add(id);
    render();
  }

  btn.addEventListener('click', e => {
    e.stopPropagation();
    setOpen(panel.hidden);
  });
  part('refresh').addEventListener('click', () => refresh());
  rowsEl.addEventListener('click', e => {
    const row = /** @type {HTMLElement|null} */ (/** @type {HTMLElement} */ (e.target).closest('.res-group'));
    if (row) toggleGroup(row);
  });
  rowsEl.addEventListener('keydown', e => {
    const row = /** @type {HTMLElement|null} */ (/** @type {HTMLElement} */ (e.target).closest('.res-group'));
    if (row && (e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault();
      toggleGroup(row);
    }
  });
  // composedPath, not e.target.closest: folding a group re-renders the rows,
  // detaching the clicked node before this listener runs.
  addEventListener('click', e => {
    const path = e.composedPath();
    if (!panel.hidden && !path.includes(panel) && !path.includes(btn)) setOpen(false);
  });
  addEventListener('keydown', e => {
    if (e.key === 'Escape' && !panel.hidden) {
      setOpen(false);
      btn.focus();
    }
  });
  addEventListener('resize', () => panel.hidden || place());

  return { refresh };
}

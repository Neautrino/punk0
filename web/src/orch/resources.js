// web/src/orch/resources.js
// The resource panel the status bar's terminals · agents · RAM button opens:
// CPU and RAM per workspace, its editor and each terminal, pull request
// reviews, and punk0 itself, from GET /api/resources (wbresources.go). The
// server sends ids and figures; names and terminal states come from the store.
import { $, esc } from '../state.js';
import { fmtBytes } from '../metricsview.js';
import { call } from './api.js';
import { state, subscribe, workspace, projectOf, wsLabel } from './store.js';

const btn = $('#st-res');
const panel = $('#wb-res');
const rowsEl = $('#wb-res-rows');
const HISTORY = 40; // samples per sparkline: two minutes at the 3s poll

let last = null;
const history = new Map(); // row key -> recent CPU percentages
const collapsed = new Set(); // workspace ids folded shut

export const fmtRAM = n => (n >= 1 << 30 ? (n / (1 << 30)).toFixed(2) + ' GB' : fmtBytes(n));
const fmtCPU = p => p.toFixed(1) + '%';

const ICON_TERM = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>';
const ICON_AGENT = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8"/><rect x="4" y="8" width="16" height="12" rx="2"/><path d="M2 14h2M20 14h2M15 13v2M9 13v2"/></svg>';
const ICON_EDITOR = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m16 18 6-6-6-6"/><path d="m8 6-6 6 6 6"/></svg>';
const ICON_PR = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M13 6h3a2 2 0 0 1 2 2v7"/><line x1="6" y1="9" x2="6" y2="21"/></svg>';
const CHEVRON = '<svg viewBox="0 0 10 10" width="9" height="9" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 2l3 3-3 3"/></svg>';

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
  if (h.length < 2) return '<span class="wb-res-spark"></span>';
  const W = 56, H = 14;
  const top = Math.max(5, ...h);
  const pts = h.map((v, i) => `${(W * (i + HISTORY - h.length) / (HISTORY - 1)).toFixed(1)},${(H - 1 - (H - 2) * v / top).toFixed(1)}`).join(' ');
  return `<svg class="wb-res-spark" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" aria-hidden="true"><polyline points="${pts}" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round"/></svg>`;
}

const figures = u => `<span class="wb-res-num">${fmtCPU(u.cpu)}</span><span class="wb-res-num">${fmtRAM(u.rss)}</span>`;

function workspaceName(id) {
  const w = workspace(id);
  const p = projectOf(id);
  if (!w) return 'Workspace';
  if (p?.scratch) return wsLabel(w);
  return p && p.name !== w.name ? `${p.name} / ${w.name}` : w.name;
}

function render() {
  if (!last) return;
  $('#wb-res-sum').innerHTML = `<span class="wb-res-big">${fmtCPU(last.cpu)}</span><span class="metric-divider">·</span><span class="wb-res-big">${fmtRAM(last.rss)}</span><span class="wb-res-faint">total</span>`;
  const out = [];
  for (const ws of last.workspaces) {
    const open = !collapsed.has(ws.id);
    const tabs = workspace(ws.id)?.tabs || [];
    out.push(`<div class="wb-res-row wb-res-group" data-ws="${esc(ws.id)}" role="button" tabindex="0" aria-expanded="${open}">
      <span class="wb-res-name"><span class="wb-res-chev${open ? ' open' : ''}">${CHEVRON}</span><span class="wb-res-label">${esc(workspaceName(ws.id))}</span></span>
      ${sparkline('ws:' + ws.id)}${figures(ws)}</div>`);
    if (!open) continue;
    if (ws.editor) {
      out.push(`<div class="wb-res-row wb-res-sub" title="The folder's editor and its language servers">
        <span class="wb-res-name"><span class="wb-res-ic">${ICON_EDITOR}</span><span class="wb-res-label">Editor</span></span>
        <span></span>${figures(ws.editor)}</div>`);
    }
    for (const r of ws.tabs) {
      const t = tabs.find(x => x.id === r.id);
      const agent = t?.kind === 'claude';
      out.push(`<div class="wb-res-row wb-res-sub" title="${agent ? 'Claude Code' : 'Shell'}, and everything running in it">
        <span class="wb-res-name"><span class="o-tab-dot on${t?.status ? ' ' + esc(t.status) : ''}"></span><span class="wb-res-ic">${agent ? ICON_AGENT : ICON_TERM}</span><span class="wb-res-label">${esc(t?.name || 'Terminal')}</span></span>
        <span></span>${figures(r)}</div>`);
    }
  }
  for (const r of last.reviews) {
    const item = state.reviews.find(x => x.id === r.id);
    out.push(`<div class="wb-res-row">
      <span class="wb-res-name"><span class="wb-res-ic">${ICON_PR}</span><span class="wb-res-label">${esc(item?.title || r.name)}</span></span>
      ${sparkline('item:' + r.id)}${figures(r)}</div>`);
  }
  if (!out.length) out.push('<p class="o-note">No terminal or editor is running.</p>');
  out.push(`<div class="wb-res-row wb-res-self" title="The punk0 server, and short-lived helpers such as git">
    <span class="wb-res-name"><span class="wb-res-label">punk<span class="wb-res-zero">[0]</span></span></span>
    ${sparkline('self')}${figures(last.self)}</div>`);
  rowsEl.innerHTML = out.join('');
}

// refresh fetches the figures, updates the bar's total and, when open, the
// panel. The sparklines need every sample, so it runs on each poll.
export async function refreshResources() {
  const res = await call('GET', 'api/resources');
  if (!res.ok) return;
  last = res.data;
  $('#st-mem').textContent = fmtRAM(last.rss);
  const seen = new Set();
  remember('self', last.self.cpu, seen);
  for (const ws of last.workspaces) remember('ws:' + ws.id, ws.cpu, seen);
  for (const r of last.reviews) remember('item:' + r.id, r.cpu, seen);
  for (const k of history.keys()) if (!seen.has(k)) history.delete(k);
  if (!panel.hidden) render();
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
  refreshResources();
}

function toggleGroup(row) {
  const id = row.dataset.ws;
  if (collapsed.has(id)) collapsed.delete(id);
  else collapsed.add(id);
  render();
}

export function initResources() {
  btn.addEventListener('click', e => {
    e.stopPropagation();
    setOpen(panel.hidden);
  });
  $('#wb-res-refresh').addEventListener('click', () => refreshResources());
  rowsEl.addEventListener('click', e => {
    const row = e.target.closest('.wb-res-group');
    if (row) toggleGroup(row);
  });
  rowsEl.addEventListener('keydown', e => {
    const row = e.target.closest('.wb-res-group');
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
  // Names and terminal states come from the store: re-render as they change.
  subscribe(() => panel.hidden || render());
}

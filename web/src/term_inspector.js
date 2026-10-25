// web/src/term_inspector.js
import { $, S } from './state.js';
import { sideShown, rightView, showRightView } from './sideviews.js';
import { on } from './bus.js';
import { showToast } from './ui.js';
import { getTabLabel } from './term-util.js';

let termEl = null;
let tabListEl = null;
let agentMenuEl = null;
let booting = null;
let xtermInstance = null;
let activeSocket = null;
let activeTabId = null;
let workspaceTabs = [];
let pollTimer = null;
let isEnded = false;
const enc = new TextEncoder();

// parentURL is a path of the orchestrator that opened this editor.
export const parentURL = path => {
  const p = path.startsWith('/') ? path.slice(1) : path;
  const base = new URL(S.meta?.workbench || '/', window.location.origin);
  return new URL(p, base).href;
};

const wsURL = path => {
  const u = new URL(parentURL(path));
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
  return u.href;
};

function termColors() {
  const cs = getComputedStyle(document.documentElement);
  const v = name => cs.getPropertyValue(name).trim();
  return {
    background: v('--bg') || '#1e1e2e',
    foreground: v('--fg') || '#cdd6f4',
    cursor: v('--accent') || '#89b4fa',
    cursorAccent: v('--bg') || '#1e1e2e',
    selectionBackground: v('--sel') || 'rgba(137,180,250,0.3)',
  };
}

function boot() {
  if (!booting) {
    booting = Promise.all([
      import(parentURL('static/vendor/xterm.mjs')),
      import(parentURL('static/vendor/addon-fit.mjs')),
    ]).then(([{ Terminal }, { FitAddon }]) => {
      const term = new Terminal({
        fontSize: 12,
        fontFamily: getComputedStyle(document.documentElement).getPropertyValue('--mono').trim() || 'monospace',
        lineHeight: 1.2,
        scrollback: 10000,
        cursorStyle: 'block',
        cursorBlink: true,
        theme: termColors(),
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(termEl);

      const fitNow = () => {
        if (termEl && termEl.clientWidth && termEl.clientHeight) {
          fit.fit();
        }
      };
      fitNow();
      new ResizeObserver(fitNow).observe(termEl);

      on('theme:changed', () => { term.options.theme = termColors(); });

      const send = data => activeSocket?.readyState === WebSocket.OPEN && activeSocket.send(data);
      const sendSize = () => send(JSON.stringify({ cols: term.cols, rows: term.rows }));

      term.onData(s => {
        if (isEnded) return connect();
        if (Date.now() - claimed > 1000) reclaim(); // typing here makes this the latest viewer
        send(enc.encode(s));
      });
      term.onBinary(s => send(Uint8Array.from(s, c => c.charCodeAt(0))));
      term.onResize(sendSize);
      // The agents page may show this tab too, and its PTY has one size: the
      // viewer that reported last (term.go). Report again whenever this pane
      // is used, so the agent draws at this pane's width.
      let claimed = 0;
      const reclaim = () => {
        if (!termEl?.clientWidth || !termEl.clientHeight) return;
        fit.fit();
        sendSize();
        claimed = Date.now();
      };
      termEl.addEventListener('pointerdown', reclaim);
      term.textarea?.addEventListener('focus', reclaim);
      addEventListener('focus', reclaim);
      document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && reclaim());

      term.attachCustomKeyEventHandler(e => {
        if (e.key !== 'Enter' || !e.shiftKey || e.ctrlKey || e.altKey || e.metaKey) return true;
        e.preventDefault();
        if (e.type === 'keydown') send(enc.encode('\x1b\r'));
        return false;
      });

      xtermInstance = { term, fit, sendSize };
      return xtermInstance;
    });
  }
  return booting;
}

function connect() {
  if (activeSocket) {
    activeSocket.onclose = null;
    activeSocket.close();
    activeSocket = null;
  }
  isEnded = false;
  if (!xtermInstance || !activeTabId) return;

  const { term, sendSize } = xtermInstance;
  term.reset();

  const ws = new WebSocket(wsURL(`api/tabs/${encodeURIComponent(activeTabId)}/term`));
  activeSocket = ws;
  ws.binaryType = 'arraybuffer';

  ws.onopen = () => {
    sendSize();
    term.focus();
  };
  ws.onmessage = e => {
    if (typeof e.data !== 'string') term.write(new Uint8Array(e.data));
  };
  ws.onclose = e => {
    if (activeSocket !== ws) return;
    activeSocket = null;
    term.write(`\r\n\x1b[2m[${e.reason || 'disconnected'} · press any key to restart]\x1b[0m\r\n`);
    isEnded = true;
  };
}

export function attachTab(tabId) {
  activeTabId = tabId;
  idleNotice = '';
  renderTabChips();
  boot().then(() => {
    if (activeTabId === tabId) connect();
  }).catch(err => {
    if (termEl) termEl.textContent = `Could not load terminal: ${err.message}`;
  });
}

function getWorkspaceID() {
  return S.meta?.workspace || S.meta?.workspaceId || '';
}

export async function refreshWorkspaceTabs() {
  const wsId = getWorkspaceID();
  if (!wsId) return;

  try {
    const res = await fetch(parentURL(`api/workspaces/${encodeURIComponent(wsId)}/tabs`));
    if (!res.ok) return;
    const data = await res.json();
    workspaceTabs = data.tabs || [];
    if (activeTabId && workspaceTabs.some(t => t.id === activeTabId)) {
      renderTabChips();
      return;
    }
    // Attaching starts a stopped tab's process (handleTerm calls Shell()), so a
    // page load or poll only reattaches to a tab that is already running.
    // A stopped tab starts when the user clicks it.
    const running = workspaceTabs.find(t => t.running);
    if (running) {
      attachTab(running.id);
      return;
    }
    if (activeSocket) {
      activeSocket.onclose = null;
      activeSocket.close();
      activeSocket = null;
    }
    activeTabId = null;
    renderTabChips();
    showIdleNotice(workspaceTabs.length
      ? 'Tabs are stopped. Click one to start it, or use + for a new one.'
      : 'No terminal tabs. Click + to start an agent or a shell.');
  } catch {}
}

let idleNotice = '';
function showIdleNotice(text) {
  if (idleNotice === text) return; // the 3 s poll would otherwise redraw it
  idleNotice = text;
  boot().then(() => {
    if (!xtermInstance || activeTabId || idleNotice !== text) return;
    xtermInstance.term.reset();
    xtermInstance.term.write(`\r\n\x1b[2m${text}\x1b[0m\r\n`);
  }).catch(() => {});
}

function renderTabChips() {
  if (!tabListEl) return;
  tabListEl.innerHTML = '';

  for (const t of workspaceTabs) {
    const btn = document.createElement('button');
    btn.className = 'term-tab-chip' + (t.id === activeTabId ? ' active' : '');
    const dot = document.createElement('span');
    dot.className = 'term-status-dot ' + (t.status === 'working' || t.status === 'waiting' ? 'busy' : 'idle');
    btn.appendChild(dot);

    const name = document.createElement('span');
    name.textContent = getTabLabel(t);
    btn.appendChild(name);

    const closeBtn = document.createElement('span');
    closeBtn.className = 'term-tab-close';
    closeBtn.innerHTML = '&times;';
    closeBtn.title = 'Close tab';
    closeBtn.onclick = (e) => {
      e.stopPropagation();
      removeTab(t.id);
    };
    btn.appendChild(closeBtn);

    btn.onclick = () => attachTab(t.id);
    tabListEl.appendChild(btn);
  }
}

export async function addTab(kind) {
  const wsId = getWorkspaceID();
  if (!wsId) {
    showToast('!', 'No workspace ID available');
    return;
  }

  try {
    const res = await fetch(parentURL(`api/workspaces/${encodeURIComponent(wsId)}/tabs`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind }),
    });
    if (res.ok) {
      const added = await res.json();
      await refreshWorkspaceTabs();
      attachTab(added.id);
    } else {
      const err = await res.json().catch(() => ({}));
      showToast('!', err.error || `Failed to create ${kind} tab`);
    }
  } catch (err) {
    showToast('!', `Failed to create ${kind} tab: ` + err.message);
  }
}

export const addShellTab = () => addTab('shell');
export const addAgentTab = (kind = 'claude') => addTab(kind);

export async function removeTab(tabId) {
  try {
    const res = await fetch(parentURL(`api/tabs/${encodeURIComponent(tabId)}`), {
      method: 'DELETE',
    });
    if (res.ok) {
      if (activeTabId === tabId) {
        if (activeSocket) {
          activeSocket.onclose = null;
          activeSocket.close();
          activeSocket = null;
        }
        activeTabId = null;
      }
      await refreshWorkspaceTabs();
    } else {
      const err = await res.json().catch(() => ({}));
      showToast('!', err.error || 'Failed to close tab');
    }
  } catch (err) {
    showToast('!', 'Failed to close tab: ' + err.message);
  }
}

const sleep = ms => new Promise(r => setTimeout(r, ms));

async function socketOpen(ms = 5000) {
  for (let t = 0; t < ms; t += 100) {
    if (activeSocket?.readyState === WebSocket.OPEN) return true;
    await sleep(100);
  }
  return false;
}

/* A tab the selection can go to: the shown one if it runs an agent, else the
   first agent tab, running ones first. A plain shell is never a target: it
   would run the pasted lines as commands. */
function chatTab() {
  const isAgent = t => t.kind !== 'shell';
  const shown = workspaceTabs.find(t => t.id === activeTabId);
  if (shown && isAgent(shown)) return shown;
  return workspaceTabs.find(t => isAgent(t) && t.running) || workspaceTabs.find(isAgent) || null;
}

/* Add to Chat: types text into an agent's prompt through xterm's paste, which
   brackets it when the agent asked for bracketed paste (Claude Code does), so
   the newlines stay in the input and nothing is sent until the user presses
   Enter. Control characters are dropped so the text cannot end the paste. */
export async function sendToActiveTerminal(text) {
  showTerminal();
  await refreshWorkspaceTabs();
  const tab = chatTab();
  if (!tab) {
    showToast('!', 'Start an agent tab first (+ in the terminal\'s row)');
    return false;
  }
  if (tab.status === 'waiting') {
    showToast('!', getTabLabel(tab) + ' is waiting for your answer');
    return false;
  }
  const wasRunning = tab.running;
  if (activeTabId !== tab.id || !activeSocket) attachTab(tab.id);
  if (!await booting || !await socketOpen()) {
    showToast('!', 'Could not connect to ' + getTabLabel(tab));
    return false;
  }
  if (!wasRunning) {
    // Just started: give the agent time to draw its prompt before typing into
    // it. Claude reports idle through its hooks; others report nothing.
    for (let t = 0; t < 20000; t += 500) {
      await sleep(500);
      if (tab.kind !== 'claude' && t >= 2500) break;
      await refreshWorkspaceTabs();
      if (workspaceTabs.find(x => x.id === tab.id)?.status === 'idle') break;
    }
  }
  const clean = text.replace(/\r\n?/g, '\n').replace(/[\x00-\x08\x0b-\x1f\x7f]/g, '');
  xtermInstance.term.paste(clean + '\n');
  xtermInstance.term.focus();
  showToast('✓', 'Added to ' + getTabLabel(tab));
  return true;
}

const termShown = () => sideShown('right') && rightView() === 'term';

// showTerminal shows the right side on its Terminal mode and puts the caret
// in it.
export function showTerminal() {
  if (termShown()) return onTermInspectorShown();
  showRightView('term'); // 'side:toggled' or 'rightview:changed' calls onTermInspectorShown
}

// openTab shows tab id in the terminal, a tab the orchestrator has just
// opened (a resumed chat). Attaching first keeps the tab list's refresh from
// picking another running tab meanwhile.
export function openTab(id) {
  showTerminal();
  attachTab(id);
  refreshWorkspaceTabs();
}

export function onTermInspectorShown() {
  refreshWorkspaceTabs();
  if (xtermInstance?.fit) {
    setTimeout(() => {
      xtermInstance.fit.fit();
      xtermInstance.term.focus();
    }, 50);
  }
  if (!pollTimer) {
    pollTimer = setInterval(() => {
      if (termShown()) {
        refreshWorkspaceTabs();
      }
    }, 3000);
  }
}

const SUPPORTED_AGENTS = [
  { id: 'claude', label: 'Claude Code', icon: '✳' },
  { id: 'codex', label: 'Codex', icon: '◆' },
  { id: 'opencode', label: 'OpenCode', icon: '◆' },
  { id: 'agy', label: 'Antigravity', icon: '◆' },
  { id: 'cursor-agent', label: 'Cursor Agent', icon: '◆' },
];

// The + menu: a tab for each agent, then a shell.
function toggleAgentMenu(e) {
  e.stopPropagation();
  if (!agentMenuEl.hidden) {
    agentMenuEl.hidden = true;
    return;
  }
  const item = (icon, label, run) => {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.setAttribute('role', 'menuitem');
    btn.innerHTML = `${icon}<span>${label}</span>`;
    btn.onclick = ev => {
      ev.stopPropagation();
      agentMenuEl.hidden = true;
      run();
    };
    return btn;
  };
  agentMenuEl.replaceChildren(
    ...SUPPORTED_AGENTS.map(a => item(`<span class="agent-icon${a.id === 'claude' ? ' claude' : ''}">${a.icon}</span>`, a.label, () => addAgentTab(a.id))),
    item('<span class="agent-icon">$</span>', 'Shell', addShellTab),
  );
  agentMenuEl.hidden = false;
}

export function initTermInspector() {
  termEl = $('#editor-term-container');
  tabListEl = $('#term-inspector-tabs');
  agentMenuEl = $('#term-agent-menu');
  const addBtn = $('#btn-term-add');
  addBtn.onclick = toggleAgentMenu;
  document.addEventListener('click', e => {
    if (!agentMenuEl.hidden && !agentMenuEl.contains(e.target) && !addBtn.contains(e.target)) agentMenuEl.hidden = true;
  });
  on('side:toggled', ({ side, shown }) => { if (side === 'right' && shown && rightView() === 'term') onTermInspectorShown(); });
  on('rightview:changed', ({ view }) => { if (view === 'term' && sideShown('right')) onTermInspectorShown(); });
}

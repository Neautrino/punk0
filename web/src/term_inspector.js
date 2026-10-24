// web/src/term_inspector.js
import { $, S } from './state.js';
import { on } from './bus.js';
import { showToast } from './ui.js';

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

const parentURL = path => {
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

export function getTabLabel(t) {
  if (t.name && t.name !== t.kind) return t.name;
  if (t.kind === 'claude') return 'Claude Code';
  if (t.kind === 'shell') return 'Shell';
  if (t.kind === 'codex') return 'Codex';
  if (t.kind === 'opencode') return 'OpenCode';
  if (t.kind === 'agy') return 'Antigravity';
  if (t.kind === 'cursor-agent') return 'Cursor Agent';
  return t.name || t.kind;
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
      ? 'Tabs are stopped. Click one to start it, or use + Agent / + Shell.'
      : 'No terminal tabs. Click + Agent or + Shell to start.');
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

export async function sendToActiveTerminal(text) {
  if (!activeTabId) {
    await refreshWorkspaceTabs();
  }
  if (!activeTabId) {
    showToast('!', 'No terminal tab available');
    return false;
  }

  try {
    const res = await fetch(parentURL(`api/tabs/${encodeURIComponent(activeTabId)}/send`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text }),
    });

    if (res.status === 409) {
      showToast('!', 'Agent is busy with an in-flight command');
      return false;
    }
    if (!res.ok) {
      showToast('!', 'Failed to send text to terminal');
      return false;
    }
    showToast('✓', 'Staged in Terminal');
    return true;
  } catch (err) {
    showToast('!', 'Network error sending to terminal: ' + err.message);
    return false;
  }
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
      if ($('#pane-right-term')?.classList.contains('active')) {
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

function toggleAgentMenu(e) {
  if (!agentMenuEl) return;
  e.stopPropagation();
  if (!agentMenuEl.hidden) {
    agentMenuEl.hidden = true;
    return;
  }

  agentMenuEl.innerHTML = '';
  for (const agent of SUPPORTED_AGENTS) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.innerHTML = `<span class="agent-icon${agent.id === 'claude' ? ' claude' : ''}">${agent.icon}</span><span>${agent.label}</span>`;
    btn.onclick = (ev) => {
      ev.stopPropagation();
      agentMenuEl.hidden = true;
      addAgentTab(agent.id);
    };
    agentMenuEl.appendChild(btn);
  }
  agentMenuEl.hidden = false;
}

export function initTermInspector() {
  termEl = $('#editor-term-container');
  tabListEl = $('#term-inspector-tabs');
  agentMenuEl = $('#term-agent-menu');
  const addShellBtn = $('#btn-term-add-shell');
  const addAgentBtn = $('#btn-term-add-agent');

  if (addShellBtn) {
    addShellBtn.onclick = () => addShellTab();
  }
  if (addAgentBtn) {
    addAgentBtn.onclick = (e) => toggleAgentMenu(e);
  }
  document.addEventListener('click', (e) => {
    if (agentMenuEl && !agentMenuEl.hidden && !agentMenuEl.contains(e.target) && e.target !== addAgentBtn) {
      agentMenuEl.hidden = true;
    }
  });
}

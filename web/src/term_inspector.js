// web/src/term_inspector.js
import { $, S } from './state.js';
import { on } from './bus.js';
import { showToast } from './ui.js';

let termEl = null;
let tabListEl = null;
let booting = null;
let xtermInstance = null;
let activeSocket = null;
let activeTabId = null;
let workspaceTabs = [];
let pollTimer = null;
let isEnded = false;
const enc = new TextEncoder();

const parentURL = path => new URL(path.startsWith('/') ? path : '/' + path, window.location.origin).href;
const wsURL = path => {
  const u = new URL(path.startsWith('/') ? path : '/' + path, window.location.origin);
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

      term.onData(s => (isEnded ? connect() : send(enc.encode(s))));
      term.onBinary(s => send(Uint8Array.from(s, c => c.charCodeAt(0))));
      term.onResize(sendSize);

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

    if (!workspaceTabs.length) {
      // Lazily spawn default tab if empty
      const addRes = await fetch(parentURL(`api/workspaces/${encodeURIComponent(wsId)}/tabs`), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ kind: 'claude' }),
      });
      if (addRes.ok) {
        const added = await addRes.json();
        workspaceTabs = [added];
      }
    }

    if (!activeTabId || !workspaceTabs.some(t => t.id === activeTabId)) {
      if (workspaceTabs.length) {
        attachTab(workspaceTabs[0].id);
      }
    } else {
      renderTabChips();
    }
  } catch {}
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
    name.textContent = t.name || (t.kind === 'claude' ? 'Claude Code' : 'Shell');
    btn.appendChild(name);

    btn.onclick = () => attachTab(t.id);
    tabListEl.appendChild(btn);
  }
}

export async function addShellTab() {
  const wsId = getWorkspaceID();
  if (!wsId) return;

  try {
    const res = await fetch(parentURL(`api/workspaces/${encodeURIComponent(wsId)}/tabs`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind: 'shell' }),
    });
    if (res.ok) {
      const added = await res.json();
      await refreshWorkspaceTabs();
      attachTab(added.id);
    }
  } catch (err) {
    showToast('!', 'Failed to create shell tab: ' + err.message);
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

export function initTermInspector() {
  termEl = $('#editor-term-container');
  tabListEl = $('#term-inspector-tabs');
  const addShellBtn = $('#btn-term-add-shell');

  if (addShellBtn) {
    addShellBtn.onclick = () => addShellTab();
  }
}

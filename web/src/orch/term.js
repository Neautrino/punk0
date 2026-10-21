// web/src/orch/term.js
// Terminal pane: xterm.js <-> api/tabs/{id}/term websocket <-> that tab's
// PTY in the parent. The PTY outlives the socket; attaching replays its
// scrollback, and attaching to a stopped tab starts it again. Binary frames
// carry PTY bytes both ways; text frames carry {"cols","rows"}.
//
// xterm.js is loaded from static/vendor on the first attach rather than
// bundled, so the page itself stays small.
import { on } from '../bus.js';
import { pageURL, wsURL } from './api.js';

// The server closes a viewer that lags behind the PTY with 1013 (try again
// later); reattaching replays scrollback, so nothing is lost.
const FELL_BEHIND = 1013;

let termEl;
let onTermChange = () => {};
let booting = null; // Promise of the xterm instance
let xterm = null;
let sock = null;
let termTab = null; // tab id the pane shows
let ended = false; // the socket closed: the next key reattaches, starting the tab again
const enc = new TextEncoder();

// initTerm binds the pane. onChange runs when a tab connects or ends, so tab
// running state can be refreshed.
export function initTerm(el, onChange) {
  termEl = el;
  onTermChange = onChange;
}

// termColors maps the page's theme tokens onto xterm's theme; ANSI colours
// stay xterm's own.
function termColors() {
  const cs = getComputedStyle(document.documentElement);
  const v = name => cs.getPropertyValue(name).trim();
  return {
    background: v('--bg'),
    foreground: v('--fg'),
    cursor: v('--accent'),
    cursorAccent: v('--bg'),
    selectionBackground: v('--sel'),
  };
}

function boot() {
  booting ||= Promise.all([
    import(pageURL('static/vendor/xterm.mjs')),
    import(pageURL('static/vendor/addon-fit.mjs')),
  ]).then(([{ Terminal }, { FitAddon }]) => {
    const cs = getComputedStyle(document.documentElement);
    const term = new Terminal({
      cursorBlink: true,
      scrollback: 10000,
      fontSize: 13,
      fontFamily: cs.getPropertyValue('--mono').trim() || 'monospace',
      theme: termColors(),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(termEl);
    // A hidden pane has no size; fitting it would shrink the PTY to 2 columns.
    const refit = () => termEl.clientWidth && termEl.clientHeight && fit.fit();
    refit();
    new ResizeObserver(refit).observe(termEl);
    on('theme:changed', () => { term.options.theme = termColors(); });

    const send = data => sock?.readyState === WebSocket.OPEN && sock.send(data);
    const sendSize = () => send(JSON.stringify({ cols: term.cols, rows: term.rows }));
    term.onData(s => (ended ? connect() : send(enc.encode(s))));
    term.onBinary(s => send(Uint8Array.from(s, c => c.charCodeAt(0))));
    term.onResize(sendSize);
    // xterm.js has no kitty keyboard protocol: send Shift+Enter as ESC CR
    // (Alt+Enter), which Claude Code reads as a newline.
    term.attachCustomKeyEventHandler(e => {
      if (e.key !== 'Enter' || !e.shiftKey || e.ctrlKey || e.altKey || e.metaKey) return true;
      e.preventDefault();
      if (e.type === 'keydown') send(enc.encode('\x1b\r'));
      return false;
    });
    xterm = { term, sendSize };
    return xterm;
  });
  return booting;
}

// connect (re)opens the socket of termTab, replacing any open one.
function connect() {
  if (sock) {
    sock.onclose = null; // a deliberate switch is not a disconnect
    sock.close();
    sock = null;
  }
  ended = false;
  const { term, sendSize } = xterm;
  term.reset(); // the server replays this tab's scrollback
  if (!termTab) return;
  const id = termTab;
  const ws = new WebSocket(wsURL(`api/tabs/${encodeURIComponent(id)}/term`));
  sock = ws;
  ws.binaryType = 'arraybuffer';
  ws.onopen = () => { sendSize(); term.focus(); onTermChange(); };
  ws.onmessage = e => {
    if (typeof e.data !== 'string') term.write(new Uint8Array(e.data));
  };
  ws.onclose = e => {
    if (sock !== ws) return;
    sock = null;
    if (e.code === FELL_BEHIND) return connect();
    term.write(`\r\n\x1b[2m[${e.reason || 'disconnected'} · press any key to restart]\x1b[0m\r\n`);
    ended = true;
    onTermChange();
  };
}

// attachTerm shows tab id in the pane; null detaches and leaves it empty.
export function attachTerm(id) {
  termTab = id;
  boot().then(() => {
    if (termTab === id) connect();
  }, err => {
    termEl.textContent = `Could not load the terminal: ${err.message}`;
  });
}

export function focusTerm() {
  xterm?.term.focus();
}

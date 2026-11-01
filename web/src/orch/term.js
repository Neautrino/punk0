// web/src/orch/term.js
// Terminal pane: xterm.js <-> api/tabs/{id}/term websocket <-> that tab's
// PTY in the parent. The PTY outlives the socket; attaching draws the
// parent's copy of its screen and scrollback, and attaching to a stopped tab
// starts it again. Binary frames carry PTY bytes both ways; text frames carry
// {"cols","rows"}, and {"use":true} when the user types, so punk0 knows which
// tab was used last.
//
// xterm.js is loaded from static/vendor on the first attach rather than
// bundled, so the page itself stays small.
import { on } from '../bus.js';
import { pageURL, wsURL } from './api.js';
import { installClipboard, clipboardKey } from '../termclip.js';
import { termOptions } from '../termoptions.js';

// The server closes a viewer that lags behind the PTY with 1013 (try again
// later); reattaching draws the screen again, so nothing is lost.
const FELL_BEHIND = 1013;

let termEl;
let onTermChange = () => {};
let booting = null; // Promise of the xterm instance
let xterm = null;
let sock = null;
let termTab = null; // tab id the pane shows
let ended = false; // the socket closed: the next key reattaches, resuming the tab
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
    const term = new Terminal({ ...termOptions(), theme: termColors() });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(termEl);
    // A hidden pane has no size; fitting it would shrink the PTY to 2 columns.
    const fitNow = () => termEl.clientWidth && termEl.clientHeight && fit.fit();
    // While a pane border is dragged (resize.js), refit once the pointer
    // rests: every column change resizes the PTY and makes a TUI such as
    // Claude Code redraw, which would stutter at 60 a second.
    let settle = 0;
    const refit = () => {
      clearTimeout(settle);
      if (document.body.classList.contains('wb-resizing')) settle = setTimeout(fitNow, 120);
      else fitNow();
    };
    fitNow();
    new ResizeObserver(refit).observe(termEl);
    on('theme:changed', () => { term.options.theme = termColors(); });
    on('setting:changed', key => {
      if (!key.startsWith('terminal.')) return;
      Object.assign(term.options, termOptions());
      fitNow();
    });
    installClipboard(term, termEl);

    const send = data => sock?.readyState === WebSocket.OPEN && sock.send(data);
    const sendSize = () => send(JSON.stringify({ cols: term.cols, rows: term.rows }));
    // A key pressed here makes this the tab last used (Records.UseTab).
    // onData cannot tell: it also carries xterm's answers to the agent's
    // terminal queries. Once a second is plenty.
    let usedAt = 0;
    const markUse = () => {
      if (sock?.readyState !== WebSocket.OPEN || Date.now() - usedAt < 1000) return;
      usedAt = Date.now();
      send(JSON.stringify({ use: true }));
    };
    term.onData(s => {
      if (ended) return connect(true); // this key only resumes the tab: the agent is not up to read it
      if (Date.now() - claimed > 1000) reclaim(); // typing here makes this the latest viewer
      send(enc.encode(s));
    });
    term.onBinary(s => send(Uint8Array.from(s, c => c.charCodeAt(0))));
    term.onResize(sendSize);
    // Other pages (the editor's Terminal pane) may show this tab too, and its
    // PTY has one size: the viewer that reported last (term.go). Report again
    // whenever this one is used, so the agent draws at this pane's width.
    let claimed = 0;
    const reclaim = () => {
      if (!termEl.clientWidth || !termEl.clientHeight) return;
      fit.fit();
      sendSize();
      claimed = Date.now();
    };
    termEl.addEventListener('pointerdown', reclaim);
    term.textarea?.addEventListener('focus', reclaim);
    addEventListener('focus', reclaim);
    document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && reclaim());
    // xterm.js has no kitty keyboard protocol: send Shift+Enter as ESC CR
    // (Alt+Enter), which Claude Code reads as a newline.
    term.attachCustomKeyEventHandler(e => {
      if (e.type === 'keydown') markUse();
      if (clipboardKey(term, e)) return false;
      if (e.key !== 'Enter' || !e.shiftKey || e.ctrlKey || e.altKey || e.metaKey) return true;
      e.preventDefault();
      if (e.type === 'keydown') send(enc.encode('\x1b\r'));
      return false;
    });
    xterm = { term, sendSize, fitNow };
    return xterm;
  });
  return booting;
}

// connect (re)opens the socket of termTab, replacing any open one. pick says
// the user chose the tab, which makes it the one punk0 reopens (term.go).
function connect(pick = false) {
  if (sock) {
    sock.onclose = null; // a deliberate switch is not a disconnect
    sock.close();
    sock = null;
  }
  ended = false;
  const { term, sendSize, fitNow } = xterm;
  term.reset(); // the server sends this tab's screen
  if (!termTab) return;
  const id = termTab;
  const ws = new WebSocket(wsURL(`api/tabs/${encodeURIComponent(id)}/term${pick ? '?pick=1' : ''}`));
  sock = ws;
  ws.binaryType = 'arraybuffer';
  ws.onopen = () => { term.focus(); onTermChange(); };
  // The server first sends the size its copy of the screen is drawn at, then
  // that copy (term.go Attach): draw it at that size, then fit this pane,
  // which reflows it, and report the pane's size.
  let drawing = false;
  ws.onmessage = e => {
    if (typeof e.data === 'string') {
      const { cols, rows } = JSON.parse(e.data);
      term.resize(cols, rows);
      drawing = true;
    } else if (drawing) {
      drawing = false;
      term.write(new Uint8Array(e.data), () => { fitNow(); sendSize(); });
    } else {
      term.write(new Uint8Array(e.data));
    }
  };
  ws.onclose = e => {
    if (sock !== ws) return;
    sock = null;
    if (e.code === FELL_BEHIND) return connect();
    // A loopback socket closed with no reason is punk0 itself going away.
    term.write(`\r\n\x1b[2m[${e.reason || 'punk0 stopped'} · press any key to resume]\x1b[0m\r\n`);
    ended = true;
    onTermChange();
  };
}

// attachTerm shows tab id in the pane; null detaches and leaves it empty.
// pick: the user chose it (connect).
export function attachTerm(id, pick = false) {
  termTab = id;
  boot().then(() => {
    if (termTab === id) connect(pick);
  }, err => {
    termEl.textContent = `Could not load the terminal: ${err.message}`;
  });
}

export function focusTerm() {
  xterm?.term.focus();
}

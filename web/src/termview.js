// web/src/termview.js
// One terminal pane: xterm.js <-> api/tabs/{id}/term websocket <-> that
// tab's PTY in the agents page's server. The PTY outlives the socket;
// attaching draws the server's copy of its screen and scrollback, and
// attaching to a stopped tab starts it again. Binary frames carry PTY bytes
// both ways; text frames carry {"cols","rows"}, and {"use":true} when the
// user types (opts.reportUse), so punk0 knows which tab was used last.
//
// Both pages build their panes from this: the agents page one per tab group,
// the editor one for its Terminal side and one for its center. xterm.js is
// loaded from static/vendor on the first attach rather than bundled, so the
// pages themselves stay small; every pane of a page shares that one load.
import { on } from './bus.js';
import { installClipboard, clipboardKey } from './termclip.js';
import { termOptions, loadTermFonts, remeasure } from './termoptions.js';
import { registerFileLinks } from './filelinks.js';

// The server closes a viewer that lags behind the PTY with 1013 (try again
// later); reattaching draws the screen again, so nothing is lost.
const FELL_BEHIND = 1013;
const enc = new TextEncoder();

let modules = null;
const loadModules = vendorURL => (modules ||= Promise.all([
  import(vendorURL('static/vendor/xterm.mjs')),
  import(vendorURL('static/vendor/addon-fit.mjs')),
  import(vendorURL('static/vendor/addon-image.mjs')),
  import(vendorURL('static/vendor/addon-web-links.mjs')),
]));

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

// createTermView binds a terminal pane to el. opts:
//   vendorURL(path)  the page's URL of a static file
//   socketURL(path)  the ws(s):// URL of an api path of the agents page
//   fileLinks()      filelinks.js's context for this pane, or null
//   onChange()       a tab connected or its socket ended
//   reportUse        tell the server when the user types (agents page)
//   pickOnResume     a key that reattaches an ended tab counts as a pick
export function createTermView(el, opts) {
  const { vendorURL, socketURL, fileLinks = () => null, onChange = () => {}, reportUse = false, pickOnResume = false } = opts;
  let booting = null; // Promise of the xterm instance
  let x = null; // { term, fit, sendSize, fitNow }
  let sock = null;
  let tabId = null; // tab id the pane shows
  let ended = false; // the socket closed: the next key reattaches, resuming the tab
  let notice = ''; // text shown while no tab is attached

  function boot() {
    if (booting) return booting;
    let late = () => {}; // set once the terminal exists
    booting = Promise.all([loadModules(vendorURL), loadTermFonts(termOptions(), () => late())])
      .then(([[{ Terminal }, { FitAddon }, { ImageAddon }, { WebLinksAddon }]]) => {
        const term = new Terminal({ ...termOptions(), theme: termColors() });
        const fit = new FitAddon();
        term.loadAddon(fit);
        // SIXEL and iTerm2 inline images: agents find SIXEL by asking
        // (XTSMGRAPHICS) and size images by the cell size reports it turns on.
        term.loadAddon(new ImageAddon());
        // http(s) URLs in the output open in a new browser tab on click,
        // without an opener; a drag over one still selects. File paths open
        // in punk0 (filelinks.js).
        term.loadAddon(new WebLinksAddon());
        registerFileLinks(term, fileLinks);
        term.open(el);
        // A hidden pane has no size; fitting it would shrink the PTY to 2 columns.
        const fitNow = () => el.clientWidth && el.clientHeight && fit.fit();
        // While a pane border is dragged (resize.js, split.js), refit once the
        // pointer rests: every column change resizes the PTY and makes a TUI
        // such as Claude Code redraw, which would stutter at 60 a second.
        let settle = 0;
        const refit = () => {
          clearTimeout(settle);
          if (document.body.classList.contains('wb-resizing')) settle = setTimeout(fitNow, 120);
          else fitNow();
        };
        fitNow();
        late = () => { remeasure(term); fitNow(); };
        new ResizeObserver(refit).observe(el);
        on('theme:changed', () => { term.options.theme = termColors(); });
        on('setting:changed', key => {
          if (!key.startsWith('terminal.')) return;
          const next = termOptions();
          loadTermFonts(next, () => late()).then(() => {
            Object.assign(term.options, next);
            fitNow();
          });
        });
        installClipboard(term, el);

        const send = data => sock?.readyState === WebSocket.OPEN && sock.send(data);
        const sendSize = () => send(JSON.stringify({ cols: term.cols, rows: term.rows }));
        // A key pressed here makes this the tab last used (Records.UseTab).
        // onData cannot tell: it also carries xterm's answers to the agent's
        // terminal queries. Once a second is plenty.
        let usedAt = 0;
        const markUse = () => {
          if (!reportUse || sock?.readyState !== WebSocket.OPEN || Date.now() - usedAt < 1000) return;
          usedAt = Date.now();
          send(JSON.stringify({ use: true }));
        };
        term.onData(s => {
          if (ended) return connect(pickOnResume); // this key only resumes the tab: the agent is not up to read it
          if (Date.now() - claimed > 1000) reclaim(); // typing here makes this the latest viewer
          send(enc.encode(s));
        });
        term.onBinary(s => send(Uint8Array.from(s, c => c.charCodeAt(0))));
        term.onResize(sendSize);
        // Another pane (the other page, or another group) may show this tab
        // too, and its PTY has one size: the viewer that reported last
        // (term.go). Report again whenever this one is used, so the agent
        // draws at this pane's width.
        let claimed = 0;
        const reclaim = () => {
          if (!el.clientWidth || !el.clientHeight) return;
          fit.fit();
          sendSize();
          claimed = Date.now();
        };
        el.addEventListener('pointerdown', reclaim);
        term.textarea?.addEventListener('focus', reclaim);
        addEventListener('focus', () => term.textarea === document.activeElement && reclaim());
        document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && el.contains(document.activeElement) && reclaim());
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
        x = { term, fit, sendSize, fitNow };
        return x;
      });
    return booting;
  }

  // connect (re)opens the socket of tabId, replacing any open one. pick says
  // the user chose the tab, which makes it the one punk0 reopens (term.go);
  // focus puts the caret in it once it is up.
  function connect(pick = false, focus = true) {
    if (sock) {
      sock.onclose = null; // a deliberate switch is not a disconnect
      sock.close();
      sock = null;
    }
    ended = false;
    const { term, sendSize, fitNow } = x;
    term.reset(); // the server sends this tab's screen
    if (!tabId) {
      if (notice) term.write(`\r\n\x1b[2m${notice}\x1b[0m\r\n`);
      return;
    }
    const ws = new WebSocket(socketURL(`api/tabs/${encodeURIComponent(tabId)}/term${pick ? '?pick=1' : ''}`));
    sock = ws;
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => {
      if (focus) term.focus();
      onChange();
    };
    // The server first sends the size its copy of the screen is drawn at,
    // then that copy (term.go Attach): draw it at that size, then fit this
    // pane, which reflows it, and report the pane's size.
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
      if (e.code === FELL_BEHIND) return connect(false, false);
      // A loopback socket closed with no reason is punk0 itself going away.
      term.write(`\r\n\x1b[2m[${e.reason || 'punk0 stopped'} · press any key to resume]\x1b[0m\r\n`);
      ended = true;
      onChange();
    };
  }

  return {
    // attach shows tab id in the pane; null detaches and leaves it empty (or
    // showing the notice). pick: the user chose it; focus: put the caret in
    // it once connected.
    attach(id, { pick = false, focus = true } = {}) {
      tabId = id;
      if (id) notice = '';
      return boot().then(() => {
        if (tabId === id) connect(pick, focus);
      }, err => {
        el.textContent = `Could not load the terminal: ${err.message}`;
      });
    },
    // showNotice writes a dim line while no tab is attached (the editor's
    // "Tabs are stopped…").
    showNotice(text) {
      if (notice === text && !tabId) return;
      notice = text;
      boot().then(() => {
        if (!tabId && notice === text) connect();
      }, () => {});
    },
    focus() { x?.term.focus(); },
    fit() { x?.fitNow(); },
    id: () => tabId,
    open: () => sock?.readyState === WebSocket.OPEN,
    ready: () => boot(),
    paste(text) {
      x?.term.paste(text);
      x?.term.focus();
    },
  };
}

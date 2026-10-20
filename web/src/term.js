// web/src/term.js
// Terminal pane: xterm.js <-> /api/tabs/{id}/term websocket <-> that tab's
// shell. The shell outlives the socket; attaching replays its scrollback.
// Binary frames carry PTY bytes both ways; text frames carry {"cols","rows"}.
import { Terminal } from '../vendor/xterm.mjs';
import { FitAddon } from '../vendor/addon-fit.mjs';

// The server closes a viewer that lags behind the shell with 1013 (try
// again later); reattaching replays scrollback, so nothing is lost.
const FELL_BEHIND = 1013;

// onChange runs when a shell connects or ends, so tab running state can be
// refreshed.
export function initTerm(el, onChange) {
  const term = new Terminal({ cursorBlink: true, scrollback: 10000, fontSize: 13, theme: { background: '#0b0d12' } });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(el);
  fit.fit();

  const enc = new TextEncoder();
  let ws = null;
  let tab = null;
  let ended = false; // the socket closed: the next key reattaches, starting a new shell
  const send = (data) => ws?.readyState === WebSocket.OPEN && ws.send(data);
  const sendSize = () => send(JSON.stringify({ cols: term.cols, rows: term.rows }));

  // attach switches to tab id; null detaches and leaves the pane empty.
  function attach(id) {
    if (ws) {
      ws.onclose = null; // a deliberate switch is not a disconnect
      ws.close();
      ws = null;
    }
    tab = id;
    ended = false;
    term.reset(); // the server replays this tab's scrollback
    if (!id) return;
    ws = new WebSocket(`ws://${location.host}/api/tabs/${encodeURIComponent(id)}/term`);
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => { sendSize(); term.focus(); onChange(); };
    ws.onmessage = (e) => term.write(new Uint8Array(e.data));
    ws.onclose = (e) => {
      if (e.code === FELL_BEHIND) return attach(tab);
      term.write(`\r\n\x1b[2m[${e.reason || 'disconnected'} · press any key to restart]\x1b[0m\r\n`);
      ended = true;
      onChange();
    };
  }

  term.onData((s) => (ended ? attach(tab) : send(enc.encode(s))));
  term.onBinary((s) => send(Uint8Array.from(s, (c) => c.charCodeAt(0))));
  term.onResize(sendSize);
  new ResizeObserver(() => fit.fit()).observe(el);

  // xterm.js has no kitty keyboard protocol: send Shift+Enter as ESC CR
  // (Alt+Enter), which Claude Code reads as a newline.
  term.attachCustomKeyEventHandler((e) => {
    if (e.key !== 'Enter' || !e.shiftKey || e.ctrlKey || e.altKey || e.metaKey) return true;
    e.preventDefault();
    if (e.type === 'keydown') send(enc.encode('\x1b\r'));
    return false;
  });

  return { attach };
}

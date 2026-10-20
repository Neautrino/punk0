// web/src/term.js
// Terminal pane: xterm.js <-> /api/satellites/{id}/term websocket <-> that
// satellite's shell. The shell outlives the socket; attaching replays its
// scrollback. Binary frames carry PTY bytes both ways; text frames carry
// {"cols","rows"} or {"run":"claude"}, which the server types into the shell
// when it is idle.
import { Terminal } from '../vendor/xterm.mjs';
import { FitAddon } from '../vendor/addon-fit.mjs';

// The server closes a viewer that lags behind the shell with 1013 (try
// again later); reattaching replays scrollback, so nothing is lost.
const FELL_BEHIND = 1013;

export function initTerm(el) {
  const term = new Terminal({ cursorBlink: true, scrollback: 10000, fontSize: 13, theme: { background: '#0b0d12' } });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(el);
  fit.fit();

  const enc = new TextEncoder();
  let ws = null;
  let sat = 'default';
  const send = (data) => ws?.readyState === WebSocket.OPEN && ws.send(data);
  const sendSize = () => send(JSON.stringify({ cols: term.cols, rows: term.rows }));

  function attach(id) {
    if (ws) {
      ws.onclose = null; // a deliberate switch is not a disconnect
      ws.close();
    }
    sat = id;
    term.reset(); // the server replays this satellite's scrollback
    ws = new WebSocket(`ws://${location.host}/api/satellites/${encodeURIComponent(id)}/term`);
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => { sendSize(); term.focus(); };
    ws.onmessage = (e) => term.write(new Uint8Array(e.data));
    ws.onclose = (e) => {
      if (e.code === FELL_BEHIND) return attach(sat);
      term.write(`\r\n\x1b[2m[${e.reason || 'disconnected'}]\x1b[0m\r\n`);
    };
  }

  term.onData((s) => send(enc.encode(s)));
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

  attach(sat);

  return {
    attach,
    run(name) {
      send(JSON.stringify({ run: name }));
      term.focus();
    },
  };
}

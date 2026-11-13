// web/src/filelinks.js
// File paths in terminal output open in punk0 on a click: a file tab on the
// agents page, an editor tab in the editor, at the line the path names
// (main.go:42, main.go:42:7, main.go(42,7), main.go#L42). A path links only
// when the workspace has that file, so words with a slash stay text. Paths
// are taken relative to the workspace folder, not the shell's current one;
// absolute paths and file:// URLs link when they lie inside it. OSC 8 links
// to such files open the same way.

// A file:// URL, or a path: optional root (C:\ / ~/ ./ ../ /), then segments.
// A path may not start inside a word or another URL (the lookbehind), and
// may end with a line, line:column, (line,column) or #Lline.
const PATH_RE = /(file:\/\/[^\s"'`<>()[\]{}]+)|(?<![\w.:/\\@+~-])((?:[A-Za-z]:[\\/]|~[\\/]|\.{1,2}[\\/]|[\\/])?[\w.@+-]+(?:[\\/][\w.@+-]+)*)(?::(\d+)(?::(\d+))?|\((\d+)(?:,\s*(\d+))?\)|#L(\d+))?/g;

// findPaths returns the path-like spans of text: {start, end} string offsets
// of the whole match (what the link underlines), the path as written, and
// its line and column (0 when not given). A bare word needs an extension to
// count (README.md, not "and/or" alone: that has a slash, so it is checked).
export function findPaths(text) {
  const out = [];
  for (const m of text.matchAll(PATH_RE)) {
    let path, line = 0, col = 0, end = m.index + m[0].length;
    if (m[1]) {
      const url = m[1].replace(/[.,;:!?]+$/, ''); // a sentence's punctuation is not part of it
      ({ path, line, col } = fromFileURL(url));
      if (!path) continue;
      end = m.index + url.length;
    } else {
      path = m[2];
      const lineAt = m[3] || m[5] || m[7];
      if (lineAt) {
        line = Number(lineAt);
        col = Number(m[4] || m[6] || 0);
      } else {
        // A sentence's full stop is not part of the file name.
        const trimmed = path.replace(/\.+$/, '');
        end -= path.length - trimmed.length;
        path = trimmed;
      }
      if (!/[\\/]/.test(path) && !/\.\w+$/.test(path)) continue;
    }
    out.push({ start: m.index, end, path, line, col });
  }
  return out;
}

// fromFileURL reads file:///abs/path[:line[:col]] and file:///C:/x, with a
// localhost host allowed.
function fromFileURL(url) {
  let rest = url.slice('file://'.length).replace(/^localhost(?=\/)/i, '');
  try { rest = decodeURIComponent(rest); } catch { return {}; }
  if (/^\/[A-Za-z]:\//.test(rest)) rest = rest.slice(1);
  const m = /^(.*?)(?::(\d+)(?::(\d+))?)?$/.exec(rest);
  return { path: m[1], line: Number(m[2] || 0), col: Number(m[3] || 0) };
}

// resolvePath turns a path as written into one relative to root (the
// workspace folder), or null when it is outside it or cannot be a file there.
// Windows folders compare case-insensitively, either slash.
export function resolvePath(path, root) {
  let p = path.replace(/\\/g, '/');
  const r = root.replace(/\\/g, '/').replace(/\/+$/, '');
  if (/^([A-Za-z]:)?\//.test(p)) {
    const win = /^[A-Za-z]:/.test(r);
    const has = win ? p.toLowerCase().startsWith(r.toLowerCase() + '/') : p.startsWith(r + '/');
    if (!has) return null;
    p = p.slice(r.length + 1);
  } else if (p.startsWith('~/')) {
    return null; // the browser does not know the home folder
  }
  p = p.replace(/^(\.\/)+/, '');
  if (!p || p.split('/').some(s => s === '' || s === '.' || s === '..')) return null;
  return p;
}

// logicalLine reads the buffer line holding row y (1-based), joined with the
// rows the terminal wrapped it onto, as a string with each UTF-16 unit's
// first cell (x, y, 1-based) and width, so string offsets map back onto
// cells across rows and past wide characters.
function logicalLine(buf, y, cols) {
  let top = y - 1;
  while (top > 0 && buf.getLine(top)?.isWrapped) top--;
  let text = '';
  const xs = [], ys = [], ws = [];
  for (let row = top, line = buf.getLine(row); line && (row === top || line.isWrapped); line = buf.getLine(++row)) {
    for (let x = 0; x < cols; x++) {
      const cell = line.getCell(x);
      const w = cell?.getWidth() ?? 1;
      if (w === 0) continue; // the second half of a wide character
      const ch = cell?.getChars() || ' ';
      for (let i = 0; i < ch.length; i++) { xs.push(x + 1); ys.push(row + 1); ws.push(w); }
      text += ch;
    }
  }
  return { text, xs, ys, ws };
}

// Directory listings are kept a few seconds: a hover asks for every line
// under the pointer, and agents create files as they go.
const LIST_TTL = 5000;

// registerFileLinks makes term's file paths clickable. ctx() is read on each
// hover and returns null (no links) or {root, list(dir), open(path, line,
// col)}: list resolves to the folder's entries ({name, dir}) or null.
export function registerFileLinks(term, ctx) {
  const lists = new Map(); // root + '\0' + dir -> {at, names: Promise<Set|null>}
  const hasFile = (c, rel) => {
    const i = rel.lastIndexOf('/');
    const dir = i < 0 ? '' : rel.slice(0, i);
    const key = c.root + '\0' + dir;
    let e = lists.get(key);
    if (!e || Date.now() - e.at > LIST_TTL) {
      if (lists.size > 200) lists.clear(); // path-like words name many folders that are not there
      e = {
        at: Date.now(),
        names: Promise.resolve(c.list(dir))
          .then(kids => kids && new Set(kids.filter(k => !k.dir).map(k => k.name)), () => null),
      };
      lists.set(key, e);
    }
    return e.names.then(names => !!names?.has(rel.slice(i + 1)));
  };

  term.registerLinkProvider({
    provideLinks(y, done) {
      const c = ctx();
      if (!c || !term.buffer.active.getLine(y - 1)) return done(undefined);
      const { text, xs, ys, ws } = logicalLine(term.buffer.active, y, term.cols);
      // Only the links that reach row y: the other rows ask for their own.
      const found = findPaths(text)
        .filter(f => ys[f.start] <= y && ys[f.end - 1] >= y)
        .map(f => ({ ...f, rel: resolvePath(f.path, c.root) }))
        .filter(f => f.rel);
      if (!found.length) return done(undefined);
      Promise.all(found.map(f => hasFile(c, f.rel).then(ok => ok && f))).then(hits => {
        const links = hits.filter(Boolean).map(f => ({
          range: {
            start: { x: xs[f.start], y: ys[f.start] },
            end: { x: xs[f.end - 1] + ws[f.end - 1] - 1, y: ys[f.end - 1] },
          },
          text: text.slice(f.start, f.end),
          decorations: { underline: true, pointerCursor: true },
          activate: () => c.open(f.rel, f.line, f.col),
        }));
        done(links.length ? links : undefined);
      });
    },
  });

  // OSC 8 links: file:// ones inside the workspace open in punk0; http(s)
  // ones keep xterm's own confirm-then-open; anything else does nothing.
  term.options.linkHandler = {
    allowNonHttpProtocols: true,
    activate(e, uri) {
      if (/^file:\/\//i.test(uri)) {
        const c = ctx();
        const f = fromFileURL(uri);
        const rel = c && f.path && resolvePath(f.path, c.root);
        if (rel) c.open(rel, f.line, f.col);
        return;
      }
      if (!/^https?:\/\//i.test(uri)) return;
      if (!confirm(`Do you want to navigate to ${uri}?\n\nWARNING: This link could potentially be dangerous`)) return;
      const w = window.open();
      if (!w) return;
      try { w.opener = null; } catch {}
      w.location.href = uri;
    },
  };
}

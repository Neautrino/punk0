// web/src/difftable.js
// Draws diff hunks ({oldStart, newStart, rows: [{type, oldLine, newLine, at,
// text, html}]}, as /api/diff and /api/changes return them) as rows, unified
// or split side by side, with changed words marked inside changed lines.
// Shared by the editor's diff view (diff.js) and the agents page's Diff view
// (orch/changes.js), which style them alike (style.css .diff-*).
//
// opts: reviewable (a row's line number is a review-comment target; see
// anchor), lineActions, the title of the line-actions button each line
// number carries, which also makes the number open its line; none without it;
// and diagnostics, a Map of new-file line to message shown on that line's
// code cell (data-diag).
import { esc } from './state.js';

// Lucide chevrons-up-down / chevrons-down-up: open out vs fold in. diff.js
// uses the same pair on its expand-all toggle.
export const ICON_UNFOLD = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m7 15 5 5 5-5"/><path d="m7 9 5-5 5 5"/></svg>';
export const ICON_FOLD = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m7 20 5-5 5 5"/><path d="m7 4 5 5 5-5"/></svg>';

/* The hunk header's action buttons: per hunk (data-hunk-act, applied as a
   patch), or, on a diff that is one whole file -- new or deleted, which git
   apply can't stage or reverse into the right state -- whole-file ones
   (data-file-act), titled by the file's kind. */
const HUNK_ACTIONS = [
  ['hunkAct', 'stage', 'Stage', 'Stage this hunk (git apply --cached)'],
  ['hunkAct', 'unstage', 'Unstage', 'Unstage this hunk (git apply --cached -R)'],
  ['hunkAct', 'discard', 'Discard', 'Discard this hunk in working tree'],
];
const FILE_ACTION_TITLES = {
  untracked: {
    stage: 'Stage this new file (git add)',
    discard: 'Delete this new file; git has no copy of it',
  },
  added: {
    stage: 'Stage the edits made since this new file was staged (git add)',
    unstage: 'Unstage this new file; it stays on disk, untracked',
    discard: 'Delete this new file and drop it from the index; HEAD has no copy of it',
  },
  deleted: {
    stage: 'Stage this deletion (git rm)',
    unstage: 'Unstage this deletion; the file stays deleted on disk',
    discard: 'Restore this file from HEAD',
  },
};
const LABELS = { stage: 'Stage', unstage: 'Unstage', discard: 'Discard' };

/* A hunk's @@ header, keeping the hunk on the element (el._hunk) for the
   buttons' handlers. opts: index (the hunk's place in its list), expand (an
   Expand / Collapse context toggle; a hunk with extra context carries its
   git rows in hunk.unexpanded) and actions: 'hunk' for the per-hunk set, a
   server wholeFile ({kind, actions}) for whole-file ones, null for none. */
export function hunkHeader(hunk, { index = 0, expand = false, actions = null } = {}) {
  const el = document.createElement('div');
  el.className = 'diff-hunk-head';
  el._hunk = hunk;

  const label = document.createElement('span');
  label.className = 'diff-hunk-label';
  label.textContent = '@@ -' + hunk.oldStart + ' +' + hunk.newStart + ' @@' + (hunk.section ? ' ' + hunk.section : '');
  el.append(label);

  const grow = document.createElement('span');
  grow.className = 'grow';
  el.append(grow);

  if (expand) {
    const expanded = !!hunk.unexpanded;
    const expandBtn = document.createElement('button');
    expandBtn.type = 'button';
    expandBtn.className = 'diff-hunk-btn diff-hunk-expand';
    expandBtn.dataset.hunkExpand = String(index);
    expandBtn.title = expanded ? 'Back to the diff\'s own context lines' : 'Show surrounding context lines';
    expandBtn.setAttribute('aria-pressed', String(expanded));
    expandBtn.innerHTML = (expanded ? ICON_FOLD + 'Collapse' : ICON_UNFOLD + 'Expand');
    el.append(expandBtn);
  }

  const buttons = actions === 'hunk' ? HUNK_ACTIONS
    : actions ? actions.actions.map(act => ['fileAct', act, LABELS[act], FILE_ACTION_TITLES[actions.kind]?.[act] || LABELS[act]])
    : [];
  for (const [attr, act, text, title] of buttons) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'diff-hunk-btn' + (act === 'discard' ? ' diff-hunk-discard' : '');
    btn.dataset[attr] = act;
    btn.title = title;
    btn.textContent = text;
    el.append(btn);
  }
  return el;
}

/* ---------- unified layout: one row per diff line ---------- */

export function unifiedTable(hunk, opts = {}) {
  const table = document.createElement('div');
  table.className = 'diff-table diff-unified';

  // Pair consecutive runs of deletions and additions to compute intra-line diffs
  const rowHtml = new Map();
  let i = 0;
  while (i < hunk.rows.length) {
    if (hunk.rows[i].type === 'ctx') { i++; continue; }
    let dels = [], adds = [];
    while (i < hunk.rows.length && hunk.rows[i].type === 'del') dels.push(hunk.rows[i++]);
    while (i < hunk.rows.length && hunk.rows[i].type === 'add') adds.push(hunk.rows[i++]);
    const n = Math.min(dels.length, adds.length);
    for (let k = 0; k < n; k++) {
      const diff = diffLine(dels[k].text, adds[k].text, dels[k].html, adds[k].html);
      rowHtml.set(dels[k], diff.oldHtml);
      rowHtml.set(adds[k], diff.newHtml);
    }
  }

  for (const row of hunk.rows) {
    const r = document.createElement('div');
    r.className = 'diff-row diff-' + row.type;
    anchor(r, row, opts);
    r.append(
      lineCell(row.type === 'add' ? '' : row.oldLine, opts),
      lineCell(row.type === 'del' ? '' : row.newLine, opts),
      markerCell(row.type),
      codeCell(rowHtml.has(row) ? { ...row, html: rowHtml.get(row) } : row, opts.diagnostics),
    );
    table.append(r);
  }
  return table;
}

/* fileLayout is the layout a file's hunks are drawn in: the one asked for,
   except that a file whose diff is one-sided -- a new file (all additions) or
   a deleted one (all deletions) -- is always unified, since split would leave
   half the width blank. */
export function fileLayout(hunks, mode) {
  if (mode !== 'split' || !hunks.length) return mode;
  const rows = hunks.flatMap(h => h.rows);
  return rows.every(r => r.type === 'add') || rows.every(r => r.type === 'del') ? 'unified' : mode;
}

/* ---------- split layout: deletions and additions paired side by side ---------- */

export function splitTable(hunk, opts = {}) {
  const table = document.createElement('div');
  table.className = 'diff-table diff-split';
  for (const pair of pairRows(hunk.rows)) {
    let leftRow = pair.left;
    let rightRow = pair.right;
    if (leftRow && rightRow && leftRow.type === 'del' && rightRow.type === 'add') {
      const diff = diffLine(leftRow.text, rightRow.text, leftRow.html, rightRow.html);
      leftRow = { ...leftRow, html: diff.oldHtml };
      rightRow = { ...rightRow, html: diff.newHtml };
    }
    const r = document.createElement('div');
    r.className = 'diff-row-pair';
    r.append(splitSide(leftRow, 'left', opts), splitSide(rightRow, 'right', opts));
    table.append(r);
  }
  return table;
}

// Walks a hunk's flat row list, pairing each run of deletions with the run of
// additions that immediately follows it (a "changed" block) index-by-index,
// padding the shorter side with blanks. Context rows go straight across.
function pairRows(rows) {
  const pairs = [];
  let i = 0;
  while (i < rows.length) {
    const row = rows[i];
    if (row.type === 'ctx') { pairs.push({ left: row, right: row }); i++; continue; }
    let dels = [], adds = [];
    while (i < rows.length && rows[i].type === 'del') dels.push(rows[i++]);
    while (i < rows.length && rows[i].type === 'add') adds.push(rows[i++]);
    const n = Math.max(dels.length, adds.length);
    for (let k = 0; k < n; k++) pairs.push({ left: dels[k] || null, right: adds[k] || null });
  }
  return pairs;
}

function splitSide(row, side, opts) {
  const el = document.createElement('div');
  el.className = 'diff-side diff-side-' + side + (row ? ' diff-' + row.type : ' diff-blank');
  if (!row) { el.append(lineCell('', opts), markerCell(''), codeCell(null)); return el; }
  const ln = side === 'left' ? row.oldLine : row.newLine;
  anchor(el, row, opts);
  el.append(lineCell(ln, opts), markerCell(row.type), codeCell(row, side === 'right' ? opts.diagnostics : null));
  return el;
}

/* Stamps where a row points in the working tree, so a selection on it can be
   edited. Context and added lines have a line on disk (data-l), which a context
   line shares across both sides of the split. A deleted line has none, only the
   place it used to be (data-at).
   reviewable marks whether this row's line number is meaningful as a GitHub
   review-comment target -- true for the PR's own diff (numbered against the
   checked-out head commit, which is what a submitted review is posted
   against), false for the reviewer's local "Your changes" section, whose
   lines don't correspond to anything pushed yet. linecomment.js reads this
   to fall back to a plain inline AI edit instead of opening the composer. */
function anchor(el, row, { reviewable = true }) {
  if (row.newLine !== undefined) el.dataset.l = row.newLine;
  else if (row.at !== undefined) el.dataset.at = row.at;
  if (row.oldLine !== undefined) el.dataset.oldL = row.oldLine;
  if (!reviewable) el.dataset.reviewable = '0';
}

function lineCell(n, { lineActions }) {
  const el = document.createElement('div');
  el.className = 'diff-ln';
  if (lineActions && n !== '' && n !== undefined) {
    el.classList.add('diff-ln-nav');
    el.title = 'Open in file view at line ' + n;
    const btn = document.createElement('span');
    btn.className = 'line-btn';
    btn.setAttribute('role', 'button');
    btn.title = lineActions;
    btn.textContent = 'Edit';
    el.append(btn);
  }
  el.append(document.createTextNode(n === '' || n === undefined ? '' : String(n)));
  return el;
}

const MARKS = { add: '+', del: '-', ctx: '' };

function markerCell(type) {
  const el = document.createElement('div');
  el.className = 'diff-mk';
  el.textContent = MARKS[type] || '';
  return el;
}

function codeCell(arg, diagnostics = null) {
  const el = document.createElement('div');
  el.className = 'diff-code';
  if (!arg) {
    el.innerHTML = '&nbsp;';
    return el;
  }
  if (typeof arg === 'string') {
    el.innerHTML = esc(arg) || '&nbsp;';
    return el;
  }
  if (diagnostics && arg.newLine !== undefined && diagnostics.has(arg.newLine)) {
    el.dataset.diag = diagnostics.get(arg.newLine);
  }
  el.innerHTML = arg.html || esc(arg.text || '') || '&nbsp;';
  return el;
}

function tokenize(str) {
  return str.match(/\w+|\s+|[^\w\s]/g) || [];
}

function myersDiff(aTokens, bTokens) {
  const n = aTokens.length, m = bTokens.length;
  if (n === 0 && m === 0) return [];
  if (n === 0) return bTokens.map(t => ({ type: 'add', val: t }));
  if (m === 0) return aTokens.map(t => ({ type: 'del', val: t }));

  const max = n + m;
  if (max > 400) return null; // Avoid deep search on massive minified lines

  const v = new Int32Array(2 * max + 1);
  const trace = [];

  for (let d = 0; d <= max; d++) {
    trace.push(v.slice(max - d, max + d + 1));
    for (let k = -d; k <= d; k += 2) {
      let x;
      if (k === -d || (k !== d && v[k - 1 + max] < v[k + 1 + max])) {
        x = v[k + 1 + max];
      } else {
        x = v[k - 1 + max] + 1;
      }
      let y = x - k;
      while (x < n && y < m && aTokens[x] === bTokens[y]) {
        x++;
        y++;
      }
      v[k + max] = x;
      if (x >= n && y >= m) {
        const script = [];
        let curX = n, curY = m;
        for (let step = d; step > 0; step--) {
          const prevV = trace[step];
          const curK = curX - curY;
          let prevK = (curK === -step || (curK !== step && prevV[curK - 1 + step] < prevV[curK + 1 + step]))
            ? curK + 1
            : curK - 1;
          const prevX = prevV[prevK + step];
          const prevY = prevX - prevK;

          while (curX > prevX && curY > prevY && aTokens[curX - 1] === bTokens[curY - 1]) {
            script.push({ type: 'eq', val: aTokens[curX - 1] });
            curX--; curY--;
          }
          if (curX > prevX) {
            script.push({ type: 'del', val: aTokens[curX - 1] });
            curX--;
          } else if (curY > prevY) {
            script.push({ type: 'add', val: bTokens[curY - 1] });
            curY--;
          }
        }
        while (curX > 0 && curY > 0) {
          script.push({ type: 'eq', val: aTokens[curX - 1] });
          curX--; curY--;
        }
        return script.reverse();
      }
    }
  }
  return null;
}

// Overlays intra-line character change spans onto Chroma syntax-highlighted HTML
// (`<i class=...>`) without breaking tag nesting.
function overlayCharMask(syntaxHtml, expectedLen, mask, spanClass, bars, barClass) {
  if (!syntaxHtml) return null;
  let out = '';
  let charIdx = 0;
  let inSpan = false;
  let i = 0;
  const n = syntaxHtml.length;

  while (i < n) {
    if (syntaxHtml[i] === '<') {
      const close = syntaxHtml.indexOf('>', i);
      if (close < 0) return null;
      if (inSpan) {
        out += '</span>';
        inSpan = false;
      }
      out += syntaxHtml.slice(i, close + 1);
      i = close + 1;
      continue;
    }

    if (bars && bars.has(charIdx)) {
      if (inSpan) { out += '</span>'; inSpan = false; }
      out += '<span class="' + barClass + '"></span>';
    }

    const wantSpan = charIdx < expectedLen && mask[charIdx] === 1;
    if (wantSpan && !inSpan) {
      out += '<span class="' + spanClass + '">';
      inSpan = true;
    } else if (!wantSpan && inSpan) {
      out += '</span>';
      inSpan = false;
    }

    if (syntaxHtml[i] === '&') {
      const semi = syntaxHtml.indexOf(';', i);
      if (semi > i && semi - i <= 10) {
        out += syntaxHtml.slice(i, semi + 1);
        i = semi + 1;
        charIdx++;
        continue;
      }
    }
    out += syntaxHtml[i++];
    charIdx++;
  }

  if (inSpan) out += '</span>';
  if (bars && bars.has(charIdx)) {
    out += '<span class="' + barClass + '"></span>';
  }
  if (charIdx !== expectedLen) return null;
  return out;
}

// Computes an intra-line diff using token Myers algorithm with sub-character refinement,
// preserving Chroma syntax highlighting tokens (`oldSyntaxHtml`, `newSyntaxHtml`) when available.
function diffLine(oldStr = '', newStr = '', oldSyntaxHtml = '', newSyntaxHtml = '') {
  if (oldStr === newStr) {
    return {
      oldHtml: oldSyntaxHtml || esc(oldStr),
      newHtml: newSyntaxHtml || esc(newStr),
    };
  }

  const oldMask = new Uint8Array(oldStr.length);
  const newMask = new Uint8Array(newStr.length);
  const oldBars = new Set();
  const newBars = new Set();

  const script = myersDiff(tokenize(oldStr), tokenize(newStr));
  if (!script) {
    return diffLinePrefixSuffix(oldStr, newStr, oldSyntaxHtml, newSyntaxHtml);
  }

  let oldHtml = '', newHtml = '';
  let oldPos = 0, newPos = 0;
  let i = 0;
  while (i < script.length) {
    if (script[i].type === 'eq') {
      const val = script[i].val;
      oldHtml += esc(val);
      newHtml += esc(val);
      oldPos += val.length;
      newPos += val.length;
      i++;
      continue;
    }
    let dels = '', adds = '';
    while (i < script.length && script[i].type === 'del') dels += script[i++].val;
    while (i < script.length && script[i].type === 'add') adds += script[i++].val;

    if (dels && adds) {
      // Sub-character diff between modified token clusters
      let start = 0;
      while (start < dels.length && start < adds.length && dels[start] === adds[start]) start++;
      let delEnd = dels.length - 1;
      let addEnd = adds.length - 1;
      while (delEnd >= start && addEnd >= start && dels[delEnd] === adds[addEnd]) {
        delEnd--; addEnd--;
      }
      const dMid = dels.slice(start, delEnd + 1);
      const aMid = adds.slice(start, addEnd + 1);

      for (let k = start; k <= delEnd; k++) oldMask[oldPos + k] = 1;
      for (let k = start; k <= addEnd; k++) newMask[newPos + k] = 1;
      if (!dMid && aMid) oldBars.add(oldPos + start);
      if (!aMid && dMid) newBars.add(newPos + start);

      const dSpan = dMid
        ? '<span class="diff-char-del">' + esc(dMid) + '</span>'
        : (aMid ? '<span class="diff-char-bar-del"></span>' : '');
      const aSpan = aMid
        ? '<span class="diff-char-add">' + esc(aMid) + '</span>'
        : (dMid ? '<span class="diff-char-bar-add"></span>' : '');

      oldHtml += esc(dels.slice(0, start)) + dSpan + esc(dels.slice(delEnd + 1));
      newHtml += esc(adds.slice(0, start)) + aSpan + esc(adds.slice(addEnd + 1));
    } else if (dels) {
      for (let k = 0; k < dels.length; k++) oldMask[oldPos + k] = 1;
      newBars.add(newPos);
      oldHtml += '<span class="diff-char-del">' + esc(dels) + '</span>';
      newHtml += '<span class="diff-char-bar-add"></span>';
    } else if (adds) {
      for (let k = 0; k < adds.length; k++) newMask[newPos + k] = 1;
      oldBars.add(oldPos);
      oldHtml += '<span class="diff-char-bar-del"></span>';
      newHtml += '<span class="diff-char-add">' + esc(adds) + '</span>';
    }
    oldPos += dels.length;
    newPos += adds.length;
  }

  return {
    oldHtml: overlayCharMask(oldSyntaxHtml, oldStr.length, oldMask, 'diff-char-del', oldBars, 'diff-char-bar-del') || oldHtml,
    newHtml: overlayCharMask(newSyntaxHtml, newStr.length, newMask, 'diff-char-add', newBars, 'diff-char-bar-add') || newHtml,
  };
}

function diffLinePrefixSuffix(oldStr, newStr, oldSyntaxHtml = '', newSyntaxHtml = '') {
  const oldLen = oldStr.length, newLen = newStr.length;
  let start = 0;
  while (start < oldLen && start < newLen && oldStr[start] === newStr[start]) start++;
  let oldEnd = oldLen - 1, newEnd = newLen - 1;
  while (oldEnd >= start && newEnd >= start && oldStr[oldEnd] === newStr[newEnd]) {
    oldEnd--; newEnd--;
  }
  const oldMid = oldStr.slice(start, oldEnd + 1);
  const newMid = newStr.slice(start, newEnd + 1);

  const oldMask = new Uint8Array(oldLen);
  const newMask = new Uint8Array(newLen);
  const oldBars = new Set();
  const newBars = new Set();
  for (let k = start; k <= oldEnd; k++) oldMask[k] = 1;
  for (let k = start; k <= newEnd; k++) newMask[k] = 1;
  if (!oldMid && newMid) oldBars.add(start);
  if (!newMid && oldMid) newBars.add(start);

  const dSpan = oldMid
    ? '<span class="diff-char-del">' + esc(oldMid) + '</span>'
    : (newMid ? '<span class="diff-char-bar-del"></span>' : '');
  const aSpan = newMid
    ? '<span class="diff-char-add">' + esc(newMid) + '</span>'
    : (oldMid ? '<span class="diff-char-bar-add"></span>' : '');
  const plainOld = esc(oldStr.slice(0, start)) + dSpan + esc(oldStr.slice(oldEnd + 1));
  const plainNew = esc(newStr.slice(0, start)) + aSpan + esc(newStr.slice(newEnd + 1));
  return {
    oldHtml: overlayCharMask(oldSyntaxHtml, oldLen, oldMask, 'diff-char-del', oldBars, 'diff-char-bar-del') || plainOld,
    newHtml: overlayCharMask(newSyntaxHtml, newLen, newMask, 'diff-char-add', newBars, 'diff-char-bar-add') || plainNew,
  };
}


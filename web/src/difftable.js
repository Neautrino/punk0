// web/src/difftable.js
// Draws diff hunks ({oldStart, newStart, rows: [{type, oldLine, newLine, at,
// text, html}]}, as /api/diff and /api/changes return them) as rows, unified
// or split side by side, with changed words marked inside changed lines.
// Shared by the editor's diff view (diff.js) and the agents page's Diff view
// (orch/changes.js), which style them alike (style.css .diff-*).
//
// opts: reviewable (a row's line number is a review-comment target; see
// anchor), and lineActions, the title of the line-actions button each line
// number carries, which also makes the number open its line; none without it.
import { esc } from './state.js';

export function hunkHeader(hunk) {
  const el = document.createElement('div');
  el.className = 'diff-hunk-head';
  el.textContent = '@@ -' + hunk.oldStart + ' +' + hunk.newStart + ' @@';
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
      const diff = diffLine(dels[k].text, adds[k].text);
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
      codeCell(rowHtml.has(row) ? { ...row, html: rowHtml.get(row) } : row),
    );
    table.append(r);
  }
  return table;
}

/* ---------- split layout: deletions and additions paired side by side ---------- */

export function splitTable(hunk, opts = {}) {
  const table = document.createElement('div');
  table.className = 'diff-table diff-split';
  for (const pair of pairRows(hunk.rows)) {
    let leftRow = pair.left;
    let rightRow = pair.right;
    if (leftRow && rightRow && leftRow.type === 'del' && rightRow.type === 'add') {
      const diff = diffLine(leftRow.text, rightRow.text);
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
  el.append(lineCell(ln, opts), markerCell(row.type), codeCell(row));
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

function codeCell(arg) {
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
    trace.push(new Int32Array(v));
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
          let prevK = (curK === -step || (curK !== step && prevV[curK - 1 + max] < prevV[curK + 1 + max]))
            ? curK + 1
            : curK - 1;
          const prevX = prevV[prevK + max];
          const prevY = prevX - prevK;

          while (curX > prevX && curY > prevY && aTokens[curX - 1] === bTokens[curY - 1]) {
            script.unshift({ type: 'eq', val: aTokens[curX - 1] });
            curX--; curY--;
          }
          if (curX > prevX) {
            script.unshift({ type: 'del', val: aTokens[curX - 1] });
            curX--;
          } else if (curY > prevY) {
            script.unshift({ type: 'add', val: bTokens[curY - 1] });
            curY--;
          }
        }
        while (curX > 0 && curY > 0) {
          script.unshift({ type: 'eq', val: aTokens[curX - 1] });
          curX--; curY--;
        }
        return script;
      }
    }
  }
  return null;
}

// Computes an intra-line diff using token Myers algorithm with sub-character refinement.
function diffLine(oldStr = '', newStr = '') {
  if (oldStr === newStr) {
    return { oldHtml: esc(oldStr), newHtml: esc(newStr) };
  }

  const script = myersDiff(tokenize(oldStr), tokenize(newStr));
  if (!script) {
    return diffLinePrefixSuffix(oldStr, newStr);
  }

  let oldHtml = '', newHtml = '';
  let i = 0;
  while (i < script.length) {
    if (script[i].type === 'eq') {
      oldHtml += esc(script[i].val);
      newHtml += esc(script[i].val);
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

      const dSpan = dMid
        ? '<span class="diff-char-del">' + esc(dMid) + '</span>'
        : (aMid ? '<span class="diff-char-bar-del"></span>' : '');
      const aSpan = aMid
        ? '<span class="diff-char-add">' + esc(aMid) + '</span>'
        : (dMid ? '<span class="diff-char-bar-add"></span>' : '');

      oldHtml += esc(dels.slice(0, start)) + dSpan + esc(dels.slice(delEnd + 1));
      newHtml += esc(adds.slice(0, start)) + aSpan + esc(adds.slice(addEnd + 1));
    } else if (dels) {
      oldHtml += '<span class="diff-char-del">' + esc(dels) + '</span>';
      newHtml += '<span class="diff-char-bar-add"></span>';
    } else if (adds) {
      oldHtml += '<span class="diff-char-bar-del"></span>';
      newHtml += '<span class="diff-char-add">' + esc(adds) + '</span>';
    }
  }

  return { oldHtml, newHtml };
}

function diffLinePrefixSuffix(oldStr, newStr) {
  const oldLen = oldStr.length, newLen = newStr.length;
  let start = 0;
  while (start < oldLen && start < newLen && oldStr[start] === newStr[start]) start++;
  let oldEnd = oldLen - 1, newEnd = newLen - 1;
  while (oldEnd >= start && newEnd >= start && oldStr[oldEnd] === newStr[newEnd]) {
    oldEnd--; newEnd--;
  }
  const oldMid = oldStr.slice(start, oldEnd + 1);
  const newMid = newStr.slice(start, newEnd + 1);
  const dSpan = oldMid
    ? '<span class="diff-char-del">' + esc(oldMid) + '</span>'
    : (newMid ? '<span class="diff-char-bar-del"></span>' : '');
  const aSpan = newMid
    ? '<span class="diff-char-add">' + esc(newMid) + '</span>'
    : (oldMid ? '<span class="diff-char-bar-add"></span>' : '');
  return {
    oldHtml: esc(oldStr.slice(0, start)) + dSpan + esc(oldStr.slice(oldEnd + 1)),
    newHtml: esc(newStr.slice(0, start)) + aSpan + esc(newStr.slice(newEnd + 1)),
  };
}


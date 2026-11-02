// web/src/listbox.js
// punk0's dropdown list, shared by every value picker: the font fields of the
// settings modal and each <select> (selectbox.js). One list exists per page,
// opened under the field it serves; focus stays in that field, which points
// at the highlighted row with aria-activedescendant. No imports, so the
// settings modal can use it without pulling anything into either bundle.
//
// openListbox(anchor, {groups, value, active, onPick, onClose, footer,
// empty, align, minWidth, typeahead, owner}):
//   groups   [{label?, count?, items: [{value, label, tag?, title?, font?, disabled?}]}]
//   value    the current value: its row is highlighted and scrolled to
//   active   index to highlight instead (0 while filtering)
//   onPick   (item) => void, after the list closes
//   onClose  () => void, however it closes
//   footer   {label, run}: a row fixed at the bottom; run() may updateListbox
//   empty    text shown when no group has items
//   align    'start' (default) or 'end': which anchor edge the list lines up with
//   minWidth px; the list is at least as wide as the anchor
//   typeahead letters jump to the next row starting with them
//   owner    the element around the anchor whose presses do not close the
//            list (the anchor itself when not given)

let el = null;
let st = null; // the open list: {anchor, opts, items, sel}
let typed = '';
let typedAt = 0;

function ensure() {
  if (el) return el;
  el = document.createElement('div');
  el.id = 'lb-menu';
  el.className = 'lb-menu';
  el.setAttribute('role', 'listbox');
  el.hidden = true;
  document.body.appendChild(el);
  // A press in the list never takes focus from the field, and never reaches
  // the document: the dialog or panel the field sits in would take a press
  // in a list outside it as a click outside, and close.
  el.addEventListener('mousedown', e => { e.preventDefault(); e.stopPropagation(); });
  el.addEventListener('click', e => {
    e.stopPropagation();
    if (!st) return;
    if (e.target.closest('.lb-foot')) return st.opts.footer?.run();
    const row = e.target.closest('[data-lb]');
    if (row) pick(Number(row.dataset.lb));
  });
  el.addEventListener('mousemove', e => {
    const row = e.target.closest('[data-lb]');
    if (st && row && Number(row.dataset.lb) !== st.sel && !st.items[row.dataset.lb].disabled) mark(Number(row.dataset.lb), false);
  });
  // Outside presses close it; scrolling or resizing moves it with its field.
  document.addEventListener('mousedown', e => {
    if (st && !el.contains(e.target) && !(st.opts.owner || st.anchor).contains(e.target)) closeListbox();
  }, true);
  addEventListener('scroll', e => { if (st && !el.contains(e.target)) place(); }, true);
  addEventListener('resize', () => st && place());
  return el;
}

const escHTML = s => String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);

function render() {
  const { opts } = st;
  const items = [];
  let html = '';
  for (const g of opts.groups || []) {
    if (!g.items?.length) continue;
    if (g.label) html += `<div class="lb-group" role="presentation">${escHTML(g.label)}${g.count ? ` <span>${g.count}</span>` : ''}</div>`;
    for (const it of g.items) {
      const i = items.push(it) - 1;
      html += `<div class="lb-opt${it.disabled ? ' lb-disabled' : ''}" role="option" id="lb-opt-${i}" data-lb="${i}"` +
        `${it.disabled ? ' aria-disabled="true"' : ''}${it.title ? ` title="${escHTML(it.title)}"` : ''}>` +
        `<span class="lb-name">${escHTML(it.label)}</span>${it.tag ? `<span class="lb-tag">${escHTML(it.tag)}</span>` : ''}</div>`;
    }
  }
  if (!items.length && opts.empty) html += `<div class="lb-note">${escHTML(opts.empty)}</div>`;
  if (opts.footer) html += `<button type="button" class="lb-foot" tabindex="-1">${escHTML(opts.footer.label)}</button>`;
  el.innerHTML = html;
  // A row drawn in its own font gets it as a style, never in the markup.
  items.forEach((it, i) => {
    if (it.font) el.querySelector(`#lb-opt-${i} .lb-name`).style.fontFamily = it.font;
  });
  st.items = items;
  let sel = opts.active ?? items.findIndex(it => it.value === opts.value);
  if (items[sel]?.disabled) sel = step(sel, 1);
  mark(sel, true);
}

function mark(i, scroll) {
  st.sel = i;
  el.querySelector('.lb-opt.sel')?.classList.remove('sel');
  const row = i >= 0 ? el.querySelector(`#lb-opt-${i}`) : null;
  row?.classList.add('sel');
  if (row) st.anchor.setAttribute('aria-activedescendant', row.id);
  else st.anchor.removeAttribute('aria-activedescendant');
  if (scroll) row?.scrollIntoView({ block: 'nearest' });
}

// step is the next enabled row from i in direction d, wrapping; -1 if none.
function step(i, d) {
  const n = st.items.length;
  for (let k = 1; k <= n; k++) {
    const j = ((i < 0 ? (d > 0 ? -1 : 0) : i) + d * k + n * k) % n;
    if (!st.items[j].disabled) return j;
  }
  return -1;
}

function place() {
  const r = st.anchor.getBoundingClientRect();
  if (!r.width && !r.height) return closeListbox(); // its field went away
  const width = Math.max(r.width, st.opts.minWidth || 0);
  const below = innerHeight - r.bottom - 8;
  const above = r.top - 8;
  const roomBelow = below >= 160 || below >= above;
  const max = Math.min(340, Math.max(80, (roomBelow ? below : above) - 4));
  el.style.width = `${width}px`;
  el.style.maxHeight = `${max}px`;
  const left = st.opts.align === 'end' ? r.right - width : r.left;
  el.style.left = `${Math.max(8, Math.min(left, innerWidth - width - 8))}px`;
  el.style.top = roomBelow ? `${r.bottom + 4}px` : `${Math.max(8, r.top - 4 - Math.min(max, el.scrollHeight))}px`;
}

function pick(i) {
  const it = st?.items[i];
  if (!it || it.disabled) return;
  const { onPick } = st.opts;
  closeListbox();
  onPick?.(it);
}

export function openListbox(anchor, opts) {
  ensure();
  if (st && st.anchor !== anchor) closeListbox();
  st = { anchor, opts, items: [], sel: -1 };
  anchor.setAttribute('aria-expanded', 'true');
  anchor.setAttribute('aria-controls', 'lb-menu');
  el.hidden = false;
  render();
  place();
  mark(st.sel, true);
}

// updateListbox re-renders the open list with changed options, e.g. as the
// user types a filter.
export function updateListbox(opts) {
  if (!st) return;
  st.opts = { ...st.opts, ...opts };
  render();
  place();
}

export function closeListbox() {
  if (!st) return;
  const { anchor, opts } = st;
  st = null;
  el.hidden = true;
  anchor.setAttribute('aria-expanded', 'false');
  anchor.removeAttribute('aria-activedescendant');
  opts.onClose?.();
}

export const listboxOpenFor = anchor => !!st && st.anchor === anchor;

// listboxKey handles a key pressed in the field of the open list and
// reports whether it did. Escape closes the list only: the event stops here,
// so a modal around the field stays open.
export function listboxKey(e) {
  if (!st) return false;
  const k = e.key;
  if (k === 'ArrowDown' || k === 'ArrowUp') mark(step(st.sel, k === 'ArrowDown' ? 1 : -1), true);
  else if (k === 'Home' || k === 'End') mark(step(k === 'Home' ? -1 : 0, k === 'Home' ? 1 : -1), true);
  else if (k === 'PageDown' || k === 'PageUp') {
    const d = k === 'PageDown' ? 1 : -1;
    let i = st.sel;
    for (let n = 0; n < 8; n++) { const j = step(i, d); if ((d > 0 && j < i) || (d < 0 && j > i)) break; i = j; }
    mark(i, true);
  } else if (k === 'Enter' || (k === ' ' && st.opts.typeahead && Date.now() - typedAt > 700)) {
    if (st.sel < 0) return false;
    pick(st.sel);
  } else if (k === 'Escape') {
    e.stopPropagation();
    closeListbox();
  } else if (k === 'Tab') {
    closeListbox();
    return false; // focus moves on as usual
  } else if (st.opts.typeahead && k.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
    const now = Date.now();
    typed = (now - typedAt < 700 ? typed : '') + k.toLowerCase();
    typedAt = now;
    const n = st.items.length;
    const from = typed.length > 1 ? st.sel : st.sel + 1;
    for (let k2 = 0; k2 < n; k2++) {
      const j = (Math.max(from, 0) + k2) % n;
      if (!st.items[j].disabled && String(st.items[j].label).toLowerCase().startsWith(typed)) { mark(j, true); break; }
    }
  } else return false;
  e.preventDefault();
  return true;
}

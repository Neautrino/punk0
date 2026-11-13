// web/src/tabdrag.js
// Dragging tabs: press on a tab in a registered bar and move 4 px, and a
// ghost of it follows the pointer. Over a bar a line shows the gap it would
// land in (reordering); over a group's body the group is outlined (append);
// over a side zone, a new group would open. Esc or a drop on nothing
// cancels. Kinds of tabs mean nothing here: the page's canDrop and onDrop
// decide. Pointer events, not HTML5 drag-and-drop: xterm's canvases and the
// existing resizers (resize.js, split.js) work the same way.
const THRESHOLD = 4;
const EDGE = 28; // px from a bar's end that scrolls it
const STEP = 10;

// insertionAt is the index of the gap x falls in among tab rectangles in
// order: before the first tab whose middle is right of x, else the end.
export function insertionAt(rects, x) {
  for (let i = 0; i < rects.length; i++) if (x < (rects[i].left + rects[i].right) / 2) return i;
  return rects.length;
}

const inside = (r, x, y) => x >= r.left && x < r.right && y >= r.top && y < r.bottom;

// createTabDrag binds the page's drag. canDrop({id, to, before}) says
// whether a drop there is allowed; onDrop({id, from, to, before}) does it,
// with before the id of the tab it goes ahead of, '' for the end. tabSel
// finds a tab in a bar and idOf reads its id.
export function createTabDrag({ canDrop = () => true, onDrop, tabSel = '[data-tab]', idOf = el => el.dataset.tab }) {
  const targets = []; // {el, kind: 'bar' | 'body' | 'side', group}
  let press = null; // {id, from, el, x, y, pointerId}
  let drag = null; // {ghost, dx, dy, over}
  let suppressClick = false;

  const line = document.createElement('div');
  line.className = 'tab-drop-line';
  line.hidden = true;
  const box = document.createElement('div');
  box.className = 'tab-drop-box';
  box.hidden = true;
  document.body.append(line, box);

  function onDown(e) {
    if (e.button !== 0 || e.target.closest('[data-act=close], .x, input')) return;
    const tab = e.target.closest(tabSel);
    const bar = tab && targets.find(t => t.kind === 'bar' && t.el.contains(tab));
    if (!bar) return;
    press = { id: idOf(tab), from: bar.group, el: tab, x: e.clientX, y: e.clientY, pointerId: e.pointerId };
  }

  function start(e) {
    const r = press.el.getBoundingClientRect();
    const ghost = press.el.cloneNode(true);
    ghost.classList.add('tab-ghost');
    ghost.style.width = `${r.width}px`;
    document.body.append(ghost);
    drag = { ghost, dx: press.x - r.left, dy: press.y - r.top, over: null };
    press.el.classList.add('tab-dragged');
    document.body.classList.add('tab-dragging');
    try { press.el.setPointerCapture(press.pointerId); } catch {}
    move(e);
  }

  // hit finds what is under the pointer: a side zone, then a bar, then a
  // group body, and where in a bar the tab would land.
  function hit(x, y) {
    for (const kind of ['side', 'bar', 'body']) {
      for (const t of targets) {
        // Hidden targets (hidden attribute, display:none, the editor's
        // right-hidden class) have no box: checkVisibility covers them all.
        if (t.kind !== kind || !t.el.isConnected || !t.el.checkVisibility()) continue;
        const r = t.el.getBoundingClientRect();
        if (!inside(r, x, y)) continue;
        if (kind !== 'bar') return { t, to: t.group, before: '', rect: r };
        const tabs = [...t.el.querySelectorAll(tabSel)].filter(el => idOf(el) !== press.id);
        const i = insertionAt(tabs.map(el => el.getBoundingClientRect()), x);
        const at = tabs[i]?.getBoundingClientRect();
        const lineX = at ? at.left : tabs.length ? tabs[tabs.length - 1].getBoundingClientRect().right : r.left;
        return { t, to: t.group, before: tabs[i] ? idOf(tabs[i]) : '', rect: r, lineX };
      }
    }
    return null;
  }

  function move(e) {
    const { ghost } = drag;
    ghost.style.left = `${e.clientX - drag.dx}px`;
    ghost.style.top = `${e.clientY - drag.dy}px`;
    let h = hit(e.clientX, e.clientY);
    if (h && !canDrop({ id: press.id, from: press.from, to: h.to, before: h.before, kind: h.t.kind })) h = null;
    drag.over = h;
    line.hidden = !(h && h.t.kind === 'bar');
    box.hidden = !(h && h.t.kind !== 'bar');
    if (h?.t.kind === 'bar') {
      Object.assign(line.style, { left: `${h.lineX - 1}px`, top: `${h.rect.top}px`, height: `${h.rect.height}px` });
      const r = h.rect;
      if (e.clientX < r.left + EDGE) h.t.el.scrollLeft -= STEP;
      else if (e.clientX > r.right - EDGE) h.t.el.scrollLeft += STEP;
    } else if (h) {
      Object.assign(box.style, { left: `${h.rect.left}px`, top: `${h.rect.top}px`, width: `${h.rect.width}px`, height: `${h.rect.height}px` });
      box.dataset.kind = h.t.kind;
    }
  }

  function end(commit) {
    const over = drag?.over;
    if (drag) {
      drag.ghost.remove();
      press.el.classList.remove('tab-dragged');
      document.body.classList.remove('tab-dragging');
      line.hidden = box.hidden = true;
      // The click that ends a drag must not select: it comes right after the
      // pointerup (or after the release, when Esc cancelled mid-drag), if at
      // all: a release over another element than the press has none.
      suppressClick = true;
    }
    const p = press;
    press = drag = null;
    if (commit && over) onDrop({ id: p.id, from: p.from, to: over.to, before: over.before, kind: over.t.kind });
  }

  addEventListener('pointermove', e => {
    if (!press || e.pointerId !== press.pointerId) return;
    if (!drag) {
      if (Math.hypot(e.clientX - press.x, e.clientY - press.y) < THRESHOLD) return;
      start(e);
    } else move(e);
  });
  addEventListener('pointerup', e => {
    if (press && e.pointerId === press.pointerId) end(true);
    // A click this release causes is dispatched before this timer runs.
    if (suppressClick) setTimeout(() => { suppressClick = false; }, 0);
  });
  addEventListener('pointercancel', () => {
    if (press) end(false);
    suppressClick = false; // no click follows a cancelled pointer
  });
  addEventListener('keydown', e => {
    if (drag && e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      end(false);
    }
  }, true);
  addEventListener('click', e => {
    if (!suppressClick) return;
    suppressClick = false;
    e.preventDefault();
    e.stopPropagation();
  }, true);

  return {
    // bar registers a tab bar of group g; its tabs become draggable.
    bar(el, group) {
      targets.push({ el, kind: 'bar', group });
      el.addEventListener('pointerdown', onDown);
    },
    // body registers group g's body: a drop there appends to the group.
    body(el, group) { targets.push({ el, kind: 'body', group }); },
    // side registers a zone that opens group g (only while it shows).
    side(el, group) { targets.push({ el, kind: 'side', group }); },
    get active() { return !!drag; },
  };
}

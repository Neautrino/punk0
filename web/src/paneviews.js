// web/src/paneviews.js
// A pane's modes: a role=tablist bar of buttons, one per data-view, picking
// which of host's <section data-pane-view> children shows. Shared by the
// agents page's right side (Files | Chats), the editor's sidebar (Files |
// Search | Symbols | References) and the editor's help card (Shortcuts |
// Tips | Vim). key, when given, remembers the choice in this browser; a mode
// the host does not have shows fallback. onChange(id) follows each change.
export function paneViews({ host, bars, key = '', fallback, onChange = (/** @type {string} */ _id) => {} }) {
  let current = '';
  const has = id => !!host.querySelector(`:scope > [data-pane-view="${CSS.escape(id)}"]`);

  function set(id) {
    if (!id || !has(id)) id = fallback;
    if (id === current) return;
    current = id;
    for (const s of host.querySelectorAll(':scope > [data-pane-view]')) s.hidden = s.dataset.paneView !== id;
    for (const bar of bars) {
      for (const b of bar.querySelectorAll('[data-view]')) {
        const on = b.dataset.view === id;
        b.setAttribute('aria-selected', String(on));
        b.tabIndex = on ? 0 : -1;
      }
    }
    if (key) try { localStorage.setItem(key, id); } catch {}
    onChange(id);
  }

  // The arrow keys, Home and End move along the bar, as in any tablist.
  function onKey(e) {
    const btns = [...e.currentTarget.querySelectorAll('[data-view]')];
    const i = btns.indexOf(document.activeElement);
    if (i < 0) return;
    const n = btns.length;
    const to = { ArrowRight: (i + 1) % n, ArrowLeft: (i + n - 1) % n, Home: 0, End: n - 1 }[e.key];
    if (to === undefined) return;
    e.preventDefault();
    set(btns[to].dataset.view);
    btns[to].focus();
  }

  for (const bar of bars) {
    bar.addEventListener('click', e => {
      const b = e.target.closest('[data-view]');
      if (b) set(b.dataset.view);
    });
    bar.addEventListener('keydown', onKey);
  }
  let saved = '';
  if (key) try { saved = localStorage.getItem(key) || ''; } catch {}
  set(saved);
  return { set, current: () => current };
}

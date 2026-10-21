// web/src/orch/complete.js
// Folder path completion under a text input, from GET /api/wb/complete.
import { esc } from '../state.js';
import { call } from './api.js';

const ICON_FOLDER = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>';

export const looksLikeURL = v => /^[a-z]+:\/\//i.test(v.trim());

// attachComplete turns a text input into a folder path field: as you type, it
// lists folders whose names start with the last path element. Tab (or a
// click) completes into the highlighted folder and lists its subfolders;
// Enter takes the highlighted folder if one was picked with the arrows.
export function attachComplete(input, list) {
  let items = [];
  let sel = -1;
  let seq = 0;
  let timer = 0;

  const close = () => { list.hidden = true; items = []; sel = -1; input.setAttribute('aria-expanded', 'false'); };
  const render = () => {
    const prefix = (input.value.match(/[^/\\]*$/) || [''])[0];
    list.innerHTML = items.map((e, i) => `<li role="option" data-i="${i}" aria-selected="${i === sel}">
      <span class="wb-ico">${ICON_FOLDER}</span>
      <span class="wb-sg-name"><b>${esc(e.name.slice(0, prefix.length))}</b>${esc(e.name.slice(prefix.length))}</span>
      ${e.git ? '<span class="wb-git">git</span>' : ''}
      <span class="wb-sg-path">${esc(e.path)}</span></li>`).join('');
    list.hidden = !items.length;
    input.setAttribute('aria-expanded', String(!list.hidden));
    list.children[sel]?.scrollIntoView({ block: 'nearest' });
  };
  const fetchNow = async () => {
    const q = input.value;
    if (!q.trim() || looksLikeURL(q)) { close(); return; }
    const my = ++seq;
    const res = await call('GET', 'api/wb/complete?' + new URLSearchParams({ q }));
    if (!res.ok || my !== seq || document.activeElement !== input) return;
    items = res.data.entries || [];
    sel = -1;
    render();
  };
  const refresh = () => { clearTimeout(timer); timer = setTimeout(fetchNow, 60); };
  const accept = i => {
    const e = items[i];
    if (!e) return;
    input.value = e.path;
    input.focus();
    fetchNow();
  };

  input.addEventListener('input', refresh);
  input.addEventListener('focus', () => { if (input.value) refresh(); });
  input.addEventListener('blur', () => setTimeout(close, 150));
  input.addEventListener('keydown', e => {
    if (list.hidden || !items.length) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      sel = (sel + (e.key === 'ArrowDown' ? 1 : -1) + items.length + 1) % (items.length + 1);
      if (sel === items.length) sel = -1;
      render();
    } else if (e.key === 'Tab' && !e.shiftKey) {
      e.preventDefault();
      accept(sel >= 0 ? sel : 0);
    } else if (e.key === 'Enter' && sel >= 0) {
      input.value = items[sel].path;
      close();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation(); // the list closes, not the form around it
      close();
    }
  });
  list.addEventListener('mousedown', e => {
    const li = e.target.closest('li[data-i]');
    if (!li) return;
    e.preventDefault(); // keep focus in the input
    accept(Number(li.dataset.i));
  });
  return { close };
}

// web/src/orch/browser.js
// A small folder browser for the flyouts (New chat, Add project): the folder
// it is in, a button up to its parent, and its subfolders, from GET
// /api/wb/complete. Clicking a folder opens it. A row can carry a checkbox
// (Add project ticks the repositories inside a folder) or a badge.
import { esc } from '../state.js';
import { call } from './api.js';
import { state, tildify } from './store.js';

const ICON_FOLDER = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>';
const ICON_UP = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 12H5M12 19l-7-7 7-7"/></svg>';

export const parentOf = dir => {
  const d = dir.replace(/[/\\]+$/, '');
  const i = Math.max(d.lastIndexOf('/'), d.lastIndexOf('\\'));
  return i > 0 ? d.slice(0, i) : d.slice(0, i + 1) || state.wb.sep;
};

export const joinPath = (dir, name) => dir.replace(/[/\\]+$/, '') + state.wb.sep + name;

// folderBrowser fills root. opts:
//   onOpen(dir, entries)  after a folder is shown (dir as the server spells it)
//   badge(path, entry)    text of a badge for a row, or ''
//   checkable(path, entry) whether the row gets a checkbox
//   onCheck(paths)        the ticked paths changed
export function folderBrowser(root, opts = {}) {
  root.innerHTML = `<div class="o-browser-head">
      <button type="button" class="mini" data-up title="Back to the parent folder" aria-label="Parent folder">${ICON_UP}</button>
      <span class="o-browser-dir"></span>
    </div>
    <ul class="o-browser-list" role="listbox" aria-label="Folders"></ul>`;
  const dirEl = root.querySelector('.o-browser-dir');
  const list = root.querySelector('.o-browser-list');
  let shown = ''; // the folder shown, as the server spelled it
  let seq = 0;
  const checked = new Set();

  const changed = () => opts.onCheck?.([...checked]);

  // browse shows the folders in dir; fallback is tried when dir cannot be
  // listed, then the home folder.
  async function browse(dir, fallback = '') {
    const my = ++seq;
    const res = await call('GET', 'api/wb/complete?' + new URLSearchParams({ q: dir.replace(/[/\\]*$/, '/'), all: '1' }));
    if (my !== seq) return;
    if (!res.ok || !res.data.dir) {
      if (fallback) return browse(fallback, '~');
      if (dir !== '~') return browse('~');
      list.innerHTML = '<li class="o-note">Cannot list this folder.</li>';
      return;
    }
    shown = res.data.dir;
    dirEl.textContent = tildify(shown);
    dirEl.title = shown;
    const entries = res.data.entries || [];
    if (checked.size) {
      checked.clear();
      changed();
    }
    list.innerHTML = entries.length
      ? entries.map(e => {
        const path = joinPath(shown, e.name);
        const badge = opts.badge?.(path, e) || '';
        const box = opts.checkable?.(path, e)
          ? `<input type="checkbox" data-check="${esc(path)}" title="Add as a project" aria-label="Add ${esc(e.name)} as a project">` : '';
        return `<li role="option" tabindex="0" data-path="${esc(path)}">${box}
          <span class="o-browser-ico">${ICON_FOLDER}</span><span class="o-browser-name">${esc(e.name)}</span>${e.git ? '<span class="wb-git">git</span>' : ''}${badge ? `<span class="o-browser-badge">${esc(badge)}</span>` : ''}</li>`;
      }).join('')
      : '<li class="o-note">No folders here.</li>';
    list.scrollTop = 0;
    opts.onOpen?.(shown, entries);
  }

  root.querySelector('[data-up]').addEventListener('click', () => browse(parentOf(shown)));
  list.addEventListener('click', e => {
    const box = e.target.closest('[data-check]');
    if (box) {
      box.checked ? checked.add(box.dataset.check) : checked.delete(box.dataset.check);
      return changed();
    }
    const li = e.target.closest('[data-path]');
    if (li) browse(li.dataset.path);
  });
  list.addEventListener('keydown', e => {
    const li = e.target.closest('[data-path]');
    if (li && e.target === li && (e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault();
      browse(li.dataset.path);
    }
  });

  return {
    browse,
    dir: () => shown,
    checked: () => [...checked],
  };
}

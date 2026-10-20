// web/src/sidebar.js
// Satellite list: switching, creating and deleting satellites. The caller's
// onSelect swaps the terminal and file view to the chosen satellite.

let listEl, formEl, nameEl, onSelect;
let active = 'default';

export function initSidebar(container, select) {
  listEl = container.querySelector('#satellites-list');
  formEl = container.querySelector('#satellites-form');
  nameEl = container.querySelector('#input-sat-name');
  onSelect = select;

  container.querySelector('#btn-new-sat').addEventListener('click', () => {
    formEl.classList.remove('hidden');
    nameEl.value = '';
    nameEl.focus();
  });
  container.querySelector('#btn-sat-cancel').addEventListener('click', () => formEl.classList.add('hidden'));
  container.querySelector('#btn-sat-create').addEventListener('click', create);
  nameEl.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') create();
    if (e.key === 'Escape') formEl.classList.add('hidden');
  });
  listEl.addEventListener('click', onListClick);
  return reload();
}

async function reload() {
  const res = await fetch('/api/satellites');
  if (res.ok) render(await res.json());
}

function render(sats) {
  listEl.innerHTML = sats.map((s) => `
    <div class="sat-item${s.id === active ? ' active' : ''}" data-id="${esc(s.id)}">
      <div class="sat-info">
        <div class="sat-header">
          <span class="sat-name">${esc(s.name)}</span>
          ${s.primary ? '<span class="sat-badge primary">main</span>' : ''}
        </div>
        <div class="sat-branch">${esc(s.branch || '(no branch)')}</div>
      </div>
      ${s.primary ? '' : `<button class="sat-del" data-id="${esc(s.id)}" title="Delete satellite">×</button>`}
    </div>`).join('');
}

function select(id) {
  active = id;
  for (const el of listEl.querySelectorAll('.sat-item')) el.classList.toggle('active', el.dataset.id === id);
  onSelect(id);
}

async function onListClick(e) {
  const del = e.target.closest('.sat-del');
  if (del) {
    const id = del.dataset.id;
    if (!confirm(`Delete satellite "${id}"? Its shell stops and uncommitted changes in its worktree are lost. The branch is kept.`)) return;
    const res = await fetch(`/api/satellites/${encodeURIComponent(id)}`, { method: 'DELETE' });
    if (!res.ok) return alert((await res.json()).error);
    if (id === active) select('default');
    return reload();
  }
  const item = e.target.closest('.sat-item');
  if (item && item.dataset.id !== active) select(item.dataset.id);
}

async function create() {
  const name = nameEl.value.trim();
  if (!name) return;
  const res = await fetch('/api/satellites', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  const j = await res.json();
  if (!res.ok) return alert(j.error);
  formEl.classList.add('hidden');
  await reload();
  select(j.id);
}

function esc(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

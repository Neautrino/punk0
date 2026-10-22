// web/src/orch/newchat.js
// New chat, a panel in the Scratch view's sidebar: a chat's title, the agent
// it runs, its group, and the folder it runs in. The folder starts as a new
// one in punk0's scratch folder, named after the title; it can be typed, with
// completion, or picked in a small folder browser. The agent last used is
// offered again. POST /api/chats (scratch.go) makes the chat; the agents come
// from GET /api/agents (tabagents.go), folders from browser.js.
import { $, esc } from '../state.js';
import { call } from './api.js';
import { state, tildify } from './store.js';
import { getSetting, visibleAgents, defaultAgent } from './settings.js';
import { attachComplete } from './complete.js';
import { placeFlyout } from './flyout.js';
import { folderBrowser, parentOf, joinPath } from './browser.js';

const PREFS = 'punk0.newChat';

let form, titleEl, agentEl, agentNote, groupEl, dirEl, dirNote, msgEl, startEl;
let browseBtn, browser, fb;
let agents = [];
let done = null; // resolves the open flyout's promise
let autoDir = ''; // the path the flyout proposed; edited away when it differs
let anchor = null; // where the button that opened the flyout was; its notch points there

export function initNewChat() {
  form = $('#o-chat-form');
  titleEl = $('#o-chat-title');
  agentEl = $('#o-chat-agent');
  agentNote = $('#o-chat-agent-note');
  groupEl = $('#o-chat-group');
  dirEl = $('#o-chat-dir');
  dirNote = $('#o-chat-dir-note');
  msgEl = $('#o-chat-msg');
  startEl = $('#o-chat-start');
  browseBtn = $('#o-chat-browse');
  browser = $('#o-chat-browser');
  fb = folderBrowser($('#o-chat-browser-view'));

  attachComplete(dirEl, $('#o-chat-dir-suggest'));
  form.addEventListener('submit', e => {
    e.preventDefault();
    start();
  });
  $('#o-chat-cancel').addEventListener('click', () => close(''));
  $('#o-chat-close').addEventListener('click', () => close(''));
  // A click outside closes it, except on the buttons that open it, which
  // move it instead.
  document.addEventListener('mousedown', e => {
    if (done && !form.contains(e.target) && !e.target.closest?.('[data-act=new-chat], #o-add-project')) close('');
  });
  addEventListener('resize', () => done && place());
  // The browser, a note or an error changes its height: stay in the window.
  new ResizeObserver(() => done && place()).observe(form);
  form.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    if (!browser.hidden) return toggleBrowser(false);
    close('');
  });
  // The path follows the title until it is edited.
  titleEl.addEventListener('input', () => {
    if (dirEl.value !== autoDir) return;
    setDir(autoDir = proposedDir());
  });
  dirEl.addEventListener('input', onDir);
  agentEl.addEventListener('change', onAgent);

  browseBtn.addEventListener('click', () => toggleBrowser(browser.hidden));
  $('#o-browser-pick').addEventListener('click', () => {
    setDir(tildify(fb.dir()));
    toggleBrowser(false);
  });
  $('#o-browser-new').addEventListener('click', () => {
    const name = prompt('New folder name, made when the chat starts')?.trim();
    if (!name || /[/\\]/.test(name)) return;
    setDir(tildify(joinPath(fb.dir(), name)));
    toggleBrowser(false);
  });
}

// openNewChat shows the flyout beside the sidebar, pointing at the button
// that opened it, with group preselected when given. It resolves to the new
// chat's workspace id, or '' when cancelled. Opened again while open, it
// moves to the new button and takes its group.
export function openNewChat(group = '', from = null) {
  // Kept as a position: the list re-renders under the flyout.
  anchor = (from || $('#o-add-project')).getBoundingClientRect();
  renderGroups(group);
  if (done) {
    place();
    titleEl.focus();
    return Promise.resolve('');
  }
  titleEl.value = '';
  setDir(autoDir = proposedDir());
  showMsg('');
  toggleBrowser(false);
  form.hidden = false;
  place();
  titleEl.focus();
  loadAgents();
  return new Promise(resolve => { done = resolve; });
}

const place = () => placeFlyout(form, anchor);

// closeNewChat cancels an open panel, as switching sidebar views does.
export const closeNewChat = () => close('');

function close(id) {
  if (!done) return;
  form.hidden = true;
  done(id);
  done = null;
}

const scratch = () => state.projects.find(p => p.scratch);

// proposedDir is a new folder in punk0's scratch folder named after the
// title, or after the time while there is none.
function proposedDir() {
  const slug = titleEl.value.toLowerCase().normalize('NFKD').replace(/[\u0300-\u036f]/g, '')
    .replace(/[^a-z0-9._-]+/g, '-').replace(/^[^a-z0-9]+/, '').slice(0, 48).replace(/[-._]+$/, '');
  const d = new Date();
  const p2 = n => String(n).padStart(2, '0');
  const name = slug || `chat-${p2(d.getMonth() + 1)}${p2(d.getDate())}-${p2(d.getHours())}${p2(d.getMinutes())}`;
  return tildify((scratch()?.dir || '') + state.wb.sep + name);
}

function setDir(v) {
  dirEl.value = v;
  onDir();
}

// onDir says what happens to the folder: punk0's own is deleted with the
// chat, one of yours never is.
function onDir() {
  const dir = dirEl.value.trim();
  const own = tildify(scratch()?.dir || '') + state.wb.sep;
  dirNote.textContent = !dir ? 'Where the agent runs.'
    : dir.startsWith(own) ? 'A new folder in punk0\'s scratch folder, deleted when you delete the chat.'
    : 'Your folder: made if it does not exist, never deleted by punk0.';
}

function toggleBrowser(open) {
  browser.hidden = !open;
  browseBtn.setAttribute('aria-expanded', String(open));
  if (!open) return;
  // Start where the typed path is, or its parent while it does not exist.
  const typed = dirEl.value.trim();
  fb.browse(typed || '~', typed ? parentOf(typed) : '');
}

function renderGroups(selected) {
  const groups = scratch()?.groups || [];
  groupEl.innerHTML = '<option value="">Ungrouped</option>' +
    groups.map(g => `<option value="${esc(g.id)}">${esc(g.name)}</option>`).join('');
  groupEl.value = groups.some(g => g.id === selected) ? selected : '';
}

const loadPrefs = () => {
  try {
    return JSON.parse(localStorage.getItem(PREFS)) || {};
  } catch {
    return {};
  }
};

// loadAgents fills the agent list: installed agents in their usual order,
// then a plain shell, then those not installed, shown but disabled; agents in
// agents.hidden are left out. The agent last used is chosen, else
// agents.default.
async function loadAgents() {
  agentEl.innerHTML = '<option>Loading…</option>';
  agentEl.disabled = startEl.disabled = true;
  const res = await call('GET', 'api/agents');
  if (!done) return;
  agents = res.ok ? res.data : [];
  const visible = visibleAgents(agents);
  const hidden = new Set(getSetting('agents.hidden') || []);
  agentEl.innerHTML = visible.map(a => `<option value="${esc(a.id)}">${esc(a.id === 'shell' ? 'Shell (no agent)' : a.label)}</option>`).join('') +
    agents.filter(a => !a.installed && !hidden.has(a.id)).map(a => `<option value="${esc(a.id)}" disabled>${esc(a.label)} (not installed)</option>`).join('');
  agentEl.disabled = startEl.disabled = !visible.length;
  const last = loadPrefs().agent;
  agentEl.value = visible.some(a => a.id === last) ? last : defaultAgent(visible);
  onAgent();
}

// onAgent says what the chosen agent does here, beyond running in its tab.
function onAgent() {
  const a = agents.find(x => x.id === agentEl.value);
  titleEl.placeholder = a?.status ? 'Optional: else your first prompt' : 'Optional: else the folder\'s name';
  agentNote.hidden = !a || a.status;
  if (a && !a.status) {
    agentNote.textContent = a.resumes
      ? `${a.label} shows no working / needs-you status here. After a restart it picks up its latest session.`
      : `${a.label} shows no working / needs-you status here, and starts a new session after a restart.`;
  }
}

const showMsg = text => {
  msgEl.textContent = text || '';
  msgEl.hidden = !text;
};

async function start() {
  const dir = dirEl.value.trim();
  if (!dir) {
    showMsg('Give the folder the chat runs in.');
    return dirEl.focus();
  }
  // Left as proposed with no title, the server picks a free time-named
  // folder, so two chats in one minute do not collide.
  const body = {
    title: titleEl.value.trim(),
    agent: agentEl.value,
    group: groupEl.value,
    dir: dir === autoDir && !titleEl.value.trim() ? '' : dir,
  };
  startEl.disabled = true;
  const res = await call('POST', 'api/chats', body);
  startEl.disabled = false;
  if (!res.ok) return showMsg(res.data.error);
  localStorage.setItem(PREFS, JSON.stringify({ agent: body.agent }));
  close(res.data.id);
}

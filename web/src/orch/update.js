// web/src/orch/update.js
// An update auto-update installed while punk0 runs (/api/wb/state update):
// a card with Restart now, What's new and Later, and a status bar item that
// stays while the restart is pending and brings the card back. Restart now
// has punk0 stop every tab and run the installed binary in its place. Every
// agents page reloads once the punk0 it talks to reports another version
// than the one it loaded with, so old page code never talks to a new punk0;
// what a page shows is in sessionStorage, so the reload keeps it.
import { $ } from '../state.js';
import { call, failed } from './api.js';
import { state, subscribe, openWhatsNew } from './store.js';

const LATER = 'punk0.updateLater'; // the version Later was clicked for, in this tab

let card, item, restartBtn, textEl;
let loadedWith = ''; // the version of the first state this page read
let restarting = false;

const later = () => {
  try {
    return sessionStorage.getItem(LATER) || '';
  } catch {
    return '';
  }
};
const setLater = v => {
  try {
    if (v) sessionStorage.setItem(LATER, v);
    else sessionStorage.removeItem(LATER);
  } catch {}
};

export function initUpdate() {
  card = $('#o-update');
  item = $('#st-update');
  restartBtn = card.querySelector('[data-act=restart]');
  textEl = card.querySelector('.o-update-text');
  item.addEventListener('click', () => {
    setLater('');
    render();
  });
  card.addEventListener('click', e => {
    const act = e.target.closest('[data-act]')?.dataset.act;
    if (act === 'restart') restart();
    else if (act === 'notes') openWhatsNew(state.wb.update);
    else if (act === 'later') {
      setLater(state.wb.update);
      render();
    }
  });
  subscribe(render);
}

function render() {
  const { version, update } = state.wb;
  if (!version) return; // no state read yet
  loadedWith ||= version;
  if (version !== loadedWith) return location.reload();
  item.hidden = !update;
  card.hidden = !update || (later() === update && !restarting);
  if (!update) return;
  item.textContent = `v${update} ready`;
  card.querySelector('.o-update-title').textContent = `punk0 v${update} is installed`;
}

async function restart() {
  const running = state.projects.flatMap(p => p.workspaces).flatMap(w => w.tabs).filter(t => t.running).length;
  if (running && !confirm(`Restart punk0? ${running === 1 ? '1 running tab stops' : `${running} running tabs stop`}. ` +
    'Agent tabs resume their chat; commands running in shell tabs are killed.')) return;
  restarting = true;
  restartBtn.disabled = true;
  textEl.textContent = 'Restarting punk0…';
  const res = await call('POST', 'api/wb/restart');
  if (!res.ok) {
    restarting = false;
    restartBtn.disabled = false;
    textEl.textContent = 'Restart to use it. Agent tabs resume their chats.';
    return failed(res);
  }
  // The poll reads the new version once punk0 is back, and render reloads.
}

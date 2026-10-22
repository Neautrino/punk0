// web/src/orch/notify.js
// Tab status notifications (Notifications settings): a desktop notification
// and a tone when a tab starts waiting for the user or finishes working, and
// the count of waiting tabs in the page title. Only tabs that report status
// (Claude Code, through its hooks) can notify. Statuses come from the
// store's api/projects polls; workbench.js polls only while the page is in
// view, so this keeps polling every 5 s while it is hidden.
import { on } from '../bus.js';
import { state, subscribe, reload, selectWorkspace, selectTab, wsLabel } from './store.js';
import { getSetting } from './settings.js';

const HIDDEN_POLL = 5000;

const seen = new Map(); // tab id -> status at the last poll
let loaded = false; // the first poll only records
let baseTitle = '';
let audio = null;

export function initNotify() {
  baseTitle = document.title;
  subscribe(detect);
  on('setting:changed', key => key === 'notifications.titleBadge' && detect());
  setInterval(() => {
    if (document.hidden && needed()) reload();
  }, HIDDEN_POLL);
}

const needed = () => (getSetting('notifications.desktop') || 'off') !== 'off' ||
  getSetting('notifications.sound') || getSetting('notifications.titleBadge') !== false;

// detect compares each tab's status with the last poll's. A tab seen for the
// first time only records, like the first poll.
function detect() {
  if (!state.projects.length && !loaded) return;
  let waiting = 0;
  const live = new Set();
  for (const w of state.projects.flatMap(p => p.workspaces)) {
    for (const t of w.tabs) {
      live.add(t.id);
      if (t.status === 'waiting') waiting++;
      const known = seen.has(t.id);
      const was = seen.get(t.id);
      seen.set(t.id, t.status);
      if (!loaded || !known) continue;
      if (t.status === 'waiting' && was !== 'waiting' && getSetting('notifications.onWaiting') !== false) announce(w, t, 'needs your input');
      else if (was === 'working' && t.status === 'idle' && getSetting('notifications.onDone') !== false) announce(w, t, 'finished');
    }
  }
  for (const id of seen.keys()) if (!live.has(id)) seen.delete(id);
  loaded = true;
  const badge = waiting && getSetting('notifications.titleBadge') !== false;
  document.title = badge ? `(${waiting}) ${baseTitle}` : baseTitle;
}

const unfocused = () => document.hidden || !document.hasFocus();

// The tone follows the notification: no desktop notification, no sound.
function announce(w, t, body) {
  const mode = getSetting('notifications.desktop') || 'off';
  if (mode === 'off' || mode === 'unfocused' && !unfocused()) return;
  if (getSetting('notifications.sound')) tone();
  if (typeof Notification === 'undefined' || Notification.permission !== 'granted') return;
  const n = new Notification(`${wsLabel(w)} · ${t.name}`, { body, tag: `punk0-tab-${t.id}` });
  n.onclick = () => {
    window.focus();
    selectWorkspace(w.id);
    selectTab(t.id);
    n.close();
  };
}

// tone plays a short two-note chime; browsers may keep audio suspended until
// the page had a user gesture, and then it stays silent.
function tone() {
  try {
    audio ||= new AudioContext();
    audio.resume?.();
    const now = audio.currentTime;
    for (const [f, at] of [[880, 0], [1320, 0.12]]) {
      const osc = audio.createOscillator();
      const gain = audio.createGain();
      osc.frequency.value = f;
      gain.gain.setValueAtTime(0.0001, now + at);
      gain.gain.exponentialRampToValueAtTime(0.15, now + at + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, now + at + 0.25);
      osc.connect(gain).connect(audio.destination);
      osc.start(now + at);
      osc.stop(now + at + 0.3);
    }
  } catch {}
}

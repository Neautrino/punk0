// web/src/orch/welcometips.js
// The welcome view's tip: one at a time, the next one each time the page
// loads, ‹ › to step through them. They ship with punk0 and fetch nothing.
// tips.enabled off hides it. In a tip, {Mod+B} is a key combination shown
// as the reader's keyboard labels it and **x** is bold.
import { $, esc, keyCaps } from '../state.js';
import { on } from '../bus.js';
import { getSetting } from './settings.js';

const KEY = 'punk0.welcomeTip';

const TIPS = [
  'Terminals keep running when you close this page. Open it again and they are as you left them.',
  'Right-click a tab and pick **Stop**: it stays in the bar, and the agent picks its chat up when you open it again.',
  'Double-click a tab to rename it.',
  '**Chats** on the right side lists every chat your agents kept, even ones started outside punk0. **Resume** opens one in a tab.',
  '**N need you** in the status bar opens the next agent waiting for you; click again for the one after.',
  'In Claude Code, {Shift+Enter} starts a new line instead of sending.',
  '{Mod+B} hides the projects pane, {Mod+Alt+B} the right side.',
  '{Mod+,} opens Settings. **Default Agent** picks what the first button here starts; **Hidden Agents** leaves agents out of every list.',
  '{Mod+G} goes to a line in the open file.',
  '**Scratch** in the sidebar is for work with no repository yet: each chat gets a folder of its own.',
  'The terminals · agents · RAM button in the status bar shows what each tab costs in CPU and memory.',
  '**New workspace** puts a branch in a git worktree of its own, so agents work side by side without touching each other\'s files. {Mod+Enter} creates it.',
  'Click a changed file in the tree to open its diff; **± All changes** shows every one.',
  '**Desktop Notifications** in Settings tell you when an agent needs you, with this page in the background.',
];

let index = 0;

export function initWelcomeTips() {
  const saved = Number(localStorage.getItem(KEY) ?? NaN);
  index = Number.isInteger(saved) ? (saved + 1) % TIPS.length : 0;
  localStorage.setItem(KEY, String(index));
  $('#o-empty-tip').addEventListener('click', e => {
    const step = Number(e.target.closest('[data-tip]')?.dataset.tip);
    if (!step) return;
    index = (index + step + TIPS.length) % TIPS.length;
    localStorage.setItem(KEY, String(index));
    render();
  });
  on('setting:changed', key => key === 'tips.enabled' && render());
  render();
}

function render() {
  const box = $('#o-empty-tip');
  box.hidden = getSetting('tips.enabled') === false;
  if (box.hidden) return;
  $('#o-empty-tip-n').textContent = `${index + 1}/${TIPS.length}`;
  $('#o-empty-tip-text').innerHTML = esc(TIPS[index])
    .replace(/\{([^}]+)\}/g, (_, combo) => keyCaps(combo))
    .replace(/\*\*(.+?)\*\*/g, '<b>$1</b>');
}

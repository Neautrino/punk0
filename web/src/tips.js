// web/src/tips.js
// Tips, the same on both pages: one list (tipsdata.js), one switch
// (tips.enabled in settings.json) and one rotation. A tip shows only where the
// reader is idle or looking for something: the agents page's welcome view,
// the editor with no file open, the command palette's footer. Help › Tips
// lists them all. Nothing pops up, runs on a timer or covers code or a
// terminal.
//
// Rotation: punk0.tips.seen in localStorage holds the ids already shown. The
// editor is served under the agents page's origin, so both pages share it: a
// tip seen on one is not shown again on the other. A tip counts as seen once
// shown; when a page has none left unseen, its ids are dropped and the round
// starts over.
import { $, esc, keyCaps, isMac } from './state.js';
import { on } from './bus.js';
import { showToast } from './ui.js';
import { getSetting, saveSetting, settingsLoaded } from './settings-form.js';
import { TIPS } from './tipsdata.js';

const SEEN = 'punk0.tips.seen';
const PAGE_NAMES = { agents: 'Agents page', editor: 'Editor' };

export const tipsOn = () => getSetting('tips.enabled') !== false;

const forPage = (tip, page) => tip.page === page || tip.page === 'both';

// tipsFor is what page shows now: its tips and both pages', less those for
// another system and those whose setting no longer has the value they teach.
export function tipsFor(page) {
  return TIPS.filter(t => forPage(t, page)
    && (!t.os || (t.os === 'mac') === isMac)
    && (!t.only || getSetting(t.only[0]) === t.only[1]));
}

function readSeen() {
  try {
    const ids = JSON.parse(localStorage.getItem(SEEN) || '[]');
    return Array.isArray(ids) ? ids : [];
  } catch {
    return [];
  }
}

// writeSeen keeps only ids still in the list, so retired tips do not pile up.
function writeSeen(ids) {
  const known = new Set(TIPS.map(t => t.id));
  try { localStorage.setItem(SEEN, JSON.stringify([...new Set(ids)].filter(id => known.has(id)))); } catch {}
}

function markSeen(id) {
  writeSeen([...readSeen(), id]);
}

// pickTip is the next tip for page, marked seen; null when it has none.
export function pickTip(page) {
  const list = tipsFor(page);
  if (!list.length) return null;
  let seen = readSeen();
  let tip = list.find(t => !seen.includes(t.id));
  if (!tip) {
    const mine = new Set(list.map(t => t.id));
    seen = seen.filter(id => !mine.has(id));
    tip = list[0];
  }
  writeSeen([...seen, tip.id]);
  return tip;
}

// tipHTML draws a tip's text: escaped, {combo} as key caps, **x** in bold.
export function tipHTML(text) {
  return esc(text)
    .replace(/\{([^}]+)\}/g, (_, combo) => keyCaps(combo))
    .replace(/\*\*(.+?)\*\*/g, '<b>$1</b>');
}

async function turnTipsOff() {
  if (await saveSetting('tips.enabled', false)) {
    showToast('i', 'Tips are off. Turn them on again in Help › Tips or Settings.', 4000);
  }
}

// mountTipBox makes el (a .tipbox in an empty view) show page's next tip,
// with ‹ › to step through the page's tips and × to turn tips off. The tip
// is picked, and so marked seen, the first time the box is on screen with
// tips on: a view the page never shows uses none up. It follows the switch
// while the page is open.
export function mountTipBox(el, page) {
  let list = [];
  let at = 0;

  // Until a tip is picked the box stays empty (.tipbox:empty takes no
  // space on screen but is still laid out, so the observer sees it).
  const draw = () => {
    el.hidden = !tipsOn();
    if (el.hidden || !list.length) return;
    el.innerHTML = '<div class="tipbox-head"><span class="tipbox-tag">Tip</span>' +
      '<button type="button" data-step="-1" title="Previous tip" aria-label="Previous tip">‹</button>' +
      `<span class="tipbox-n">${at + 1}/${list.length}</span>` +
      '<button type="button" data-step="1" title="Next tip" aria-label="Next tip">›</button>' +
      '<button type="button" data-off title="Turn tips off" aria-label="Turn tips off">×</button></div>' +
      `<div class="tipbox-text">${tipHTML(list[at].text)}</div>`;
  };

  const seen = new IntersectionObserver(entries => {
    if (list.length || !tipsOn() || !entries.some(e => e.isIntersecting)) return;
    seen.disconnect();
    list = tipsFor(page);
    at = Math.max(0, list.indexOf(pickTip(page)));
    draw();
  });

  el.addEventListener('click', e => {
    const btn = e.target.closest('button');
    if (!btn) return;
    if ('off' in btn.dataset) return void turnTipsOff();
    at = (at + Number(btn.dataset.step) + list.length) % list.length;
    markSeen(list[at].id);
    draw();
  });
  settingsLoaded.then(() => {
    draw();
    seen.observe(el);
    on('setting:changed', key => { if (key === 'tips.enabled') draw(); });
  });
}

// tipLine is one tip for a single line (the palette's footer), or '' while
// tips are off.
export function tipLine(page) {
  const tip = tipsOn() ? pickTip(page) : null;
  return tip ? `<span class="tipbox-tag">Tip</span>${tipHTML(tip.text)}` : '';
}

const plain = text => text.replace(/\*\*|[{}]/g, '').toLowerCase();

// initTipsHelp fills section (Help's Tips tab) with every tip, this page's
// first, a filter and the Show tips switch. refresh() redraws it as the tab
// comes to the front; focus() puts the cursor in the filter.
export function initTipsHelp(section, page) {
  const other = page === 'agents' ? 'editor' : 'agents';
  section.innerHTML = '<div class="tips-header-bar">' +
    '<div class="tips-search-wrap"><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>' +
    '<input type="text" class="tips-filter" placeholder="Filter tips" aria-label="Filter tips" autocomplete="off" spellcheck="false"></div>' +
    '<label class="tips-setting-toggle" title="Show a tip in empty views and the command palette"><input type="checkbox" class="tips-on"><span>Show tips</span></label>' +
    '</div><div class="tips-list"></div>';
  const filter = $('.tips-filter', section);
  const toggle = $('.tips-on', section);
  const listEl = $('.tips-list', section);

  const group = (title, tips) => tips.length
    ? `<section class="tips-group"><h3>${esc(title)}<span>${tips.length}</span></h3><ul>` +
      tips.map(t => `<li>${tipHTML(t.text)}</li>`).join('') + '</ul></section>'
    : '';

  function refresh() {
    toggle.checked = tipsOn();
    const q = filter.value.trim().toLowerCase();
    const match = t => !q || plain(t.text).includes(q);
    const here = TIPS.filter(t => forPage(t, page) && match(t));
    const there = TIPS.filter(t => t.page === other && match(t));
    listEl.innerHTML = group('This page', here) + group(PAGE_NAMES[other], there) ||
      `<p class="tips-none">No tip mentions “${esc(filter.value.trim())}”.</p>`;
  }

  filter.addEventListener('input', refresh);
  toggle.addEventListener('change', async () => {
    if (!await saveSetting('tips.enabled', toggle.checked)) toggle.checked = tipsOn();
  });
  on('setting:changed', key => { if (key === 'tips.enabled') toggle.checked = tipsOn(); });
  refresh();
  return { refresh, focus: () => filter.focus() };
}

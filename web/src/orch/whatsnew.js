// web/src/orch/whatsnew.js
// The What's New tab: the release notes of one punk0 version from GET
// api/wb/whats-new (rendered by the server with raw HTML dropped), a list of
// its sections and each entry's bold title that follows the reading, and
// links to the release and the changelog. It opens by itself once after a
// version change (/api/wb/state whatsNew): the first page to open it tells
// the server, so other pages and the next start do not open it again. The
// update card (update.js) and the help sheet open it too.
import { $, esc } from '../state.js';
import { call } from './api.js';
import { state, subscribe, openWhatsNew } from './store.js';

let vp, titleEl, metaEl;
const loaded = new Map(); // version -> answer with notes; a failure is asked again
let shown = ''; // version drawn in vp
let spy = null; // marks the list's link of where the reading is; null when nothing is drawn
let frame = 0;
let claimed = '';

export function initWhatsNew() {
  vp = $('#o-notes-vp');
  titleEl = $('#o-file-title');
  metaEl = $('#o-file-meta');
  vp.addEventListener('click', e => {
    const id = e.target.closest('.wn-nav a')?.dataset.to;
    if (!id) return;
    e.preventDefault();
    vp.querySelector(`#${CSS.escape(id)}`)?.scrollIntoView({ block: 'start' });
  });
  vp.addEventListener('scroll', () => {
    if (spy && !frame) frame = requestAnimationFrame(() => { frame = 0; spy?.(); });
  }, { passive: true });
  subscribe(autoOpen);
}

function autoOpen() {
  const v = state.wb.whatsNew;
  if (!v || claimed === v) return;
  claimed = v;
  call('POST', 'api/wb/whats-new/seen');
  openWhatsNew(v);
}

// showWhatsNew draws version ver's notes; the path row names them.
export async function showWhatsNew(ver) {
  titleEl.textContent = `What's New · v${ver}`;
  metaEl.textContent = '';
  if (shown === ver) return;
  shown = ver;
  let res = loaded.get(ver);
  if (!res) {
    vp.innerHTML = '<p class="o-note">Loading the release notes…</p>';
    const r = await call('GET', `api/wb/whats-new?v=${encodeURIComponent(ver)}`);
    res = r.ok ? r.data : { version: ver, html: '' };
    if (res.html) loaded.set(ver, res);
  }
  if (shown === ver) draw(res);
}

export function hideWhatsNew() {
  shown = '';
  spy = null;
}

function draw({ version, html, release, changelog }) {
  spy = null;
  const links = [release && `<a href="${esc(release)}" target="_blank" rel="noopener">GitHub release</a>`,
    changelog && `<a href="${esc(changelog)}" target="_blank" rel="noopener">Changelog</a>`].filter(Boolean).join(' · ');
  vp.innerHTML = `<div class="wn"><nav class="wn-nav" aria-label="On this page"><p>On this page</p><ul></ul></nav>
    <article class="wn-body"><h1>What's new in punk0 ${esc(version)}</h1><p class="wn-links">${links}</p>
    <div class="wn-notes"></div></article></div>`;
  const notes = vp.querySelector('.wn-notes');
  if (!html) {
    vp.querySelector('.wn').classList.add('wn-bare');
    vp.querySelector('.wn-nav').remove();
    notes.innerHTML = `<p>Couldn't load the notes for v${esc(version)}.</p>`;
    return;
  }
  notes.innerHTML = html;
  for (const a of notes.querySelectorAll('a[href]')) {
    a.target = '_blank';
    a.rel = 'noopener';
  }
  vp.scrollTop = 0;
  spyOn(contents(notes));
}

// contents lists the notes' sections (h2, h3) and, under each, the entries
// that lead with a bold title, giving entries ids to jump to.
function contents(notes) {
  const nav = vp.querySelector('.wn-nav ul');
  const targets = [];
  let sub = null;
  let n = 0;
  for (const el of notes.querySelectorAll('h2, h3, li')) {
    if (el.tagName !== 'LI') {
      if (!el.id) el.id = `wn-s${n++}`;
      const li = document.createElement('li');
      li.innerHTML = `<a href="#" data-to="${esc(el.id)}">${esc(el.textContent.trim())}</a><ul></ul>`;
      nav.append(li);
      sub = li.querySelector('ul');
      targets.push(el);
      continue;
    }
    if (!sub || el.parentElement.closest('li')) continue;
    const lead = leadingBold(el);
    if (!lead) continue;
    el.id = `wn-e${n++}`;
    const li = document.createElement('li');
    const text = lead.textContent.trim().replace(/[.:]$/, '');
    li.innerHTML = `<a href="#" data-to="${el.id}" title="${esc(text)}">${esc(text)}</a>`;
    sub.append(li);
    targets.push(el);
  }
  for (const ul of nav.querySelectorAll('ul:empty')) ul.remove();
  if (!targets.length) {
    vp.querySelector('.wn').classList.add('wn-bare');
    vp.querySelector('.wn-nav').remove();
  }
  return targets;
}

// leadingBold is the <strong> an entry starts with (in a loose list, its
// first paragraph does), or null: "The **+** menu…" has no title.
function leadingBold(li) {
  const box = li.firstElementChild?.tagName === 'P' ? li.firstElementChild : li;
  const first = [...box.childNodes].find(n => n.nodeType !== Node.TEXT_NODE || n.textContent.trim());
  return first?.nodeName === 'STRONG' ? first : null;
}

// spyOn marks the list's link of the last section or entry whose top has
// scrolled up to the top of the tab, or, scrolled to the bottom, the last
// one, since the last entries may never get that high.
function spyOn(targets) {
  if (!targets.length) return;
  const links = new Map(targets.map(t => [t, vp.querySelector(`.wn-nav a[data-to="${CSS.escape(t.id)}"]`)]));
  spy = () => {
    const line = vp.getBoundingClientRect().top + 48;
    let at = targets[0];
    for (const t of targets) if (t.getBoundingClientRect().top <= line) at = t;
    if (vp.scrollTop > 0 && vp.scrollTop + vp.clientHeight >= vp.scrollHeight - 2) at = targets[targets.length - 1];
    for (const [t, a] of links) a?.toggleAttribute('aria-current', t === at);
  };
  spy();
}

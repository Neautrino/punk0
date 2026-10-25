// web/src/chatlist.js
// One chat of the coding agents' history (history.go's GET api/history), as
// both pages list it: the agents page's Chats mode (orch/history.js) and the
// editor's (chats.js). A row is one button -- dot (filled while a tab has
// the chat open), title, agent, age, chevron -- that expands the chat's
// preview: how it started and its last exchange, read on demand from
// GET api/history/{agent}/{id}/preview, with the page's buttons under it.
// Everything read from a transcript is escaped: it is an agent's or the
// user's own text, never markup.
import { esc } from './state.js';

export const AGENT_NAMES = { claude: 'Claude Code', codex: 'Codex', opencode: 'OpenCode' };

export const agentName = a => AGENT_NAMES[a] || a;

export const ICON_CHEV = '<svg class="chat-chev" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6"/></svg>';

export const chatKey = r => `${r.agent}:${r.id}`;

// ago is how long since a chat was touched, as short as the row needs.
export function ago(ms) {
  if (!ms) return '';
  const s = Math.max(0, (Date.now() - ms) / 1000);
  if (s < 60) return 'now';
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  const d = Math.floor(s / 86400);
  if (d < 7) return `${d}d`;
  if (d < 365) return `${Math.floor(d / 7)}w`;
  return `${Math.floor(d / 365)}y`;
}

// chatRow is one chat. shown: its preview is the expanded one, drawn from
// preview ({loading} | {error} | {data}); where: the folder line of the
// title's tooltip; acts: buttons over the row's end (the agents page's
// trash); prevActs: the preview's buttons; dir(d): how a folder is written
// among the preview's facts ('' leaves it out).
export function chatRow(r, { shown, preview, where = '', acts = '', prevActs = '', dir = (/** @type {string} */ _d) => '' }) {
  const cls = ['chat-row', r.open ? 'open' : '', r.removed ? 'removed' : '', r.missing ? 'missing' : '', shown ? 'shown' : ''].filter(Boolean).join(' ');
  return `
    <div class="${cls}" data-key="${esc(chatKey(r))}">
      <div class="chat-line">
        <button type="button" class="chat-open" data-act="row" aria-expanded="${shown}" title="${esc(r.title)}${where ? '\n' + esc(where) : ''}">
          <span class="chat-dot" aria-label="${r.open ? 'Open in a tab' : 'Not open'}"></span>
          <span class="chat-title">${esc(r.title)}</span>
          <span class="chat-agent">${esc(agentName(r.agent))}${r.open ? ' · open' : ''}</span>
          <span class="chat-age">${esc(ago(r.updated))}</span>
          ${ICON_CHEV}
        </button>
        ${acts ? `<span class="chat-acts">${acts}</span>` : ''}
      </div>
      ${shown ? previewHTML(r, preview, prevActs, dir) : ''}
    </div>`;
}

function previewHTML(r, p, prevActs, dir) {
  let body = '<p class="chat-prev-note">Reading the conversation…</p>';
  if (p?.error) body = `<p class="chat-prev-note">${esc(p.error)}</p>`;
  else if (p?.data) {
    const d = p.data;
    // A detached head names no branch: punk0 never sends HEAD, and this keeps it so.
    const branch = d.branch && d.branch !== 'HEAD' ? d.branch : '';
    const facts = [d.messages ? `${d.messages} msg${d.messages === 1 ? '' : 's'}` : '', dir(d.dir || r.dir), branch].filter(Boolean);
    body = `
      ${facts.length ? `<p class="chat-facts">${facts.map(esc).join(' · ')}</p>` : ''}
      ${d.lastPrompt ? `<h4 class="chat-prev-h">Last exchange</h4>
        <p class="chat-prompt"><span class="chat-role">You</span>${esc(d.lastPrompt)}</p>
        ${d.lastReply ? `<p class="chat-reply"><span class="chat-role">${esc(agentName(r.agent))}</span>${esc(d.lastReply)}</p>` : ''}` : ''}
      ${d.started ? `<h4 class="chat-prev-h">Started with</h4><p class="chat-prompt chat-started">${esc(d.started)}</p>` : ''}
      ${!d.lastPrompt ? '<p class="chat-prev-note">Nothing was said in this chat yet.</p>' : ''}`;
  }
  return `
      <div class="chat-prev">
        ${body}
        <div class="chat-prev-acts">${prevActs}</div>
      </div>`;
}

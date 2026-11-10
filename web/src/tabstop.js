// web/src/tabstop.js
// Stopping a terminal tab by hand (POST api/tabs/{id}/stop, workbench.go),
// from the agents page's tab menu and the resource panel on both pages. The
// tab stays: opening it again starts it, an agent picking its chat up where it
// can. A stop that would lose something (a command running in a shell, an
// agent's turn or the answer it waits for, a chat its agent cannot resume)
// comes back 409 with what, and is repeated with force only once the user
// agrees.
import { showToast } from './ui.js';

// stopTab stops tab {id, name} through the workbench whose API paths resolve
// against base, and reports whether it stopped.
/** @param {string} base @param {{ id: string, name: string }} tab */
export async function stopTab(base, tab) {
  /** @param {boolean} force */
  const url = force => new URL(`api/tabs/${encodeURIComponent(tab.id)}/stop${force ? '?force=1' : ''}`, base);
  let res = await post(url(false));
  if (res.status === 409 && res.data?.confirm) {
    if (!confirm(`Stop "${tab.name}"? ${res.data.error}`)) return false;
    res = await post(url(true));
  }
  if (!res.ok) {
    showToast('!', res.data?.error || 'Could not stop the tab', 4000);
    return false;
  }
  return true;
}

/** @param {URL} url */
async function post(url) {
  try {
    const r = await fetch(url, { method: 'POST' });
    return { ok: r.ok, status: r.status, data: await r.json().catch(() => null) };
  } catch (e) {
    return { ok: false, status: 0, data: { error: e.message } };
  }
}

// web/src/orch/api.js
// Requests from the orchestrator page. Every path is relative to the page's
// <base href>, which handleIndex sets to the parent's base path, so the page
// works under any -base-path.
import { showToast } from '../ui.js';

export const pageURL = path => new URL(path, document.baseURI).href;

// wsURL is pageURL with the websocket scheme matching the page's.
export function wsURL(path) {
  const u = new URL(path, document.baseURI);
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
  return u.href;
}

// call sends a JSON request and resolves to { ok, status, data }; it never
// throws. On failure data holds the server's { error }, or one made from the
// network error or a non-JSON body.
export async function call(method, path, body) {
  let res;
  try {
    res = await fetch(pageURL(path), {
      method,
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (e) {
    return { ok: false, status: 0, data: { error: e.message } };
  }
  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = { error: text.trim() || res.statusText };
  }
  if (!res.ok && !data?.error) data = { ...data, error: `${res.status} ${res.statusText}` };
  return { ok: res.ok, status: res.status, data };
}

export const failed = res => showToast('!', res.data?.error || 'Request failed', 4000);

// dirID is the folder backend id in a workspace href ("w/<id>/"); editor
// windows are named after it, so opening a folder twice reuses its window.
export const dirID = href => (href.match(/w\/([^/]+)\/?$/) || [])[1] || href;

export function openEditor(href) {
  window.open(pageURL(href), 'punk0-w-' + dirID(href));
}

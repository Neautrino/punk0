// web/src/orch/removedialog.js
// The confirmation behind ✕ on a project or workspace row and behind the
// trash on a Chats row: a flyout with a checkbox for everything that can go
// with the record (its folder, or the conversation itself) and, for a project
// folder, its name typed back. The button follows the checkboxes: `go.off`
// while nothing is ticked, `go.on` in danger red once something is. It only
// collects answers; the callers know what they mean and send the request.
import { $, esc } from '../state.js';
import { placeFlyout, placeFlyoutAt, anchorRect } from './flyout.js';

let form, headingEl, whatEl, optsEl, confirmField, confirmName, confirmEl, goEl;
let done = null;   // resolves the open dialog's promise
let anchor = null; // where the button that opened it was; the notch points there
let want = '';     // the name that must be typed back, '' when none is asked for
let go = { off: '', on: '' }; // the button's two labels
let danger = true; // red whatever is ticked (the sidebar's deletes)
let beside = false; // place beside the button itself, not the sidebar
let opts = [];     // the options as the caller described them, for their notes

export function initRemoveDialog() {
  form = $('#o-rm-form');
  headingEl = $('#o-rm-heading');
  whatEl = $('#o-rm-what');
  optsEl = $('#o-rm-opts');
  confirmField = $('#o-rm-confirm-field');
  confirmName = $('#o-rm-confirm-name');
  confirmEl = $('#o-rm-confirm');
  goEl = $('#o-rm-go');

  form.addEventListener('submit', e => {
    e.preventDefault();
    if (!goEl.disabled) close({ ...ticked(), confirm: confirmEl.value.trim() });
  });
  $('#o-rm-cancel').addEventListener('click', () => close(null));
  $('#o-rm-close').addEventListener('click', () => close(null));
  // A click outside cancels, as it does on the other flyouts.
  document.addEventListener('mousedown', e => {
    if (done && !form.contains(e.target)) close(null);
  });
  form.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    close(null);
  });
  addEventListener('resize', () => done && place());
  // A note or the name field changes its height: stay in the window.
  new ResizeObserver(() => done && place()).observe(form);
  optsEl.addEventListener('change', sync);
  confirmEl.addEventListener('input', sync);
}

// openRemoveDialog shows d pointing at the button in from, and resolves to
// { <option id>: bool, …, confirm } or null when cancelled.
// d: { heading, go: {off, on}, what (HTML: escape anything the user or an
// agent wrote), options: [{ id, label, note, noteOn: what the tick itself
// does, said only while it is ticked, checked }], confirm: the name to type
// back or '', danger: red even with nothing ticked, beside: place beside the
// button rather than the sidebar }.
export function openRemoveDialog(d, from) {
  close(null);
  anchor = anchorRect(from); // kept: the list re-renders under the flyout
  headingEl.textContent = d.heading;
  go = d.go;
  danger = d.danger !== false;
  beside = !!d.beside;
  whatEl.innerHTML = d.what;
  opts = d.options;
  optsEl.innerHTML = d.options.map(o => `
    <label class="o-rm-opt">
      <input type="checkbox" data-opt="${esc(o.id)}"${o.checked ? ' checked' : ''}>
      <span>${esc(o.label)}</span>
      <small>${esc(o.note)}</small>
    </label>`).join('');
  want = d.confirm || '';
  confirmName.textContent = want;
  confirmName.title = want; // read whole when a long name is cut
  confirmEl.value = '';
  form.hidden = false;
  sync();
  place();
  goEl.focus();
  return new Promise(resolve => { done = resolve; });
}

// closeRemoveDialog cancels an open dialog, as switching sidebar views does.
export const closeRemoveDialog = () => close(null);

function close(answers) {
  if (!done) return;
  form.hidden = true;
  done(answers);
  done = null;
}

const place = () => (beside ? placeFlyoutAt : placeFlyout)(form, anchor);

// ticked is the state of the checkboxes, keyed by their option id.
function ticked() {
  const out = {};
  for (const el of optsEl.querySelectorAll('[data-opt]')) out[el.dataset.opt] = el.checked;
  return out;
}

// sync keeps the button saying what it will do: the harmless label while
// nothing is ticked, the red one once something is, and each option's note
// saying what its own tick does only while it is ticked. It asks for the name
// only while the folder is actually going, and stays off until it is typed
// exactly.
function sync() {
  const on = Object.values(ticked()).some(Boolean);
  goEl.textContent = on ? go.on : go.off;
  goEl.classList.toggle('o-rm-danger', danger || on);
  for (const el of optsEl.querySelectorAll('[data-opt]')) {
    const o = opts.find(x => x.id === el.dataset.opt);
    if (!o?.noteOn) continue;
    el.parentElement.querySelector('small').textContent = el.checked ? `${o.note} ${o.noteOn}` : o.note;
  }
  const files = !!ticked().files && !!want;
  confirmField.hidden = !files;
  goEl.disabled = files && confirmEl.value.trim() !== want;
}

// web/src/agent.js
import { S, api, apiPost } from './state.js';
import { showToast } from './ui.js';
import { openFile, reloadOpenTabs } from './tabs.js';
import { refreshTree } from './tree.js';
import { emit } from './bus.js';

/* The coding harness selection, shared by every panel that dispatches work to
   a harness (git sync, merge message, commit message, PR batch apply).
   Harnesses are detected, not configured: the pickers list what is installed
   and the choice is remembered in the settings file. Detecting one is never
   enough to run it, so nothing runs until one is picked. */

const chosen = () => (S.meta && S.meta.agent) || '';
const chosenModel = () => (S.meta && S.meta.agentModel) || '';

// Harness/model selects owned by other panels. Each shows the same global
// selection and writes it back.
const pickers = new Set();
export function registerAgentPicker(picker) {
  pickers.add(picker);
  picker.harnessSelect.addEventListener('change', () => {
    if (picker.harnessSelect.value) select(picker.harnessSelect.value, msg => showToast('!', msg));
  });
  picker.modelSelect.addEventListener('change', () => {
    select(picker.harnessSelect.value || chosen(), picker.modelSelect.value, msg => showToast('!', msg));
  });
  updatePicker(picker);
}

export function applyAgentMeta() {
  for (const picker of pickers) updatePicker(picker);
  emit('agent:meta'); // the git panel's AI actions depend on there being a harness
}

// fillHarnessOptions lists the installed harnesses in sel. With none picked
// yet it leads with a disabled "pick a harness" entry: otherwise the first
// harness would look chosen while the server has none (every AI action is then
// refused), and choosing that same displayed entry fires no change event.
function fillHarnessOptions(sel, ready, current) {
  if (!current) {
    const opt = document.createElement('option');
    opt.value = '';
    opt.textContent = 'pick a harness';
    opt.disabled = true;
    opt.selected = true;
    sel.appendChild(opt);
  }
  for (const h of ready) {
    const opt = document.createElement('option');
    opt.value = h.name;
    opt.textContent = h.name;
    if (h.name === current) opt.selected = true;
    sel.appendChild(opt);
  }
}

function updatePicker(picker) {
  if (!picker.harnessSelect || !picker.modelSelect) return;
  const ready = (S.meta?.agents || []).filter(h => h.installed);
  const currentHarness = chosen();
  const currentModel = chosenModel();

  picker.harnessSelect.innerHTML = '';
  if (!ready.length) {
    const opt = document.createElement('option');
    opt.value = '';
    opt.textContent = 'no harness';
    picker.harnessSelect.appendChild(opt);
    picker.harnessSelect.disabled = true;
    picker.modelSelect.innerHTML = '';
    picker.modelSelect.hidden = true;
    return;
  }

  fillHarnessOptions(picker.harnessSelect, ready, currentHarness);
  const isBusy = picker.el.classList.contains('busy');
  picker.harnessSelect.disabled = isBusy || !!(S.meta && S.meta.agentPinned);
  picker.harnessSelect.title = S.meta && S.meta.agentPinned
    ? 'Fixed for this run by -agent'
    : 'Change the coding harness';

  // Models for the currently selected harness (none until one is picked)
  const activeH = ready.find(h => h.name === (picker.harnessSelect.value || currentHarness));
  picker.modelSelect.innerHTML = '';
  const models = activeH?.models || [];
  if (models.length > 0) {
    for (const m of models) {
      const opt = document.createElement('option');
      opt.value = m;
      opt.textContent = m;
      if (m === currentModel) opt.selected = true;
      picker.modelSelect.appendChild(opt);
    }
    picker.modelSelect.hidden = false;
    picker.modelSelect.disabled = isBusy;
    picker.modelSelect.title = 'Model for ' + activeH.name;
  } else {
    picker.modelSelect.hidden = true;
  }
}

// Loads harnesses and models asynchronously after the browser UI has loaded.
export async function loadAgentAsync() {
  try {
    const j = await api('/api/agent/harnesses');
    S.meta.agents = j.harnesses || [];
    S.meta.agent = j.selected || S.meta.agent || '';
    S.meta.agentModel = j.model || S.meta.agentModel || '';
    S.meta.agentPinned = !!j.pinned;
    applyAgentMeta();
  } catch {}
}

// Makes name the harness for every later run. Returns whether it took.
async function select(name, model, onError) {
  if (typeof model === 'function') {
    onError = model;
    model = '';
  }
  try {
    const params = { name };
    if (model) params.model = model;
    const j = await apiPost('/api/agent/select', params);
    S.meta.agent = j.selected || '';
    S.meta.agentModel = j.model || '';
    S.meta.agents = j.harnesses || S.meta.agents;
    S.meta.agentPinned = !!j.pinned;
  } catch (e) {
    if (onError) onError(e.message);
    return false;
  }
  applyAgentMeta();
  return true;
}

/* Reload in the order the data depends on: the index first, so the tree and
   git badges agree with disk, then the open tabs, which keep their scroll,
   cursor and view across the swap. Two harness runs can finish close together,
   so reloads are queued rather than left to interleave. Returns whether it worked. */
let reloadChain = Promise.resolve();
export function reloadWorkspace(focus, what = 'Changed') {
  const run = async () => {
    try {
      await api('/api/reindex');
      await reloadOpenTabs();
      if (focus?.path) {
        await openFile(focus.path, { line: focus.l1, push: false });
      }
      await refreshTree();
    } catch (e) {
      showToast('!', what + ', but the reload failed: ' + e.message);
      return false;
    }
    return true;
  };
  const result = reloadChain.then(run, run);
  reloadChain = result.then(() => {}, () => {});
  return result;
}

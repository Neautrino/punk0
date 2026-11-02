// web/src/selectbox.js
// Every <select> on a page shows as a button that opens punk0's list
// (listbox.js) instead of the browser's own popup, which cannot be themed.
// The <select> stays in the page, visually hidden, and keeps being the
// control: picking sets its value and fires input and change on it, and
// code that sets .value, swaps its options or disables it is followed. A
// <select multiple>, one with a size, or one marked data-native is left alone.
import { openListbox, closeListbox, listboxOpenFor, listboxKey } from './listbox.js';

const CHEVRON = '<svg class="lb-chevron" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg>';
const proto = HTMLSelectElement.prototype;
const VALUE = Object.getOwnPropertyDescriptor(proto, 'value');
const INDEX = Object.getOwnPropertyDescriptor(proto, 'selectedIndex');

// watchSelects turns every select under root, now and added later, into one.
export function watchSelects(root = document.body) {
  root.querySelectorAll('select').forEach(enhanceSelect);
  new MutationObserver(muts => {
    for (const m of muts) {
      for (const n of m.addedNodes) {
        if (!(n instanceof Element)) continue;
        if (n.matches('select')) enhanceSelect(n);
        else n.querySelectorAll('select').forEach(enhanceSelect);
      }
    }
  }).observe(root, { childList: true, subtree: true });
}

export function enhanceSelect(select) {
  if (select.lbButton || select.multiple || select.size > 1 || 'native' in select.dataset) return;
  const button = document.createElement('button');
  button.type = 'button';
  button.innerHTML = `<span class="lb-value"></span>${CHEVRON}`;
  button.setAttribute('role', 'combobox');
  button.setAttribute('aria-haspopup', 'listbox');
  button.setAttribute('aria-expanded', 'false');
  select.lbButton = button;
  select.classList.add('lb-native');
  select.tabIndex = -1;
  select.setAttribute('aria-hidden', 'true');
  select.after(button);

  const sync = () => {
    const opt = select.options[INDEX.get.call(select)];
    button.querySelector('.lb-value').textContent = opt ? opt.textContent : '';
    button.className = [...select.classList].filter(c => c !== 'lb-native').concat('lb-button').join(' ');
    button.disabled = select.disabled;
    button.hidden = select.hidden;
    if (select.title) button.title = select.title;
    else button.removeAttribute('title');
    const name = select.getAttribute('aria-label') || select.title || labelText(select);
    if (name) button.setAttribute('aria-label', name);
  };
  // Code that sets the value directly fires no event: follow it here.
  const follow = (name, d) => Object.defineProperty(select, name, {
    configurable: true,
    get() { return d.get.call(this); },
    set(v) { d.set.call(this, v); sync(); },
  });
  follow('value', VALUE);
  follow('selectedIndex', INDEX);
  new MutationObserver(sync).observe(select, {
    childList: true, subtree: true, characterData: true,
    attributes: true, attributeFilter: ['disabled', 'hidden', 'title', 'class', 'selected', 'aria-label'],
  });
  select.addEventListener('change', sync);
  // A label that wraps or names the select focuses it: hand that to the button.
  select.addEventListener('focus', () => button.focus());

  const open = () => {
    sync();
    const groups = [];
    let loose = null;
    for (const child of select.children) {
      if (child.tagName === 'OPTGROUP') {
        groups.push({ label: child.label, items: [...child.children].map(o => item(o, child.disabled)) });
        loose = null;
      } else if (child.tagName === 'OPTION') {
        if (!loose) groups.push(loose = { items: [] });
        loose.items.push(item(child, false));
      }
    }
    openListbox(button, {
      groups,
      value: VALUE.get.call(select),
      typeahead: true,
      empty: 'Nothing to choose',
      onPick: it => {
        if (it.value === VALUE.get.call(select)) return;
        VALUE.set.call(select, it.value);
        sync();
        select.dispatchEvent(new Event('input', { bubbles: true }));
        select.dispatchEvent(new Event('change', { bubbles: true }));
      },
    });
  };
  button.addEventListener('click', () => (listboxOpenFor(button) ? closeListbox() : open()));
  button.addEventListener('keydown', e => {
    if (listboxOpenFor(button)) {
      listboxKey(e);
      return;
    }
    if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(e.key) || (e.altKey && e.key === 'ArrowDown')) {
      e.preventDefault();
      open();
    }
  });
  button.addEventListener('blur', () => listboxOpenFor(button) && closeListbox());
  sync();
}

const item = (o, groupDisabled) => ({
  value: o.value,
  label: o.textContent,
  title: o.title || undefined,
  disabled: o.disabled || groupDisabled,
});

// labelText is the name a label gives select: its own text, without the
// text of the options inside it when it wraps the select.
function labelText(select) {
  const label = select.labels?.[0];
  if (!label) return '';
  return [...label.childNodes].filter(n => n.nodeType === 3).map(n => n.textContent).join(' ').trim();
}

// web/src/selpill.js
import { keyLabel } from './state.js';
import { copyToClipboard } from './ui.js';
import { sendToActiveTerminal } from './term_inspector.js';
import { openQuickEdit } from './quick_edit.js';

let pillEl = null;
let currentInfo = null;

export function calculatePillPosition(selectionRect, viewport, pillSize) {
  let left = selectionRect.left;
  // Clamp right edge so it doesn't overflow viewport
  if (left + pillSize.width > viewport.width - 10) {
    left = Math.max(10, viewport.width - pillSize.width - 10);
  }
  if (left < 10) left = 10;

  let top = selectionRect.top - pillSize.height - 8;
  // If too close to top edge, flip below selection
  if (top < 45) {
    top = selectionRect.bottom + 8;
  }

  return { left: Math.round(left), top: Math.round(top) };
}

export function showSelPill(info) {
  if (typeof window === 'undefined') return;
  if (!info || info.isMarkdownPreview || info.fromDiff) {
    hideSelPill();
    return;
  }
  currentInfo = info;

  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || !sel.rangeCount) {
    hideSelPill();
    return;
  }

  const range = sel.getRangeAt(0);
  const rect = range.getBoundingClientRect();
  if (!rect || rect.width === 0 || rect.height === 0) {
    hideSelPill();
    return;
  }

  if (!pillEl) initSelPill();
  if (!pillEl) return;

  const pillWidth = pillEl.offsetWidth || 310;
  const pillHeight = pillEl.offsetHeight || 32;

  const pos = calculatePillPosition(
    rect,
    { width: window.innerWidth, height: window.innerHeight },
    { width: pillWidth, height: pillHeight }
  );

  pillEl.style.left = `${pos.left}px`;
  pillEl.style.top = `${pos.top}px`;
  pillEl.hidden = false;
}

export function hideSelPill() {
  currentInfo = null;
  if (pillEl) {
    pillEl.hidden = true;
  }
}

export function initSelPill() {
  if (typeof document === 'undefined' || pillEl) return;
  pillEl = document.createElement('div');
  pillEl.id = 'floating-sel-pill';
  pillEl.className = 'floating-sel-pill';
  pillEl.hidden = true;

  // 1. Quick Edit
  const btnEdit = document.createElement('button');
  btnEdit.className = 'sel-pill-btn pill-btn-primary';
  btnEdit.title = `Quick Edit (${keyLabel('Mod+K')})`;
  btnEdit.innerHTML = `<span class="pill-sparkle">✦</span> Quick Edit <kbd>${keyLabel('Mod+K')}</kbd>`;
  btnEdit.onclick = e => {
    e.preventDefault();
    e.stopPropagation();
    const info = currentInfo;
    hideSelPill();
    if (info) openQuickEdit(info);
  };

  // 2. Add to Chat
  const btnChat = document.createElement('button');
  btnChat.className = 'sel-pill-btn';
  btnChat.title = `Add to Chat (${keyLabel('Alt+L')})`;
  btnChat.innerHTML = `💬 Chat <kbd>${keyLabel('Alt+L')}</kbd>`;
  btnChat.onclick = e => {
    e.preventDefault();
    e.stopPropagation();
    const info = currentInfo;
    hideSelPill();
    if (info) {
      const ext = info.path.split('.').pop() || '';
      const payload = `@${info.path}:${info.l1}-${info.l2}\n\`\`\`${ext}\n${info.text}\n\`\`\` `;
      sendToActiveTerminal(payload);
    }
  };

  // 3. Copy Ref
  const btnCopy = document.createElement('button');
  btnCopy.className = 'sel-pill-btn';
  btnCopy.title = `Copy Ref (${keyLabel('Alt+C')})`;
  btnCopy.innerHTML = `📋 Ref <kbd>${keyLabel('Alt+C')}</kbd>`;
  btnCopy.onclick = e => {
    e.preventDefault();
    e.stopPropagation();
    const info = currentInfo;
    hideSelPill();
    if (info) {
      const ref = `@${info.path}:${info.l1}${info.l1 === info.l2 ? '' : '-' + info.l2}`;
      copyToClipboard(ref, 'Copied ' + ref);
    }
  };

  pillEl.append(btnEdit, btnChat, btnCopy);
  document.body.appendChild(pillEl);
}

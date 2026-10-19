// Punk0 — Clean UI Interactive Logic
document.addEventListener('DOMContentLoaded', () => {
  initInstallCopy();
  initPreviewTabs();
  initSearchModal();
});

// 1. One-click Install Command Copy
function initInstallCopy() {
  const copyButtons = document.querySelectorAll('[data-copy]');

  copyButtons.forEach(btn => {
    btn.addEventListener('click', () => {
      const textToCopy = btn.getAttribute('data-copy') || 'curl -fsSL https://punk0.ai/install.sh | sh';
      navigator.clipboard.writeText(textToCopy).then(() => {
        const originalContent = btn.innerHTML;
        btn.classList.add('copied');
        btn.innerHTML = `
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="20 6 9 17 4 12"></polyline>
          </svg>
          <span>Copied</span>
        `;

        setTimeout(() => {
          btn.innerHTML = originalContent;
          btn.classList.remove('copied');
        }, 2000);
      });
    });
  });
}

// 2. Interactive Preview Window Tab Switcher
const previewPanes = {
  diff: `
<div class="diff-row"><span class="diff-ln">24</span><span class="diff-sign"> </span><span class="diff-code"><span class="syn-k">func</span> (r *<span class="syn-t">Router</span>) <span class="syn-f">Dispatch</span>(packet []<span class="syn-t">byte</span>) <span class="syn-t">error</span> {</span></div>
<div class="diff-row del"><span class="diff-ln">25</span><span class="diff-sign">-</span><span class="diff-code">    buf := make([]byte, len(packet))</span></div>
<div class="diff-row del"><span class="diff-ln">26</span><span class="diff-sign">-</span><span class="diff-code">    copy(buf, packet)</span></div>
<div class="diff-row add"><span class="diff-ln">25</span><span class="diff-sign">+</span><span class="diff-code">    <span class="syn-c">// Zero-copy slice borrowed from ring buffer</span></span></div>
<div class="diff-row add"><span class="diff-ln">26</span><span class="diff-sign">+</span><span class="diff-code">    buf := r.ringBuffer.BorrowSlice(packet)</span></div>
<div class="diff-row"><span class="diff-ln">27</span><span class="diff-sign"> </span><span class="diff-code">    <span class="syn-k">return</span> r.handler.<span class="syn-f">Handle</span>(buf)</span></div>
<div class="diff-row"><span class="diff-ln">28</span><span class="diff-sign"> </span><span class="diff-code">}</span></div>
`,
  engine: `
<div class="diff-row"><span class="diff-ln">1</span><span class="diff-sign"> </span><span class="diff-code"><span class="syn-k">package</span> core</span></div>
<div class="diff-row"><span class="diff-ln">2</span><span class="diff-sign"> </span><span class="diff-code"><span class="syn-k">type</span> <span class="syn-t">Engine</span> <span class="syn-k">struct</span> {</span></div>
<div class="diff-row"><span class="diff-ln">3</span><span class="diff-sign"> </span><span class="diff-code">    Workers  <span class="syn-t">int</span></span></div>
<div class="diff-row"><span class="diff-ln">4</span><span class="diff-sign"> </span><span class="diff-code">    ZeroCopy <span class="syn-t">bool</span></span></div>
<div class="diff-row"><span class="diff-ln">5</span><span class="diff-sign"> </span><span class="diff-code">}</span></div>
<div class="diff-row"><span class="diff-ln">6</span><span class="diff-sign"> </span><span class="diff-code"><span class="syn-k">func</span> <span class="syn-f">NewEngine</span>() *<span class="syn-t">Engine</span> {</span></div>
<div class="diff-row"><span class="diff-ln">7</span><span class="diff-sign"> </span><span class="diff-code">    <span class="syn-k">return</span> &amp;<span class="syn-t">Engine</span>{Workers: 64, ZeroCopy: <span class="syn-t">true</span>}</span></div>
<div class="diff-row"><span class="diff-ln">8</span><span class="diff-sign"> </span><span class="diff-code">}</span></div>
`,
  agent: `
<div class="diff-row"><span class="diff-ln">1</span><span class="diff-sign"> </span><span class="diff-code"><span class="syn-c">// Local AI Agent Handoff Session (Antigravity / Claude)</span></span></div>
<div class="diff-row"><span class="diff-ln">2</span><span class="diff-sign"> </span><span class="diff-code"><span class="syn-k">Session</span>:    #ag-8921-verified</span></div>
<div class="diff-row"><span class="diff-ln">3</span><span class="diff-sign"> </span><span class="diff-code"><span class="syn-k">Trigger</span>:    "Optimize packet handling with zero-copy buffer"</span></div>
<div class="diff-row"><span class="diff-ln">4</span><span class="diff-sign"> </span><span class="diff-code"><span class="syn-k">Status</span>:     <span style="color:#22c55e;">Ready to merge (2 files touched, 0 regressions)</span></span></div>
<div class="diff-row"><span class="diff-ln">5</span><span class="diff-sign"> </span><span class="diff-code"><span class="syn-k">Action</span>:     Press <kbd style="color:#fbbf24; background:rgba(255,255,255,0.06); padding:1px 5px;">Ctrl + Enter</kbd> to apply patch</span></div>
`
};

function initPreviewTabs() {
  const tabs = document.querySelectorAll('.tab-btn');
  const previewContent = document.getElementById('preview-code-body');
  const previewStatus = document.getElementById('preview-file-status');

  if (!previewContent) return;

  const statusLabels = {
    diff: 'core/router.go (2 modified)',
    engine: 'core/engine.go (clean)',
    agent: 'AI Handoff (1 patch ready)'
  };

  tabs.forEach(tab => {
    tab.addEventListener('click', () => {
      tabs.forEach(t => t.classList.remove('active'));
      tab.classList.add('active');

      const target = tab.getAttribute('data-tab');
      if (previewPanes[target]) {
        previewContent.innerHTML = previewPanes[target];
        if (previewStatus && statusLabels[target]) {
          previewStatus.textContent = statusLabels[target];
        }
      }
    });
  });
}

// 3. Search Command Palette Modal
const sampleFiles = [
  { name: 'router.go', path: 'core/router.go' },
  { name: 'engine.go', path: 'core/engine.go' },
  { name: 'client.rs', path: 'crates/harness/client.rs' },
  { name: 'config.toml', path: '.config/punk0.toml' },
  { name: 'sort.c', path: 'kernel/lib/sort.c' }
];

function initSearchModal() {
  const backdrop = document.getElementById('search-modal');
  const openTriggers = document.querySelectorAll('[data-open-search]');
  const input = document.getElementById('search-input');
  const list = document.getElementById('search-list');

  if (!backdrop || !input || !list) return;

  const render = (query = '') => {
    list.innerHTML = '';
    const q = query.toLowerCase().trim();
    const matches = sampleFiles.filter(f => f.name.toLowerCase().includes(q) || f.path.toLowerCase().includes(q));

    if (matches.length === 0) {
      list.innerHTML = `<div style="padding:12px 18px; color:var(--txt-tertiary); font-size:13px;">No files found</div>`;
      return;
    }

    matches.forEach((item, idx) => {
      const el = document.createElement('div');
      el.className = `modal-item ${idx === 0 ? 'active' : ''}`;
      el.innerHTML = `
        <span class="item-name">${item.name}</span>
        <span class="item-path">${item.path}</span>
      `;
      el.addEventListener('click', () => {
        closeModal();
      });
      list.appendChild(el);
    });
  };

  const openModal = () => {
    backdrop.classList.remove('hidden');
    input.value = '';
    render('');
    setTimeout(() => input.focus(), 30);
  };

  const closeModal = () => {
    backdrop.classList.add('hidden');
  };

  openTriggers.forEach(t => t.addEventListener('click', openModal));

  backdrop.addEventListener('click', (e) => {
    if (e.target === backdrop) closeModal();
  });

  input.addEventListener('input', (e) => render(e.target.value));

  window.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      backdrop.classList.contains('hidden') ? openModal() : closeModal();
    }
    if (e.key === 'Escape' && !backdrop.classList.contains('hidden')) {
      closeModal();
    }
  });
}

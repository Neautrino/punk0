// punk0 site: four independent units, each a no-op without its elements.
(() => {
  'use strict';

  // ---------------------------------------------------------------- themes
  // Names and swatch colours mirror web/themes/*.css (--theme-name, --bg, --accent).
  const THEMES = [
    ['catppuccin-latte', 'Catppuccin Latte', '#eff1f5', '#8839ef'],
    ['light', 'Paper', '#ffffff', '#0969da'],
    ['gruvbox-light', 'Gruvbox Light', '#fbf1c7', '#b57614'],
    ['solarized-light', 'Solarized Light', '#fdf6e3', '#268bd2'],
    ['catppuccin-mocha', 'Catppuccin Mocha', '#1e1e2e', '#cba6f7'],
    ['dark', 'Tokyo Night', '#1a1b26', '#7aa2f7'],
    ['dracula', 'Dracula', '#282a36', '#bd93f9'],
    ['github-dark', 'GitHub Dark', '#0d1117', '#1f6feb'],
    ['gruvbox-dark', 'Gruvbox Dark', '#282828', '#fabd2f'],
    ['monokai', 'Monokai', '#272822', '#a6e22e'],
    ['nord', 'Nord', '#2e3440', '#88c0d0'],
    ['one-dark', 'One Dark', '#282c34', '#61afef'],
    ['rose-pine', 'Rose Pine', '#191724', '#c4a7e7'],
    ['solarized-dark', 'Solarized Dark', '#002b36', '#268bd2'],
  ];
  const THEME_KEY = 'punk0-site.theme';
  const DEFAULT_THEME = 'catppuccin-latte';

  function readTheme() {
    try {
      const v = localStorage.getItem(THEME_KEY);
      return THEMES.some(t => t[0] === v) ? v : DEFAULT_THEME;
    } catch { return DEFAULT_THEME; }
  }

  function initThemePicker() {
    const box = document.querySelector('[data-swatches]');
    if (!box) return;
    const label = document.querySelector('[data-theme-name]');
    const apply = id => {
      document.querySelectorAll('.app').forEach(el => { el.dataset.theme = id; });
      box.querySelectorAll('.swatch').forEach(b => {
        const on = b.dataset.id === id;
        b.setAttribute('aria-checked', String(on));
        b.tabIndex = on ? 0 : -1;
      });
      if (label) label.textContent = THEMES.find(t => t[0] === id)[1];
    };
    for (const [id, name, bg, accent] of THEMES) {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'swatch';
      b.dataset.id = id;
      b.setAttribute('role', 'radio');
      b.setAttribute('aria-label', name);
      b.title = name;
      b.style.background = bg;
      b.style.setProperty('--sw-accent', accent);
      box.append(b);
    }
    const choose = id => {
      apply(id);
      try { localStorage.setItem(THEME_KEY, id); } catch { /* storage off: session only */ }
    };
    box.addEventListener('click', e => {
      const b = e.target.closest('.swatch');
      if (b) choose(b.dataset.id);
    });
    box.addEventListener('keydown', e => {
      const keys = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 };
      if (!(e.key in keys) && e.key !== 'Home' && e.key !== 'End') return;
      e.preventDefault();
      const all = [...box.querySelectorAll('.swatch')];
      const i = all.indexOf(document.activeElement);
      let n = e.key === 'Home' ? 0 : e.key === 'End' ? all.length - 1 : (i + keys[e.key] + all.length) % all.length;
      choose(all[n].dataset.id);
      all[n].focus();
    });
    apply(readTheme());
  }

  // ------------------------------------------------------------------ fit
  // Fixed-width product views scale as a whole; the inline script in the
  // page sets the first value before paint.
  function initFit() {
    const views = document.querySelectorAll('[data-fit]');
    if (!views.length) return;
    const fit = el => { el.style.zoom = String(Math.min(1, el.parentElement.clientWidth / +el.dataset.fit)); };
    const ro = new ResizeObserver(entries => entries.forEach(e => {
      const el = e.target.querySelector(':scope > [data-fit]');
      if (el) fit(el);
    }));
    views.forEach(el => { fit(el); ro.observe(el.parentElement); });
  }

  // ----------------------------------------------------------------- hero
  const PROMPT = 'Add a Len method to ring in ring.go that returns how many bytes it holds, and a test for it in ring_test.go.';
  const BANNER = [
    '<span class="ban"> ▐▛███▜▌</span>   <b>Claude Code</b>',
    '<span class="ban">▝▜█████▛▘</span>  <span class="dim">~/.punk0/worktrees/punk0/ring-len</span>',
    '<span class="ban">  ▘▘ ▝▝</span>',
    '',
  ];
  const esc = s => s.replace(/&/g, '&amp;').replace(/</g, '&lt;');
  const wrap = (s, n) => s.replace(new RegExp(`(.{1,${n}})(\\s+|$)`, 'g'), '$1\n').trimEnd().split('\n');

  // Transcript per state: what the Claude tab shows once that state is reached.
  // In state 5 the Changes column takes half the width, so the terminal is
  // narrower and Claude's output reflows, as it does in the real tab.
  function transcript(state, typed) {
    const cols = state === 5 ? 44 : 70;
    const lines = [...BANNER];
    const prompt = text => lines.push('<span class="box">' + wrap('❯ ' + text, cols).map(esc).join('\n  ') + '</span>');
    if (state <= 1) {
      prompt(typed);
      lines[lines.length - 1] = lines[lines.length - 1].replace('</span>', '<i class="caret"></i></span>');
      lines.push('<span class="dim">  ? for shortcuts</span>');
      return lines.join('\n');
    }
    lines.push(...wrap('> ' + PROMPT, cols).map(l => '<span class="dim">' + esc(l) + '</span>'), '');
    lines.push('● Read(ring.go)', '  <span class="dim">⎿  Read 36 lines</span>');
    lines.push('● Update(ring.go)', '  <span class="dim">⎿  Updated ring.go with 8 additions</span>');
    if (state === 3) {
      lines.push('● Update(ring_test.go)');
      lines.push('<span class="ask"> Do you want to make this edit to ring_test.go?\n <span class="hl">❯ 1. Yes</span>\n   2. Yes, allow all edits during this session\n   3. No, and tell Claude what to do differently</span>');
    }
    if (state >= 4) lines.push('● Update(ring_test.go)', '  <span class="dim">⎿  Updated ring_test.go with 4 additions</span>');
    if (state === 5) {
      lines.push('', ...wrap('● Added Len() to ring in ring.go and a TestRingLen test in ring_test.go.', cols).map((l, i) => (i ? '  ' : '') + esc(l)));
      lines.push('', '<span class="dim">✻ Baked for 11s</span>');
      prompt('');
    }
    return lines.join('\n');
  }

  function initHero() {
    const app = document.querySelector('[data-demo]');
    if (!app) return;
    const term = app.querySelector('[data-term]');
    const badge = app.querySelector('[data-badge]');
    const count = app.querySelector('[data-files-count]');
    const staged = app.querySelector('[data-branch-state]');
    const approve = document.querySelector('[data-approve]');
    const replay = document.querySelector('[data-replay]');
    const status = document.querySelector('[data-hero-status]');
    const idleText = status ? status.textContent : '';
    const reduce = matchMedia('(prefers-reduced-motion: reduce)');
    const BADGE = { 2: ['work', 'working'], 3: ['wait', 'needs you'], 4: ['work', 'working'], 5: ['idle', 'idle'] };

    let epoch = 0;
    let visible = false;
    let started = false;

    const show = (state, typed = '') => {
      const prev = +app.dataset.state;
      app.dataset.state = String(state);
      term.innerHTML = transcript(state, typed);
      const b = BADGE[state];
      badge.dataset.badge = b ? b[0] : '';
      badge.dataset.label = b ? b[1] : '';
      count.textContent = state >= 4 ? '2' : '1';
      staged.textContent = state >= 2 ? `0 / ${state >= 4 ? 2 : 1} staged` : 'no changes';
      if (prev !== state) {
        app.querySelectorAll(`[data-file="${state === 2 ? 'ring.go' : state === 4 ? 'ring_test.go' : ''}"]`).forEach(el => {
          el.classList.remove('new'); void el.offsetWidth; el.classList.add('new');
        });
      }
      const waiting = state === 3;
      if (approve) approve.hidden = !waiting;
      if (status) {
        status.classList.toggle('waiting', waiting);
        if (waiting) status.textContent = 'Claude is waiting for you.';
        else if (state === 5 && started) status.textContent = 'Done. The changes opened beside the terminal.';
        else if (state === 0) status.textContent = idleText;
      }
    };

    // sleep counts only time while the view is on screen, and rejects once a
    // newer run has started.
    const sleep = (ms, run) => new Promise((resolve, reject) => {
      let left = ms;
      let last = performance.now();
      const tick = now => {
        if (run !== epoch) return reject(run);
        if (visible) left -= now - last;
        last = now;
        if (left <= 0) resolve(); else requestAnimationFrame(tick);
      };
      requestAnimationFrame(tick);
    });

    const waitApprove = (ms, run) => new Promise((resolve, reject) => {
      const done = () => { approve.removeEventListener('click', done); resolve(); };
      approve.addEventListener('click', done);
      sleep(ms, run).then(done, err => { approve.removeEventListener('click', done); reject(err); });
    });

    async function play() {
      const run = ++epoch;
      started = true;
      if (replay) replay.hidden = false;
      try {
        show(0);
        await sleep(900, run);
        for (let i = 1; i <= PROMPT.length; i++) {
          show(1, PROMPT.slice(0, i));
          await sleep(18, run);
        }
        await sleep(500, run);
        show(2);
        await sleep(2600, run);
        show(3);
        await waitApprove(5000, run);
        show(4);
        await sleep(1500, run);
        show(5);
      } catch (e) {
        if (e !== run) throw e; // a newer run took over
      }
    }

    // Reduced motion: the finished state, with the caption describing the view.
    const finish = () => {
      epoch++;
      started = false;
      show(5);
      if (status) status.textContent = idleText;
      if (replay) replay.hidden = true;
    };

    if (reduce.matches) finish(); else show(0);
    reduce.addEventListener('change', () => { if (reduce.matches) finish(); });
    if (replay) replay.addEventListener('click', () => { if (!reduce.matches) play(); });

    new IntersectionObserver(entries => {
      visible = entries[0].isIntersecting;
      if (visible && !started && !reduce.matches) play();
    }, { threshold: 0.3 }).observe(app);
  }

  // ----------------------------------------------------------------- copy
  function initCopy() {
    document.querySelectorAll('[data-copy-from]').forEach(btn => {
      btn.addEventListener('click', async () => {
        const src = document.getElementById(btn.dataset.copyFrom);
        try {
          await navigator.clipboard.writeText(src.innerText.trim());
          btn.textContent = 'Copied';
          btn.dataset.state = 'done';
        } catch {
          const r = document.createRange();
          r.selectNodeContents(src);
          const s = getSelection(); s.removeAllRanges(); s.addRange(r);
          btn.textContent = 'Press Ctrl+C';
        }
        setTimeout(() => { btn.textContent = 'Copy'; delete btn.dataset.state; }, 1600);
      });
    });
  }

  initThemePicker();
  initFit();
  initHero();
  initCopy();
})();

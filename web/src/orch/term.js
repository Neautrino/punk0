// web/src/orch/term.js
// The agents page's terminal panes (termview.js), one per tab group: the
// tab's websocket is the page's own api, a key typed tells punk0 the tab was
// used, and file paths in the output open as file tabs of the selected
// workspace, at their line.
import { emit } from '../bus.js';
import { call, pageURL, wsURL } from './api.js';
import { workspace, openFile } from './store.js';
import { createTermView } from '../termview.js';

// fileLinks is filelinks.js's view of the selected workspace, whose tabs the
// panes show: a path in it opens as a file tab, at its line.
function fileLinks() {
  const w = workspace();
  if (!w || w.missing) return null;
  return {
    root: w.dir,
    list: dir => call('GET', `${w.href}api/tree?` + new URLSearchParams({ dir })).then(r => (r.ok ? r.data.children : null)),
    open: (path, line, col) => {
      openFile(path, line ? 'source' : undefined);
      if (line) emit('file:reveal', { path, line, col }); // the file's view (workbench.js) goes there
    },
  };
}

// newTermView binds a terminal pane in el. onChange runs when a tab
// connects or ends, so tab running state can be refreshed.
export function newTermView(el, onChange) {
  return createTermView(el, {
    vendorURL: pageURL,
    socketURL: wsURL,
    fileLinks,
    onChange,
    reportUse: true,
    pickOnResume: true,
  });
}

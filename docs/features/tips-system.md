# Tips

punk0 shows short tips about its features on both pages, the agents page and the editor. They come from one list that ships with punk0 (`web/src/tipsdata.js`): nothing is fetched, and every tip describes the version you run.

## Where tips show

A tip shows only where you are idle or looking for something. Nothing pops up, nothing changes on a timer, and nothing covers code or a terminal.

| Page | Where | When |
|---|---|---|
| Agents page | The welcome view, under its buttons | A workspace is picked or selected and has no tab open. One tip per page load. |
| Editor | Under the shortcut list shown with no file open | No file is open. One tip per page load. |
| Editor | The command palette's footer (`Mod+Shift+P`) | Each time the palette opens in command mode, a new tip, kept while you type. Other palette modes keep their usage hint. |
| Both | **Help › Tips** (the **?** button at the bottom left) | On demand: every tip of both pages, this page's first, with a filter. In the editor, the command palette's **Help: Tips** opens it too. |

A tip box has **‹** and **›** to step through the page's tips, its place in them (`3/25`), and **×**, which turns tips off.

## On and off

One setting decides it on both pages: **Show Tips** (`tips.enabled`, on by default) in Settings (`Mod+,`). **×** on a tip and the **Show tips** checkbox in Help › Tips change that same setting. The page you change it on follows at once; another open page follows when you switch to it, as it does for every setting. Help › Tips stays available with tips off.

## Which tip

- **Not seen yet first.** A tip counts as seen once it is shown. Both pages share the list of seen tips (`punk0.tips.seen` in the browser's storage, kept per address), so a tip for both pages seen on one is not shown again on the other. When a page has shown all its tips, it starts over.
- **Only what applies.** A tip about turning something on (Desktop Notifications, Vim keys) is left out once that setting is changed, and a tip about Windows/Linux keys is not shown on a Mac.
- An empty view that never comes on screen uses no tip up: the editor picks its tip only once its empty view is visible.

## Adding a tip

> [!IMPORTANT]
> A new user-facing feature, shortcut or command gets a tip.

Add an entry to `TIPS` in `web/src/tipsdata.js`:

```js
{ id: 'e-diff', page: 'editor', text: '{Mod+D} switches the open file between its source and its diff against HEAD.' },
```

- `id`: stable and unique; the rotation remembers tips by it.
- `page`: `'agents'`, `'editor'` or `'both'`.
- `text`: at most 160 characters, true of this version, with UI labels written as the UI shows them. `{Mod+K}` is a key combination, drawn as the reader's keyboard labels it (⌘K on a Mac, Ctrl+K elsewhere); `**Label**` is a UI label, in bold.
- `only: [key, value]` (optional): shown only while that setting has that value, which must be its default.
- `os: 'mac' | 'other'` (optional): shown only there.

Then run `bun scripts/check-web.js` (its tests check every tip's fields, length, markup and `only` setting) and `bun scripts/build-web.js`.

# Tips & Tricks System

punk0 includes an interactive tips and tricks system designed to introduce developers to powerful keyboard shortcuts, Git workflows, review capabilities, and editor features without obstructing startup.

---

## Overview & Architecture

Unlike heavy IDEs that display large intrusive modals on startup, punk0 loads tips asynchronously in the background.

- **Non-Intrusive Floating Widget**: Appears gently in the bottom-right corner after an initial delay (~1.8 seconds), presenting one tip at a time with previous/next navigation and automatic progression.
- **Tips Tab in the Help Card**: The help card (`#helpsheet`) has tabs **Shortcuts | Tips | Vim**. The Tips tab lists all tips in a two-column grid (`#tips-list`) with a filter (`#tips-filter`), a count, a reload button and a **Show tip popups** checkbox.
- **Left Bottom Bar Help Button**: The `#btn-help` button in the status bar's first section (`.status-side`, under the sidebar) opens the help card on its Tips tab.
- **Public URL Delivery**: Tips are loaded live from `https://px0.ai/tips.json` (px0's site; the list is not part of this repo) and cached locally, with built-in offline fallbacks (`web/src/tips-util.js`).
- **Permanent Dismissal**: Users can permanently hide the floating tip popups at any time via the "Don't show tips" button, the Tips tab's **Show tip popups** checkbox, or the `tips.enabled` preference in Settings (`Cmd/Ctrl+,`).

---

## How to Access Tips

1. **Floating Tip Widget**: Automatically appears shortly after opening punk0 unless permanently hidden. Use **‹** and **›** to browse tips one after another.
2. **Help Button**: Click the question mark icon in the left bottom bar (beside the theme and settings buttons) to open the help card on its Tips tab.
3. **Tip Widget**: Click **All Tips** on the floating tip widget.
4. **Command Palette**: Press **`Cmd/Ctrl + Shift + P`** and choose **Help: Tips & Tricks (All Tips)** or **Help: Show Tip**.
5. **Keyboard Shortcuts Sheet**: Press **`?`** to open the keyboard cheat sheet, then click the **Tips** tab.

---

## Keeping Tips Updated (Agent & Contributor Guidelines)

> [!IMPORTANT]
> Whenever rolling out a new user-facing feature, shortcut, command, or workflow improvement, add a corresponding tip entry so users can discover it.

### Tip Entry Schema

Each tip is a JSON object with the following fields:

```json
{
  "id": "feature-slug",
  "title": "Clear and Concise Title",
  "category": "Navigation | Git | AI | Editing | Viewer | Workbench | Performance",
  "tag": "ShortTag",
  "content": "Short markdown description of the feature. Supports **bold**, `code`, links, and <kbd> tags.",
  "shortcut": "Mod+Key" // optional keyboard shortcut combo
}
```

### Steps to Add or Update a Tip

1. **Public List**:
   The live list is served from px0's site (`https://px0.ai/tips.json`), outside this repo, so punk0 cannot publish tips there.

2. **Add to Built-in Fallbacks**:
   Add the identical entry to `DEFAULT_TIPS` in `web/src/tips-util.js` so the tip is available offline.

3. **Verify and Rebuild**:
   - Run `bun test web/src/` to run the frontend unit tests.
   - Run `bun ./scripts/check-web.js` and `bun ./scripts/build-web.js` to rebuild `web/app.js`.
   - Run `go test ./...` and `make build`.

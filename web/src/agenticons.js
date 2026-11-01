// web/src/agenticons.js
// The mark each tab shows for what runs in it, drawn in currentColor so the
// theme colours it. Marks are their owners' trademarks, shown only to say
// which agent runs. claude (the Claude Code mascot), opencode and
// cursor-agent come from simple-icons 16.36.0 (CC0); agy is Google's
// one-colour Antigravity icon from antigravity.google/press. OpenAI's marks
// are not licensed for use, so codex gets a neutral robot; shell is a prompt.

const ROBOT = '<svg viewBox="0 0 24 24" fill="currentColor" fill-rule="evenodd" aria-hidden="true"><path d="M6 7h12a3 3 0 0 1 3 3v7a3 3 0 0 1-3 3H6a3 3 0 0 1-3-3v-7a3 3 0 0 1 3-3ZM9 12a1.5 1.5 0 1 0 0 3a1.5 1.5 0 1 0 0-3ZM15 12a1.5 1.5 0 1 0 0 3a1.5 1.5 0 1 0 0-3ZM11.25 4.5h1.5V7h-1.5ZM12 1.5a1.5 1.5 0 1 0 0 3a1.5 1.5 0 1 0 0-3Z"/></svg>';

const ICONS = {
  claude: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M21 10.5h3v3h-3v3h-1.5v3H18v-3h-1.5v3H15v-3H9v3H7.5v-3H6v3H4.5v-3H3v-3H0v-3h3v-6h18Zm-15 0h1.5v-3H6Zm10.5 0H18v-3h-1.5z"/></svg>',
  codex: ROBOT,
  opencode: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M22 24H2V0h20zM17 4.8H7v14.4h10z"/></svg>',
  agy: '<svg viewBox="17 17 146 146" fill="currentColor" aria-hidden="true"><path d="M144.248 149.062C151.748 154.688 162.998 150.938 152.685 140.625C121.748 110.625 128.31 28.125 89.8727 28.125C51.4352 28.125 57.9977 110.625 27.0602 140.625C15.8102 151.875 27.9977 154.688 35.4977 149.062C64.5602 129.375 62.6852 94.6875 89.8727 94.6875C117.06 94.6875 115.185 129.375 144.248 149.062Z"/></svg>',
  'cursor-agent': '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M11.503.131 1.891 5.678a.84.84 0 0 0-.42.726v11.188c0 .3.162.575.42.724l9.609 5.55a1 1 0 0 0 .998 0l9.61-5.55a.84.84 0 0 0 .42-.724V6.404a.84.84 0 0 0-.42-.726L12.497.131a1.01 1.01 0 0 0-.996 0M2.657 6.338h18.55c.263 0 .43.287.297.515L12.23 22.918c-.062.107-.229.064-.229-.06V12.335a.59.59 0 0 0-.295-.51l-9.11-5.257c-.109-.063-.064-.23.061-.23"/></svg>',
  shell: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M5.88 5.12 12.77 12l-6.89 6.88-1.76-1.76L9.23 12 4.12 6.88ZM12 16.75h8v2.5h-8Z"/></svg>',
};

/** The mark for kind (an agent id or "shell") in a span of class cls; an
 *  agent punk0 has no mark for gets the robot. */
export function agentIcon(kind, cls) {
  return `<span class="${cls} agent-ic" data-agent="${kind === 'shell' ? 'shell' : 'agent'}">${ICONS[kind] || ROBOT}</span>`;
}

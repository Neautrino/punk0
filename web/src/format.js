// web/src/format.js
// Number formatting shared by the editor and the agents page, so it depends
// on nothing.

/** @param {number} n bytes */
export function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1048576).toFixed(1) + ' MB';
}

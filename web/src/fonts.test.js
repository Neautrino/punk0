import { test } from 'node:test';
import assert from 'node:assert';
import { termFontFamily, missingFont, families } from './fonts.js';

const MONO = '"JetBrains Mono", monospace';

test('no terminal font chosen draws with punk0\'s Nerd Font Mono, then the code font', () => {
  assert.equal(termFontFamily('', MONO), '"JetBrainsMono Nerd Font Mono", "JetBrains Mono", monospace');
  assert.equal(termFontFamily('  ', MONO), '"JetBrainsMono Nerd Font Mono", "JetBrains Mono", monospace');
});

test('a chosen font without icons keeps its glyphs and takes icons from the Nerd Font Mono', () => {
  assert.deepEqual(families(termFontFamily('"Fira Code"', MONO)), ['Fira Code', 'JetBrainsMono Nerd Font Mono', 'monospace']);
});

test('a chosen Nerd Font is used as written', () => {
  for (const f of ['"JetBrainsMono Nerd Font"', 'Hack Nerd Font Mono, monospace', '"FantasqueSansM NF"', '"Iosevka NFM"']) {
    assert.equal(termFontFamily(f, MONO), f);
  }
});

test('the first named font is reported when it is not installed', () => {
  const installed = new Set(['fira code', 'jetbrainsmono nerd font']);
  assert.equal(missingFont('"Fira Cod", "Fira Code"', installed), 'Fira Cod');
  assert.equal(missingFont('"Fira Code", "Nope"', installed), '', 'only the first name decides which font draws');
  assert.equal(missingFont("'FIRA CODE'", installed), '', 'names match case-insensitively');
  assert.equal(missingFont('"JetBrains Mono"', installed), '', 'punk0 serves it');
  assert.equal(missingFont('monospace', installed), '');
  assert.equal(missingFont('', installed), '');
});

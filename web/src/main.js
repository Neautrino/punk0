// web/src/main.js
import { initTerm } from './term.js';

const term = initTerm(document.getElementById('term'));
document.getElementById('btn-claude').addEventListener('click', () => term.run('claude'));

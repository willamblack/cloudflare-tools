import assert from 'node:assert/strict';
import test from 'node:test';

import { escapeHTML } from './utils.js';

test('escapeHTML neutralizes markup and attribute delimiters', () => {
  assert.equal(
    escapeHTML(`<img src=x onerror="alert('x')">&`),
    '&lt;img src=x onerror=&quot;alert(&#039;x&#039;)&quot;&gt;&amp;'
  );
});

// SPDX-License-Identifier: Apache-2.0
// Tokenizes a feature file with the TextMate grammar, using the same engine as VS Code
// (vscode-textmate with the Oniguruma WebAssembly build), and checks the scopes.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { before, test } from 'node:test';
import oniguruma from 'vscode-oniguruma';
import textmate from 'vscode-textmate';

const require = createRequire(import.meta.url);
const grammarPath = new URL('../syntaxes/gherkin.tmLanguage.json', import.meta.url);
const SCOPE = 'text.gherkin.feature';

const sample = `# language: en
@smoke @api:parcels # a comment after tags
Feature: Parcels API
  A description line with "quotes" and <angles>.

  Background:
    Given the parcels service with the following properties:
      | url | http://localhost:8400 |

  Rule: Parcels have names

  @issue#12
  Scenario Outline: Create <name>
    When a POST request is sent to "/parcels" with body:
      """json
      {"name": "<name>", "quote": "a \\"b\\""}
      """
    Then the response status is <status>
    And the response body contains 'ok'
    But the user's name isn't checked
    * the step "with \\"escaped\\" quotes" runs

    Examples: valid names
      | name   | status |
      | Ada    | 201    |
      | a\\|b  | 400    |

  Example: plain
    Given the value 5 < 6 > 4
    And a doc string:
      \`\`\`
      # not a comment
      Given not a step
      \`\`\`
`;

let lines;

before(async () => {
  const wasm = await readFile(require.resolve('vscode-oniguruma/release/onig.wasm'));
  await oniguruma.loadWASM(wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength));
  const registry = new textmate.Registry({
    onigLib: Promise.resolve({
      createOnigScanner: (patterns) => new oniguruma.OnigScanner(patterns),
      createOnigString: (s) => new oniguruma.OnigString(s),
    }),
    loadGrammar: async (scopeName) =>
      scopeName === SCOPE
        ? textmate.parseRawGrammar(await readFile(grammarPath, 'utf8'), grammarPath.pathname)
        : null,
  });
  const grammar = await registry.loadGrammar(SCOPE);
  let state = textmate.INITIAL;
  lines = sample.split('\n').map((text) => {
    const result = grammar.tokenizeLine(text, state);
    state = result.ruleStack;
    return { text, tokens: result.tokens };
  });
});

// The scopes of every token that overlaps `fragment` on the first line containing `line`
// (`fragment` itself by default), one array per token.
function scopes(line, fragment = line, occurrence = 0) {
  const found = lines.find((l) => l.text.includes(line));
  assert.ok(found, `no line contains ${JSON.stringify(line)}`);
  let start = -1;
  for (let i = 0; i <= occurrence; i++) {
    start = found.text.indexOf(fragment, start + 1);
  }
  assert.ok(start >= 0, `${JSON.stringify(fragment)} not found in ${JSON.stringify(found.text)}`);
  const end = start + fragment.length;
  return found.tokens.filter((t) => t.startIndex < end && t.endIndex > start).map((t) => t.scopes);
}

function has(line, fragment, scope, occurrence) {
  for (const s of scopes(line, fragment, occurrence)) {
    assert.ok(s.includes(scope), `${JSON.stringify(fragment)} in ${JSON.stringify(line)}: want ${scope}, got ${s.join(' ')}`);
  }
}

function lacks(line, fragment, prefix, occurrence) {
  for (const s of scopes(line, fragment, occurrence)) {
    assert.ok(!s.some((x) => x.startsWith(prefix)), `${JSON.stringify(fragment)} in ${JSON.stringify(line)}: unexpected ${prefix}*, got ${s.join(' ')}`);
  }
}

test('comments', () => {
  has('# language: en', '# language: en', 'comment.line.number-sign.gherkin');
  has('@smoke', '# a comment after tags', 'comment.line.number-sign.gherkin');
});

test('tags', () => {
  has('@smoke', '@smoke', 'entity.name.tag.gherkin');
  has('@smoke', '@api:parcels', 'entity.name.tag.gherkin');
  has('@issue#12', '@issue#12', 'entity.name.tag.gherkin');
});

test('section keywords and titles', () => {
  has('Feature:', 'Feature:', 'keyword.other.feature.gherkin');
  has('Feature:', 'Parcels API', 'entity.name.section.feature.gherkin');
  has('Background:', 'Background:', 'keyword.other.background.gherkin');
  has('Rule:', 'Rule:', 'keyword.other.rule.gherkin');
  has('Scenario Outline:', 'Scenario Outline:', 'keyword.other.scenario-outline.gherkin');
  has('Scenario Outline:', '<name>', 'variable.other.placeholder.gherkin');
  has('Examples:', 'Examples:', 'keyword.other.examples.gherkin');
  has('Examples:', 'valid names', 'entity.name.section.examples.gherkin');
  has('Example: plain', 'Example:', 'keyword.other.scenario.gherkin');
});

test('description lines are plain text', () => {
  const line = 'A description line';
  for (const s of scopes(line, 'A description line with "quotes" and <angles>.')) {
    assert.deepEqual(s, [SCOPE]);
  }
});

test('step keywords', () => {
  has('Given the parcels', 'Given', 'keyword.control.step.gherkin');
  has('When a POST', 'When', 'keyword.control.step.gherkin');
  has('Then the response status', 'Then', 'keyword.control.step.gherkin');
  has('And the response body', 'And', 'keyword.control.step.gherkin');
  has("But the user's", 'But', 'keyword.control.step.gherkin');
  has('* the step', '*', 'keyword.control.step.gherkin');
  has('Given the parcels', 'the parcels service', 'meta.step.gherkin');
  lacks('Given the parcels', 'the parcels service', 'keyword');
});

test('strings and placeholders in steps', () => {
  has('When a POST', '"/parcels"', 'string.quoted.double.gherkin');
  has('Then the response status', '<status>', 'variable.other.placeholder.gherkin');
  has('And the response body', "'ok'", 'string.quoted.single.gherkin');
  lacks("But the user's", "user's name isn't checked", 'string');
  has('* the step', '"with \\"escaped\\" quotes"', 'string.quoted.double.gherkin');
  has('* the step', '\\"', 'constant.character.escape.gherkin');
  lacks('* the step', 'runs', 'string');
  lacks('Given the value 5', '< 6 >', 'variable');
});

test('data tables', () => {
  has('| url |', '|', 'punctuation.separator.table.gherkin');
  has('| url |', 'http://localhost:8400', 'string.unquoted.table-cell.gherkin');
  has('| a\\|b', '\\|', 'constant.character.escape.gherkin');
  has('| a\\|b', '400', 'string.unquoted.table-cell.gherkin');
});

test('doc strings', () => {
  has('"""json', '"""', 'punctuation.definition.string.begin.gherkin');
  has('"""json', 'json', 'entity.name.type.media-type.gherkin');
  has('{"name"', '{"name": ', 'string.quoted.docstring.gherkin');
  has('{"name"', '<name>', 'variable.other.placeholder.gherkin');
  has('# not a comment', '# not a comment', 'string.quoted.docstring.gherkin');
  lacks('# not a comment', '# not a comment', 'comment');
  lacks('Given not a step', 'Given', 'keyword');
  // The doc string ends, and the line after it is a step again.
  has('Then the response status', 'Then', 'keyword.control.step.gherkin');
  lacks('Then the response status', 'Then', 'string');
});

// SPDX-License-Identifier: Apache-2.0
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseFeature, tableCells, type GherkinNode } from '../../src/gherkin';

// A compact view of a tree: "kind line-endLine name [examples]".
function outline(node: GherkinNode, depth = 0): string[] {
  const examples = node.examples ? ` [${node.examples}]` : '';
  const self = `${'  '.repeat(depth)}${node.kind} ${node.line}-${node.endLine} ${node.name}${examples}`;
  return [self, ...node.children.flatMap((c) => outline(c, depth + 1))];
}

const FEATURE = `# language: en
@api
Feature: Parcels
  A description that mentions Scenario: in passing.

  Background:
    Given the parcels service with the following properties:
      | url | http://localhost:8400 |

  @smoke
  Scenario: Register a parcel
    When a POST request is sent with body:
      """json
      Scenario: not a scenario
      | not | a row |
      """
    Then the response status is 201

  Scenario Outline: Status of <file>
    Given a GET request to /<file>
    Then the response status code is <status>

    Examples: found
      | file       | status |
      | hello.json | 200    |

    # a comment between examples
    @missing
    Examples:
      | file         | status |
      | missing.json | 404    |
      | a\\|b.json    | 404    |

  Rule: Depots

    Background:
      Given a depot

    Example: Dispatch a parcel
      Given a parcel

    Scenario Template: Batch of <n>
      Given <n> parcels
      Scenarios:
        | n |
        | 3 |

  Rule: Empty rule
`;

test('parses features, rules, scenarios, outlines and example rows with their lines', () => {
  const feature = parseFeature(FEATURE);
  assert.ok(feature);
  assert.deepEqual(outline(feature), [
    'feature 3-48 Parcels',
    '  scenario 11-17 Register a parcel',
    '  scenario 19-32 Status of <file>',
    '    example 25-25 hello.json | 200 [found]',
    '    example 31-31 missing.json | 404',
    '    example 32-32 a|b.json | 404',
    '  rule 34-46 Depots',
    '    scenario 39-40 Dispatch a parcel',
    '    scenario 42-46 Batch of <n>',
    '      example 46-46 3',
    '  rule 48-48 Empty rule',
  ]);
});

test('keeps the keyword as written', () => {
  const feature = parseFeature(FEATURE)!;
  const [scenario, outlineNode, rule] = feature.children;
  assert.equal(feature.keyword, 'Feature');
  assert.equal(scenario.keyword, 'Scenario');
  assert.equal(outlineNode.keyword, 'Scenario Outline');
  assert.equal(rule.keyword, 'Rule');
  assert.equal(rule.children[0].keyword, 'Example');
  assert.equal(rule.children[1].keyword, 'Scenario Template');
});

test('a scenario ends before the tags of the next one', () => {
  const feature = parseFeature('Feature: f\n  Scenario: a\n    Given x\n\n  @tag\n  Scenario: b\n    Given y\n')!;
  assert.deepEqual(
    feature.children.map((s) => [s.line, s.endLine]),
    [
      [2, 3],
      [6, 7],
    ],
  );
});

test('synonyms, CRLF line breaks and backtick doc strings', () => {
  const text = [
    'Ability: a',
    '  Scenario: s',
    '    Given x',
    '      ```',
    '      Scenario: inside',
    '      ```',
    '  Example: e',
    '    Given y',
  ].join('\r\n');
  const feature = parseFeature(text)!;
  assert.equal(feature.keyword, 'Ability');
  assert.deepEqual(
    feature.children.map((s) => `${s.line} ${s.name}`),
    ['2 s', '7 e'],
  );
});

test('no feature, no tree', () => {
  assert.equal(parseFeature(''), undefined);
  assert.equal(parseFeature('# just a comment\n  Scenario: orphan\n'), undefined);
});

test('a data table under a step is not an example row', () => {
  const feature = parseFeature('Feature: f\n  Scenario: s\n    Given a table\n      | a | b |\n      | 1 | 2 |\n')!;
  assert.deepEqual(feature.children[0].children, []);
});

test('table cells unescape \\| \\\\ and \\n', () => {
  assert.deepEqual(tableCells('| a | b\\|c | d\\\\e | f\\ng | h\\x |'), ['a', 'b|c', 'd\\e', 'f\ng', 'h\\x']);
  assert.deepEqual(tableCells('|a|b| trailing'), ['a', 'b']);
});

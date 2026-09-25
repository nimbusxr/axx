// SPDX-License-Identifier: Apache-2.0
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseDebugRequest } from '../../src/ideMarker';
import { parseLocationHint, parseServiceMessage, TeamCityReader, unescapeValue, type Outcome } from '../../src/teamcity';

test('parses attributes of a service message', () => {
  assert.deepEqual(
    parseServiceMessage("##teamcity[testStarted nodeId='279/270' parentNodeId='279' name='Given x']"),
    { name: 'testStarted', attrs: { nodeId: '279/270', parentNodeId: '279', name: 'Given x' } },
  );
  assert.deepEqual(parseServiceMessage('##teamcity[testingFinished]'), { name: 'testingFinished', attrs: {} });
  assert.deepEqual(parseServiceMessage("  ##teamcity[message 'single value']  "), {
    name: 'message',
    attrs: {},
    value: 'single value',
  });
});

test('unescapes values', () => {
  const msg = parseServiceMessage(
    "##teamcity[testFailed nodeId='1' message='it|'s |[not|] a||b|nline|r' details='|x|l|p|0x0041|0x00e9']",
  );
  assert.equal(msg?.attrs.message, "it's [not] a|b\nline\r");
  assert.equal(msg?.attrs.details, '\u0085\u2028\u2029Aé');
  assert.equal(unescapeValue('a|zb'), 'azb'); // unknown escapes keep the character
  assert.equal(unescapeValue('trailing|'), 'trailing|');
});

test('other lines are not service messages', () => {
  for (const line of [
    '',
    'Failed scenarios:',
    "log: ##teamcity[testStarted nodeId='1']",
    "##teamcity[testStarted nodeId='1'", // no closing bracket
    "##teamcity[testStarted nodeId='1]", // unterminated value
    "##teamcity[testStarted nodeId=1]", // unquoted value
  ]) {
    assert.equal(parseServiceMessage(line), undefined, line);
  }
});

test('parses location hints', () => {
  assert.deepEqual(parseLocationHint('file:///work/p/features/smoke.feature:12', false), {
    file: '/work/p/features/smoke.feature',
    line: 12,
  });
  assert.deepEqual(parseLocationHint('file:///C:/work/p/a b.feature:3', true), { file: 'C:\\work\\p\\a b.feature', line: 3 });
  assert.equal(parseLocationHint('file:///work/p/no-line.feature', false), undefined);
  assert.equal(parseLocationHint(undefined, false), undefined);
});

// Interleaved output of two scenarios, as `axx run --format teamcity` prints it.
const RUN = `##teamcity[enteredTheMatrix]
##teamcity[testingStarted]
##teamcity[testSuiteStarted nodeId='feature-1' parentNodeId='0' name='Feature: Hello axx' locationHint='file:///p/features/smoke.feature:1']
##teamcity[testSuiteStarted nodeId='279' parentNodeId='feature-1' name='Scenario: A wrong expectation' locationHint='file:///p/features/smoke.feature:12']
##teamcity[testStarted nodeId='279/270' parentNodeId='279' name='Given a GET request' locationHint='file:///p/features/smoke.feature:13']
##teamcity[testSuiteStarted nodeId='290' parentNodeId='feature-1' name='Scenario Outline: Status of hello.json (example 1)' locationHint='file:///p/features/smoke.feature:25']
##teamcity[testStarted nodeId='290/272' parentNodeId='290' name='Given a GET request to /hello.json' locationHint='file:///p/features/smoke.feature:19']
##teamcity[testFinished nodeId='279/270' duration='3']
##teamcity[testStdOut nodeId='290/272' out='log: GET -> 200|n{"message": "Hello, axx"}|n']
##teamcity[testFinished nodeId='290/272' duration='7']
##teamcity[testStarted nodeId='279/285' parentNodeId='279' name='Then the message is |'Hello, world|'' locationHint='file:///p/features/smoke.feature:15']
##teamcity[testSuiteFinished nodeId='290']
##teamcity[testFailed nodeId='279/285' message='message is not Hello, world' details='message is not Hello, world|n  expected: "Hello, world"|n  actual:   "Hello, axx"' type='comparisonFailure' expected='"Hello, world"' actual='"Hello, axx"']
##teamcity[testFinished nodeId='279/285' duration='0']
##teamcity[testStarted nodeId='279/289' parentNodeId='279' name='And the status is 200' locationHint='file:///p/features/smoke.feature:16']
##teamcity[testIgnored nodeId='279/289' message='skipped']
##teamcity[testFinished nodeId='279/289' duration='0']
##teamcity[testSuiteFinished nodeId='279']
##teamcity[testSuiteFinished nodeId='feature-1']
##teamcity[testingFinished]

2 scenarios (1 failed, 1 passed)`;

test('follows interleaved scenarios by node id', () => {
  const events: string[] = [];
  const outcomes = new Map<string, Outcome>();
  const reader = new TeamCityReader(
    {
      suiteStarted: (n) => events.push(`suite+ ${n.id} ${n.name} @${n.location?.line}`),
      suiteFinished: (n, outcome) => {
        outcomes.set(n.id, outcome);
        events.push(`suite- ${n.id} ${outcome}`);
      },
      testStarted: (n) => events.push(`test+ ${n.id} in ${n.parentId} @${n.location?.line}`),
      testOutput: (n, text) => events.push(`out ${n.id} ${JSON.stringify(text)}`),
      testFinished: (n) => events.push(`test- ${n.id} ${n.outcome} ${n.durationMs}ms`),
      output: (line) => events.push(`plain ${JSON.stringify(line)}`),
    },
    false,
  );
  for (const line of RUN.split('\n')) reader.line(line);

  assert.deepEqual(events, [
    'suite+ feature-1 Feature: Hello axx @1',
    'suite+ 279 Scenario: A wrong expectation @12',
    'test+ 279/270 in 279 @13',
    'suite+ 290 Scenario Outline: Status of hello.json (example 1) @25',
    'test+ 290/272 in 290 @19',
    'test- 279/270 passed 3ms',
    'out 290/272 "log: GET -> 200\\n{\\"message\\": \\"Hello, axx\\"}\\n"',
    'test- 290/272 passed 7ms',
    'test+ 279/285 in 279 @15',
    'suite- 290 passed',
    'test- 279/285 failed 0ms',
    'test+ 279/289 in 279 @16',
    'test- 279/289 skipped 0ms',
    'suite- 279 failed',
    'suite- feature-1 failed',
    'plain ""',
    'plain "2 scenarios (1 failed, 1 passed)"',
  ]);

  const failed = reader.node('279/285');
  assert.equal(failed?.name, "Then the message is 'Hello, world'");
  assert.deepEqual(failed?.failure, {
    message: 'message is not Hello, world',
    details: 'message is not Hello, world\n  expected: "Hello, world"\n  actual:   "Hello, axx"',
    expected: '"Hello, world"',
    actual: '"Hello, axx"',
  });
  assert.equal(reader.node('279/289')?.ignored, 'skipped');
  assert.deepEqual(reader.node('feature-1')?.counts, { passed: 2, failed: 1, skipped: 1 });
});

test('a failure without a comparison has no expected or actual value', () => {
  const reader = new TeamCityReader({}, false);
  reader.line("##teamcity[testSuiteStarted nodeId='1' parentNodeId='0' name='s']");
  reader.line("##teamcity[testStarted nodeId='1/1' parentNodeId='1' name='Given something axx does not know']");
  reader.line("##teamcity[testFailed nodeId='1/1' message='Undefined step' details='search the steps']");
  reader.line("##teamcity[testFinished nodeId='1/1' duration='0']");
  assert.deepEqual(reader.node('1/1')?.failure, { message: 'Undefined step', details: 'search the steps' });
});

test('a suite whose steps were skipped (pending) is skipped', () => {
  let outcome: Outcome | undefined;
  const reader = new TeamCityReader({ suiteFinished: (_, o) => (outcome = o) }, false);
  reader.line("##teamcity[testSuiteStarted nodeId='1' parentNodeId='0' name='s']");
  reader.line("##teamcity[testStarted nodeId='1/1' parentNodeId='1' name='Given a']");
  reader.line("##teamcity[testFinished nodeId='1/1' duration='1']");
  reader.line("##teamcity[testStarted nodeId='1/2' parentNodeId='1' name='When b']");
  reader.line("##teamcity[testIgnored nodeId='1/2' message='pending: not written yet']");
  reader.line("##teamcity[testFinished nodeId='1/2' duration='0']");
  reader.line("##teamcity[testSuiteFinished nodeId='1']");
  assert.equal(outcome, 'skipped');
  assert.equal(reader.node('1/2')?.ignored, 'pending: not written yet');
});

test('messages for unknown nodes are ignored', () => {
  const reader = new TeamCityReader({ testFinished: () => assert.fail('no such node') }, false);
  reader.line("##teamcity[testFailed nodeId='9/9' message='x']");
  reader.line("##teamcity[testFinished nodeId='9/9' duration='1']");
  reader.line("##teamcity[testSuiteFinished nodeId='9']");
});

test('parses the debugger request for step code', () => {
  assert.deepEqual(parseDebugRequest('[AXX-IDE] debug-attach-request name=axx-steps type=go host=127.0.0.1 port=2345'), {
    kind: 'attach',
    name: 'axx-steps',
    type: 'go',
    host: '127.0.0.1',
    port: 2345,
  });
  assert.deepEqual(
    parseDebugRequest('\x1b[2m12:00:01\x1b[0m [AXX-IDE] debug-listener-request port=5005 x host=localhost type=java name=api name=other extra=1'),
    { kind: 'listener', name: 'api', type: 'java', host: 'localhost', port: 5005 },
  );
});

test('rejects incomplete debugger requests', () => {
  for (const line of [
    'debug-attach-request name=a type=go host=h port=1',
    '[AXX-IDE] debug-attach-request name=a type=go host=h',
    '[AXX-IDE] debug-attach-request name=a type=go host=h port=0',
    '[AXX-IDE] debug-attach-request name=a type=go host=h port=65536',
    '[AXX-IDE] debug-attach-request name=a type=go host=h port=12ab',
    '[AXX-IDE] debug-attach-requests name=a type=go host=h port=1',
  ]) {
    assert.equal(parseDebugRequest(line), undefined, line);
  }
});

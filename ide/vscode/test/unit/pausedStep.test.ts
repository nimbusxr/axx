// SPDX-License-Identifier: Apache-2.0
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as http from 'node:http';
import type { AddressInfo } from 'node:net';
import * as os from 'node:os';
import * as path from 'node:path';
import { afterEach, beforeEach, test } from 'node:test';
import { isLoopbackUrl, parseIdeAnnouncement, parseScenarioPause } from '../../src/ideMarker';
import {
  featureSteps,
  fromRecordingFile,
  highlightStep,
  indentOf,
  linesOf,
  recordedSteps,
  reindent,
  runStep,
  stepAt,
  stepWithTable,
} from '../../src/pausedStep';

const URL = 'http://127.0.0.1:53211/3f9c0a/';

// --- The lines ---

test('parses a pause with its location and recording file, and a resume', () => {
  assert.deepEqual(
    parseScenarioPause(`[AXX-IDE] paused url=${URL} location=features/shop-portal.feature:24 recording=/work/parcels/.axx/web/recording.txt`),
    { kind: 'paused', url: URL, location: 'features/shop-portal.feature:24', recording: '/work/parcels/.axx/web/recording.txt' },
  );
  assert.deepEqual(parseScenarioPause(`[AXX-IDE] resumed url=${URL}`), { kind: 'resumed', url: URL });
});

test('follows the marker rules for pauses: other text first, ANSI colors, any order, %20', () => {
  assert.deepEqual(
    parseScenarioPause(
      `\x1b[2m12:00:01\x1b[0m [AXX-IDE] paused future=1 recording=/work/my%20parcels/recording.txt location=features/My%20Parcels.feature:3 url=${URL} url=http://127.0.0.1:1/other/`,
    ),
    { kind: 'paused', url: URL, location: 'features/My Parcels.feature:3', recording: '/work/my parcels/recording.txt' },
  );
  // Without a location or recording file, or with a relative one.
  assert.deepEqual(parseScenarioPause(`[AXX-IDE] paused url=${URL} recording=.axx/web/recording.txt`), { kind: 'paused', url: URL });
  // Lines of other kinds do not mix.
  assert.equal(parseScenarioPause('[AXX-IDE] trace path=/p/t.zip location=features/a.feature:3'), undefined);
  assert.equal(parseIdeAnnouncement(`[AXX-IDE] paused url=${URL}`), undefined);
});

test('rejects pauses without a loopback http url ending with /', () => {
  for (const line of [
    '',
    `paused url=${URL}`,
    '[AXX-IDE] paused',
    '[AXX-IDE] paused location=features/a.feature:3',
    '[AXX-IDE] resumed url=',
    '[AXX-IDE] paused url=http://127.0.0.1:53211/3f9c0a',
    '[AXX-IDE] paused url=https://127.0.0.1:53211/3f9c0a/',
    '[AXX-IDE] paused url=http://example.com:53211/3f9c0a/',
    '[AXX-IDE] paused url=http://10.0.0.7:53211/3f9c0a/',
    '[AXX-IDE] paused url=http://127.0.0.1:53211/3f9c0a/?x=1',
    '[AXX-IDE] paused url=file:///tmp/',
    `[AXX-IDE] pause url=${URL}`,
  ]) {
    assert.equal(parseScenarioPause(line), undefined, line);
  }
  assert.equal(isLoopbackUrl('http://localhost:1/a/'), true);
  assert.equal(isLoopbackUrl('http://[::1]:1/a/'), true);
  assert.equal(isLoopbackUrl('http://127.0.0.1.example.com:1/a/'), false);
  assert.equal(isLoopbackUrl('http://user@127.0.0.1:1/a/'), false);
  assert.equal(isLoopbackUrl('http://127.0.0.1:1'), false);
});

// --- The steps ---

// The feature of the IntelliJ plugin's AxxStepBreakpointTypeTest, where the Gherkin plugin finds the
// same steps.
const FEATURE = linesOf(
  [
    '@web', // 1
    'Feature: Shop portal',
    '  Given in a description, not a step', // 3
    '',
    '  Background:', // 5
    '    Given the shop portal with the following properties:',
    '      | url | http://localhost:8400 |', // 7
    '',
    '  Scenario: Register a parcel', // 9
    '    # When a comment',
    '    When the page "/parcels/new" is opened', // 11
    '    And the text "Register" is entered in:',
    '      """', // 13
    '      Then not a step',
    '      """', // 15
    '    * the button "Register" is clicked',
    '    Then the page shows "Registered"', // 17
    '',
    '  Scenario Outline: Track <parcel>', // 19
    '    When the page "/track/<parcel>" is opened',
    '    But the page does not show "Unknown parcel"', // 21
    '',
    '    Examples:', // 23
    '      | parcel |',
    '      | P-1    |', // 25
    '',
  ].join('\n'),
);

test('finds the steps of a feature file', () => {
  const lines = featureSteps(FEATURE).flatMap((step, i) => (step === undefined ? [] : [i + 1]));
  assert.deepEqual(lines, [6, 11, 12, 16, 17, 20, 21]);
  for (let line = -1; line < FEATURE.length + 2; line++) {
    assert.equal(stepAt(FEATURE, line) !== undefined, lines.includes(line + 1), `line ${line + 1}`);
  }
  assert.equal(stepAt(FEATURE, 15), '* the button "Register" is clicked');
  assert.equal(stepAt(linesOf('Feature: A\r\nScenario: B\r\n\tThen the shop shows "Registered"  \r\n'), 2), 'Then the shop shows "Registered"');
});

test("finds the steps of a rule's background and scenarios, which Debug pauses at", () => {
  const lines = linesOf(
    [
      'Feature: Returns', // 1
      '',
      '  Rule: A parcel is returned within 30 days', // 3
      '',
      '    Background:', // 5
      '      Given the returns portal is open',
      '', // 7
      '    Example: Return a parcel',
      '      When the parcel "P-1" is returned with:', // 9
      '        ```',
      '        And not a step', // 11
      '        ```',
      '      Then the return is "accepted"', // 13
      '',
      '    @slow', // 15
      '    Scenario Template: Return <parcel>',
      '      Then the return of "<parcel>" is "<result>"', // 17
      '',
      '      Scenarios:', // 19
      '        | parcel | result  |',
      '        | P-2    | refused |', // 21
    ].join('\n'),
  );
  assert.deepEqual(featureSteps(lines).flatMap((step, i) => (step === undefined ? [] : [i + 1])), [6, 9, 13, 17]);
});

test('gives a step with the rows of its table right below it', () => {
  assert.equal(stepWithTable(FEATURE, 5), 'Given the shop portal with the following properties:\n| url | http://localhost:8400 |');
  assert.equal(stepWithTable(FEATURE, 10), 'When the page "/parcels/new" is opened');
  assert.equal(stepWithTable(FEATURE, 11), 'And the text "Register" is entered in:'); // a doc string is no table
  assert.equal(stepWithTable(FEATURE, 6), undefined);
  const lines = linesOf(
    [
      'Feature: Quotes',
      '  Scenario: Quote a parcel',
      '    When the form is filled in with:',
      '      | Weight | 2 kg |',
      '      | Zone   | 3    |',
      '    # the quote',
      '      | not | this |',
    ].join('\n'),
  );
  assert.equal(stepWithTable(lines, 2), 'When the form is filled in with:\n| Weight | 2 kg |\n| Zone   | 3    |');
});

test('re-indents recorded steps, keeping their comments and relative indentation', () => {
  const recorded =
    '\n  When the page "/quote" is opened  \n  # Not recorded: a drag and drop\n\n  And the "Get a quote" button is clicked\n    | a | b |\n\n\n';
  assert.equal(
    reindent(recorded, '      '),
    '      When the page "/quote" is opened\n      # Not recorded: a drag and drop\n\n      And the "Get a quote" button is clicked\n        | a | b |',
  );
  assert.equal(
    reindent(recorded),
    '  When the page "/quote" is opened\n  # Not recorded: a drag and drop\n\n  And the "Get a quote" button is clicked\n    | a | b |',
  );
  assert.equal(reindent('        Then done', '\t'), '\tThen done');
  assert.equal(reindent('\n  \n', '    '), '');
  assert.equal(indentOf('\t When x'), '\t ');
  assert.equal(indentOf('When x'), '');
});

test('drops the comment line a recording file starts with', () => {
  assert.equal(
    fromRecordingFile('# Steps recorded in Playwright\'s Inspector\n    When the page "/quote" is opened\n    # Not recorded: a hover\n'),
    '    When the page "/quote" is opened\n    # Not recorded: a hover\n',
  );
  assert.equal(fromRecordingFile('    When x\n'), '    When x\n');
  assert.equal(fromRecordingFile("# Steps recorded in Playwright's Inspector"), '');
  assert.equal(fromRecordingFile(''), '');
});

// --- The requests ---

interface Seen {
  method: string;
  path: string;
  body: string;
}

// A paused scenario's server, as the web pack runs it: it answers what a test sets below its URL.
let server: http.Server;
let url: string;
let seen: Seen[];
let answers: Map<string, { status: number; text: string; delayMs?: number }>;

beforeEach(async () => {
  seen = [];
  answers = new Map();
  server = http.createServer((req, res) => {
    let body = '';
    req.setEncoding('utf8');
    req.on('data', (chunk: string) => (body += chunk));
    req.on('end', () => {
      const below = (req.url ?? '').replace(/^\/3f9c0a\//, '');
      seen.push({ method: req.method ?? '', path: below, body });
      const answer = answers.get(`${req.method} ${below}`) ?? { status: 404, text: '404 page not found' };
      setTimeout(() => {
        res.writeHead(answer.status, { 'Content-Type': 'text/plain; charset=utf-8' });
        res.end(answer.text);
      }, answer.delayMs ?? 0);
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  url = `http://127.0.0.1:${(server.address() as AddressInfo).port}/3f9c0a/`;
});

afterEach(async () => {
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
});

test("posts a step's text to highlight, and gives the answer", async () => {
  answers.set('POST highlight', { status: 200, text: 'the button named "Get a quote"' });
  assert.deepEqual(await highlightStep(url, 'And the "Get a quote" button is clicked'), {
    status: 200,
    text: 'the button named "Get a quote"',
  });
  assert.deepEqual(seen, [{ method: 'POST', path: 'highlight', body: 'And the "Get a quote" button is clicked' }]);
  answers.set('POST highlight', { status: 404, text: 'No button named "Send" on the page; 1 buttons: ...' });
  assert.equal((await highlightStep(url, 'And the "Send" button is clicked')).status, 404);
});

test('gives up on a highlight after 2 seconds', async () => {
  answers.set('POST highlight', { status: 200, text: 'late', delayMs: 4000 });
  await assert.rejects(highlightStep(url, 'When x'), /no answer in 2000 ms/);
});

test('runs a step with its table, as long as it takes', async () => {
  answers.set('POST run', { status: 200, text: 'passed', delayMs: 2500 });
  const step = 'When the form is filled in with:\n| Weight | 2 kg |';
  assert.deepEqual(await runStep(url, step), { status: 200, text: 'passed' });
  assert.deepEqual(seen, [{ method: 'POST', path: 'run', body: step }]);
  answers.set('POST run', { status: 422, text: 'No button named "Send" on the page' });
  assert.deepEqual(await runStep(url, 'And the "Send" button is clicked'), { status: 422, text: 'No button named "Send" on the page' });
});

test('stops waiting for a step when told to', async () => {
  answers.set('POST run', { status: 200, text: 'passed', delayMs: 4000 });
  const abort = new AbortController();
  const running = runStep(url, 'When x', abort.signal);
  setTimeout(() => abort.abort(), 100);
  await assert.rejects(running, { name: 'AbortError' });
});

test('gets the recorded steps from the run, else from the recording file', async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'axx-recording-'));
  const recording = path.join(dir, 'recording.txt');
  fs.writeFileSync(recording, '# Steps recorded in Playwright\'s Inspector\n    When the page "/quote" is opened\n');
  answers.set('GET recorded', { status: 200, text: '    And the "Get a quote" button is clicked\n' });

  assert.equal(await recordedSteps(url, recording), '    And the "Get a quote" button is clicked\n');
  assert.deepEqual(seen, [{ method: 'GET', path: 'recorded', body: '' }]);

  // Not answered, or the run has ended: the file.
  answers.set('GET recorded', { status: 500, text: 'oops' });
  assert.equal(await recordedSteps(url, recording), '    When the page "/quote" is opened\n');
  assert.equal(await recordedSteps('http://127.0.0.1:1/3f9c0a/', recording), '    When the page "/quote" is opened\n');
  assert.equal(await recordedSteps(undefined, recording), '    When the page "/quote" is opened\n');

  assert.equal(await recordedSteps(undefined, path.join(dir, 'missing.txt')), undefined);
  assert.equal(await recordedSteps(undefined, undefined), undefined);
});

test('sends nothing to a URL off the loopback interface', async () => {
  await assert.rejects(runStep('http://example.com/3f9c0a/', 'When x'), /not a paused scenario's URL/);
  assert.deepEqual(seen, []);
});

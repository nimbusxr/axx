// SPDX-License-Identifier: Apache-2.0
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { test } from 'node:test';
import { notAStep, pauseSteps, runArguments, traceViewerUrl, viewerFolder } from '../../src/browsers';
import { parseDebugRequest, parseIdeAnnouncement } from '../../src/ideMarker';
import { linesOf } from '../../src/pausedStep';

const VIEWER = '/home/dev/.cache/axx/web/playwright-1.62.1-node-24.19.0-linux-x64/package/lib/vite/traceViewer';

test('parses what packs announce', () => {
  assert.deepEqual(parseIdeAnnouncement(`[AXX-IDE] trace-viewer dir=${VIEWER}`), { kind: 'trace-viewer', dir: VIEWER });
  assert.deepEqual(parseIdeAnnouncement('[AXX-IDE] trace-viewer dir=/cache/my%20viewer'), {
    kind: 'trace-viewer',
    dir: '/cache/my viewer',
  });
  assert.deepEqual(
    parseIdeAnnouncement(
      '[AXX-IDE] trace path=/work/parcels/.axx/web/traces/register.zip location=features/shop-portal.feature:42',
    ),
    { kind: 'trace', path: '/work/parcels/.axx/web/traces/register.zip', file: 'features/shop-portal.feature', line: 42 },
  );
  assert.deepEqual(
    parseIdeAnnouncement('[AXX-IDE] video location=features/shop-portal.feature:7 path=/work/parcels/.axx/web/videos/a.webm'),
    { kind: 'video', path: '/work/parcels/.axx/web/videos/a.webm', file: 'features/shop-portal.feature', line: 7 },
  );
});

test('follows the marker rules: other text first, ANSI colors, any order, first value wins, %20', () => {
  assert.deepEqual(
    parseIdeAnnouncement(
      '\x1b[2m12:00:01\x1b[0m [AXX-IDE] trace stray future=1 location=features/My%20Parcels.feature:3 path=/p/my%20traces/t.zip path=/other.zip',
    ),
    { kind: 'trace', path: '/p/my traces/t.zip', file: 'features/My Parcels.feature', line: 3 },
  );
  // Debugger requests are not announcements, nor the other way around.
  assert.equal(parseIdeAnnouncement('[AXX-IDE] debug-attach-request name=a type=go host=h port=1'), undefined);
  assert.equal(parseDebugRequest(`[AXX-IDE] trace-viewer dir=${VIEWER}`), undefined);
});

test('rejects incomplete announcements', () => {
  for (const line of [
    'trace-viewer dir=/cache/traceViewer',
    '[AXX-IDE] trace-viewer',
    '[AXX-IDE] trace-viewer dir=',
    '[AXX-IDE] trace-viewer dir=cache/traceViewer',
    '[AXX-IDE] trace-viewer url=http://localhost:1234/',
    '[AXX-IDE] trace-viewers dir=/cache/traceViewer',
    '[AXX-IDE] trace path=/p/t.zip',
    '[AXX-IDE] trace location=features/a.feature:3',
    '[AXX-IDE] trace path=relative/t.zip location=features/a.feature:3',
    '[AXX-IDE] trace path=/p/t.zip location=features/a.feature',
    '[AXX-IDE] trace path=/p/t.zip location=features/a.feature:0',
    '[AXX-IDE] video path=/p/v.webm location=:3',
  ]) {
    assert.equal(parseIdeAnnouncement(line), undefined, line);
  }
});

test('builds the run arguments', () => {
  assert.deepEqual(runArguments(['features/a.feature:3']), ['run', '--format', 'teamcity', 'features/a.feature:3']);
  assert.deepEqual(runArguments(['features'], { profiles: ['ios', 'watch'] }), [
    'run',
    '--format',
    'teamcity',
    'features',
    '--profile',
    'ios,watch',
  ]);
  assert.deepEqual(runArguments(['features'], { profiles: [] }), runArguments(['features']));
  assert.deepEqual(runArguments(['features'], { debug: true, debugSteps: true }), [
    'run',
    '--format',
    'teamcity',
    '--debug-steps',
    '--workers',
    '1',
    '--set',
    'packs.web-core.pauseOnFailure=true',
    'features',
  ]);
  // Without the Go extension: the same run, without step code under the debugger.
  assert.deepEqual(runArguments(['features'], { debug: true }), [
    'run',
    '--format',
    'teamcity',
    '--workers',
    '1',
    '--set',
    'packs.web-core.pauseOnFailure=true',
    'features',
  ]);
  assert.deepEqual(runArguments(['features'], { debug: true, debugSteps: false }), runArguments(['features'], { debug: true }));
  assert.deepEqual(runArguments(['features/a.feature:3', 'features/b.feature'], { watchSlowdownMs: 300 }), [
    'run',
    '--format',
    'teamcity',
    '--workers',
    '1',
    '--watch',
    '--slowdown',
    '300ms',
    'features/a.feature:3',
    'features/b.feature',
  ]);
});

test('debugging pauses before steps and where a scenario fails, one scenario at a time', () => {
  const pauseAt = ['features/shop-portal.feature:9', 'features/shop-portal.feature:24'];
  // Step code breakpoints still stop.
  assert.deepEqual(runArguments(['features/shop-portal.feature'], { debug: true, debugSteps: true, pauseAt }), [
    'run',
    '--format',
    'teamcity',
    '--debug-steps',
    '--workers',
    '1',
    '--set',
    'packs.web-core.pauseOnFailure=true',
    '--pause-at',
    'features/shop-portal.feature:9',
    '--pause-at',
    'features/shop-portal.feature:24',
    'features/shop-portal.feature',
  ]);
  // Without the Go extension, the scenarios pause all the same.
  assert.deepEqual(runArguments(['features/shop-portal.feature'], { debug: true, pauseAt }), [
    'run',
    '--format',
    'teamcity',
    '--workers',
    '1',
    '--set',
    'packs.web-core.pauseOnFailure=true',
    '--pause-at',
    'features/shop-portal.feature:9',
    '--pause-at',
    'features/shop-portal.feature:24',
    'features/shop-portal.feature',
  ]);
  // Watching only shows the browsers, and running neither shows nor pauses.
  assert.deepEqual(runArguments(['features/shop-portal.feature:20'], { watchSlowdownMs: 0, pauseAt }), [
    'run',
    '--format',
    'teamcity',
    '--workers',
    '1',
    '--watch',
    'features/shop-portal.feature:20',
  ]);
  assert.deepEqual(runArguments(['features'], { pauseAt }), runArguments(['features']));
  assert.deepEqual(runArguments(['features'], { debugSteps: true, pauseAt }), runArguments(['features']));
});

// The feature of the IntelliJ plugin's AxxStepBreakpointTypeTest: its steps are on lines 6, 11, 12,
// 16, 17, 20 and 21, a Scenario Outline's included.
const SHOP_PORTAL = linesOf(
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
const SHOP_PORTAL_STEPS = [6, 11, 12, 16, 17, 20, 21];

const TRACKING = linesOf(
  [
    'Feature: Tracking', // 1
    '',
    '  Scenario: Track a parcel', // 3
    '    When the page "/track/P-1" is opened',
    '    Then the page shows "In transit"', // 5
  ].join('\n'),
);

const PROJECT = path.join(path.sep, 'work', 'parcels', 'acceptance');
const PORTAL = path.join(PROJECT, 'features', 'web', 'shop-portal.feature');
const TRACKING_FILE = path.join(PROJECT, 'features', 'tracking.feature');
const rel = (file: string, line: number): string => `${path.relative(PROJECT, file)}:${line}`;

test('pauses before the breakpoints on steps in the files the run runs, in order', () => {
  const other = path.join(path.sep, 'work', 'other', 'features', 'tracking.feature');
  const breakpoints = [
    { file: PORTAL, line: 21 },
    { file: TRACKING_FILE, line: 4 },
    { file: PORTAL, line: 11 },
    { file: PORTAL, line: 21 },
    { file: other, line: 4 },
  ];
  assert.deepEqual(pauseSteps(breakpoints, PROJECT, new Map([[PORTAL, SHOP_PORTAL]])), {
    pauseAt: [rel(PORTAL, 11), rel(PORTAL, 21)],
    notSteps: [],
  });
  const both = new Map([
    [PORTAL, SHOP_PORTAL],
    [TRACKING_FILE, TRACKING],
  ]);
  assert.deepEqual(pauseSteps(breakpoints, PROJECT, both), {
    pauseAt: [rel(TRACKING_FILE, 4), rel(PORTAL, 11), rel(PORTAL, 21)],
    notSteps: [],
  });
  const returns = path.join(PROJECT, 'features', 'returns.feature');
  assert.deepEqual(pauseSteps(breakpoints, PROJECT, new Map([[returns, TRACKING]])), { pauseAt: [], notSteps: [] });
  assert.deepEqual(pauseSteps([], PROJECT, both), { pauseAt: [], notSteps: [] });
});

test('ignores the breakpoints on lines that are not steps, as the IntelliJ plugin does', () => {
  const everyLine = SHOP_PORTAL.map((_, i) => ({ file: PORTAL, line: i + 1 }));
  const { pauseAt, notSteps } = pauseSteps(everyLine, PROJECT, new Map([[PORTAL, SHOP_PORTAL]]));
  assert.deepEqual(pauseAt, SHOP_PORTAL_STEPS.map((line) => rel(PORTAL, line)));
  assert.deepEqual(notSteps, everyLine.filter((b) => !SHOP_PORTAL_STEPS.includes(b.line)));

  // Once each, in file and line order, in the files the run runs only; the steps still pause.
  const breakpoints = [
    { file: PORTAL, line: 19 }, // Scenario Outline: Track <parcel>
    { file: TRACKING_FILE, line: 3 }, // Scenario: Track a parcel
    { file: PORTAL, line: 14 }, // in a doc string
    { file: PORTAL, line: 20 },
    { file: PORTAL, line: 19 },
    { file: path.join(PROJECT, 'features', 'returns.feature'), line: 1 },
  ];
  const both = new Map([
    [PORTAL, SHOP_PORTAL],
    [TRACKING_FILE, TRACKING],
  ]);
  assert.deepEqual(pauseSteps(breakpoints, PROJECT, both), {
    pauseAt: [rel(PORTAL, 20)],
    notSteps: [
      { file: TRACKING_FILE, line: 3 },
      { file: PORTAL, line: 14 },
      { file: PORTAL, line: 19 },
    ],
  });
});

test('says why it ignores a breakpoint', () => {
  assert.equal(
    notAStep(PROJECT, { file: PORTAL, line: 19 }),
    `Ignored the breakpoint at ${rel(PORTAL, 19)}: it is not on a step. Debug pauses only at breakpoints on steps.`,
  );
});

test('a slowdown of 0 adds none', () => {
  const watch = (watchSlowdownMs: number): string[] => runArguments(['features'], { watchSlowdownMs });
  assert.deepEqual(watch(0), ['run', '--format', 'teamcity', '--workers', '1', '--watch', 'features']);
  assert.deepEqual(watch(Number.NaN), watch(0));
  assert.deepEqual(watch(-5), watch(0));
  assert.equal(watch(1250.7).at(-2), '1250ms');
});

test("opens a trace in the trace viewer's files, served with it", () => {
  const files = 'http://127.0.0.1:51234/9a8b7c6d5e4f30211203f4e5d6c7b8a9/traceViewer/';
  const trace = 'http://127.0.0.1:51234/0f1e2d3c4b5a69788796a5b4c3d2e1f0/register.zip';
  const page = traceViewerUrl(files, trace);
  assert.equal(page, `${files}index.html?trace=${encodeURIComponent(trace)}`);
  assert.equal(traceViewerUrl(files.slice(0, -1), trace), page);
  assert.equal(new URL(page).searchParams.get('trace'), trace);
});

test("uses the first trace viewer's folder that still has it", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'axx-viewer-'));
  try {
    const current = path.join(dir, 'new', 'traceViewer');
    fs.mkdirSync(current, { recursive: true });
    fs.writeFileSync(path.join(current, 'index.html'), '<html>');
    const gone = path.join(dir, 'old', 'traceViewer');
    assert.equal(viewerFolder(gone, current), current);
    assert.equal(viewerFolder(undefined, current), current);
    assert.equal(viewerFolder(current, undefined), current);
    assert.equal(viewerFolder(gone, undefined), undefined);
    assert.equal(viewerFolder(), undefined);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

// SPDX-License-Identifier: Apache-2.0
import assert from 'node:assert/strict';
import { spawn, type ChildProcess } from 'node:child_process';
import { EventEmitter } from 'node:events';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { PassThrough } from 'node:stream';
import { test } from 'node:test';
import {
  answerLine,
  approvalMessage,
  declaresServices,
  HostProcess,
  HostState,
  hostStatus,
  mapRunFile,
  parseHostError,
  parseHostEvent,
  runError,
  runStateText,
  type ApprovalEvent,
  type HostEvent,
  type HostExit,
  type RunEvent,
} from '../../src/host';
import { mappingKeys } from '../../src/profiles';

// What `axx host --ide` printed for two runs of an agent, the second after compose.yaml changed
// (shortened: the definitions and the report).
const SERVING = '{"event":"serving","addr":"127.0.0.1:51860","dir":"/work/parcels","services":["api"],"sources":["compose.yaml"]}';
const EVENTS = [
  SERVING,
  '{"event":"run","dir":"/work/parcels","run":1,"command":"axx run --compact","state":"queued"}',
  '{"event":"run","services":["api"],"run":1,"command":"axx run --compact","state":"starting"}',
  '{"event":"run","services":["api"],"run":1,"command":"axx run --compact","state":"started","seconds":2.004112416}',
  `{"event":"report","run":1,"line":"##teamcity[testSuiteStarted nodeId='129' parentNodeId='feature-1' name='Scenario: the service is healthy' locationHint='file:///work/parcels/features/health.feature:6']"}`,
  '{"event":"report","run":1}',
  '{"event":"run","run":1,"command":"axx run --compact","state":"stopping"}',
  '{"event":"run","run":1,"command":"axx run --compact","state":"ended","result":{"passed":1,"failed":0,"other":0},"seconds":3.307338666}',
  '{"event":"run","dir":"/work/parcels","run":2,"command":"axx run --compact","state":"queued"}',
  '{"event":"run","run":2,"command":"axx run --compact","state":"approval"}',
  '{"event":"approval","warnings":["services.api: compose service api mounts /tmp of this machine"],"run":2,"command":"axx run --compact","id":1,"approved":"services:\\n  api: {}\\n","proposed":"services:\\n  api:\\n    volumes: [/tmp:/host-tmp]\\n","diff":"--- approved\\n+++ proposed\\n"}',
  '{"event":"answered","run":2,"id":1,"answer":"approved"}',
];

test('parses the events of axx host --ide', () => {
  const events = EVENTS.map(parseHostEvent);
  assert.deepEqual(events[0], {
    event: 'serving',
    addr: '127.0.0.1:51860',
    dir: '/work/parcels',
    services: ['api'],
    sources: ['compose.yaml'],
    warnings: [],
    changed: false,
  });
  assert.deepEqual(events[1], { event: 'run', run: 1, command: 'axx run --compact', dir: '/work/parcels', state: 'queued', services: [] });
  assert.deepEqual(events[3], { event: 'run', run: 1, command: 'axx run --compact', state: 'started', services: ['api'], seconds: 2.004112416 });
  assert.equal((events[4] as { line: string }).line.slice(0, 33), "##teamcity[testSuiteStarted nodeI");
  assert.deepEqual(events[5], { event: 'report', run: 1, line: '' }); // an empty line of the report
  assert.deepEqual(events[7], {
    event: 'run',
    run: 1,
    command: 'axx run --compact',
    state: 'ended',
    services: [],
    seconds: 3.307338666,
    result: { passed: 1, failed: 0, other: 0 },
  });
  assert.deepEqual(events[10], {
    event: 'approval',
    id: 1,
    run: 2,
    command: 'axx run --compact',
    approved: 'services:\n  api: {}\n',
    proposed: 'services:\n  api:\n    volumes: [/tmp:/host-tmp]\n',
    diff: '--- approved\n+++ proposed\n',
    warnings: ['services.api: compose service api mounts /tmp of this machine'],
  });
  assert.deepEqual(events[11], { event: 'answered', id: 1, run: 2, answer: 'approved' });
});

test('parses what a run ended with, its editor, and a changed definition', () => {
  assert.deepEqual(
    parseHostEvent(
      '{"event":"run","run":3,"command":"axx run","ide":"intellij","state":"ended","seconds":0.2,"error":{"code":"AXX-E0501","message":"not approved","hint":"ask them","exit":3}}',
    ),
    {
      event: 'run',
      run: 3,
      command: 'axx run',
      ide: 'intellij',
      state: 'ended',
      services: [],
      seconds: 0.2,
      error: { code: 'AXX-E0501', message: 'not approved', hint: 'ask them', exit: 3 },
    },
  );
  const serving = parseHostEvent('{"event":"serving","addr":"127.0.0.1:1","dir":"/p","services":["api","db"],"warnings":["w"],"changed":true}');
  assert.equal(serving?.event === 'serving' && serving.changed, true);
  assert.deepEqual(serving?.event === 'serving' && serving.sources, []);
});

test('ignores lines that are no event it knows', () => {
  for (const line of [
    '',
    'axx host: serving /p',
    '[]',
    'null',
    '"serving"',
    '{"event":"serving","dir":"/p"}', // no address
    '{"event":"run","command":"axx run","state":"queued"}', // no run
    '{"event":"run","run":0,"state":"queued"}',
    '{"event":"run","run":1.5,"state":"queued"}',
    '{"event":"run","run":1,"state":"paused"}',
    '{"event":"report","line":"x"}',
    '{"event":"approval","run":1,"proposed":"x"}', // no id
    '{"event":"answered","id":1,"answer":"maybe"}',
    '{"event":"future","id":1}',
  ]) {
    assert.equal(parseHostEvent(line), undefined, line);
  }
});

test('answers on one line of JSON', () => {
  assert.equal(answerLine(4, true), '{"approve":4}\n');
  assert.equal(answerLine(4, false), '{"refuse":4}\n');
});

function stateOf(lines: string[]): HostState {
  const state = new HostState();
  for (const line of lines) state.apply(parseHostEvent(line)!);
  return state;
}

test('follows the runs that have not ended, and the questions not answered', () => {
  const state = stateOf(EVENTS.slice(0, 4));
  assert.deepEqual([...state.runs.values()], [
    { run: 1, command: 'axx run --compact', ide: undefined, dir: '/work/parcels', state: 'started', services: ['api'] },
  ]);
  const later = stateOf(EVENTS.slice(0, 11));
  assert.deepEqual([...later.runs.keys()], [2]);
  assert.equal(later.current()?.state, 'approval');
  assert.deepEqual([...later.approvals.keys()], [1]);
  later.apply(parseHostEvent(EVENTS[11])!);
  assert.equal(later.approvals.size, 0);
});

test('a definition that changed waits for an approval until a run starts services', () => {
  const state = stateOf([JSON.stringify({ ...JSON.parse(SERVING), changed: true })]);
  assert.equal(state.changed, true);
  assert.match(hostStatus('serving', state).tooltip, /changed since you last approved one/);
  state.apply(parseHostEvent('{"event":"run","run":1,"command":"axx run","state":"starting","services":["api"]}')!);
  assert.equal(state.changed, false);
});

test('the run that has the services is the current one, else the first that waits', () => {
  const state = stateOf([
    SERVING,
    '{"event":"run","run":5,"command":"axx run features/a","state":"waiting"}',
    '{"event":"run","run":6,"command":"axx run features/b","state":"queued"}',
  ]);
  assert.equal(state.current()?.run, 5);
  state.apply(parseHostEvent('{"event":"run","run":6,"command":"axx run features/b","state":"stopping"}')!);
  assert.equal(state.current()?.run, 6);
  state.apply(parseHostEvent('{"event":"run","run":6,"command":"axx run features/b","state":"ended"}')!);
  state.apply(parseHostEvent('{"event":"run","run":5,"command":"axx run features/a","state":"ended"}')!);
  assert.equal(state.current(), undefined);
});

test('the status bar says what the host does', () => {
  const s = (lines: string[]): string => hostStatus('serving', stateOf(lines)).text;
  assert.equal(s([SERVING]), '$(server-process) axx host: ready');
  assert.equal(s(EVENTS.slice(0, 2)), '$(server-process) axx host: run 1: axx run --compact (queued)');
  assert.equal(s(EVENTS.slice(0, 3)), '$(server-process) axx host: run 1: axx run --compact (starting)');
  assert.equal(s(EVENTS.slice(0, 4)), '$(server-process) axx host: run 1: axx run --compact (running)');
  assert.equal(s(EVENTS.slice(0, 7)), '$(server-process) axx host: run 1: axx run --compact (cleaning up)');
  assert.equal(s(EVENTS.slice(0, 8)), '$(server-process) axx host: ready');
  assert.equal(
    s([SERVING, '{"event":"run","run":7,"command":"axx run --compact --tags @parcels features/parcels/register.feature","state":"started"}']),
    '$(server-process) axx host: run 7: axx run --compact --tags @parcels featu… (running)',
  );

  const asking = hostStatus('serving', stateOf(EVENTS.slice(0, 11)));
  assert.equal(asking.text, '$(warning) axx host: waiting for your approval');
  assert.equal(asking.approval, 1);
  assert.equal(
    asking.tooltip,
    [
      'Serving /work/parcels on 127.0.0.1:51860',
      'Services: api',
      'Sources: compose.yaml',
      'Run 2: axx run --compact (waits for your approval)',
      "Click to answer run 2's approval.",
    ].join('\n'),
  );
  assert.equal(hostStatus('serving', stateOf(EVENTS)).approval, undefined);

  const none = new HostState();
  assert.equal(hostStatus('starting', none).text, '$(loading~spin) axx host: starting');
  assert.equal(hostStatus('stopping', none).text, '$(loading~spin) axx host: stopping');
  assert.equal(hostStatus('stopped', none).text, '$(debug-stop) axx host: stopped');
  const elsewhere = hostStatus('elsewhere', none, 'an axx host already serves /work/parcels (pid 4242, 127.0.0.1:51860)');
  assert.equal(elsewhere.text, '$(terminal) axx host: already running in a terminal');
  assert.match(elsewhere.tooltip, /^an axx host already serves \/work\/parcels/);
  assert.equal(hostStatus('failed', none, 'cannot listen on 127.0.0.1:1').tooltip, 'cannot listen on 127.0.0.1:1\nClick for its log.');
  assert.equal(
    hostStatus('serving', stateOf(EVENTS.slice(0, 4))).tooltip,
    ['Serving /work/parcels on 127.0.0.1:51860', 'Services: api', 'Sources: compose.yaml', 'Run 1: axx run --compact (running)', 'Click for its log.'].join('\n'),
  );
  assert.equal(hostStatus('serving', stateOf([SERVING]), '', 'parcels').text, '$(server-process) axx host (parcels): ready');
});

test("an approval's question has its run and warnings", () => {
  const approval = parseHostEvent(EVENTS[10]) as ApprovalEvent;
  assert.equal(
    approvalMessage(approval),
    'axx host: run 2 (axx run --compact) needs services whose definition changed. Approve? Warnings: services.api: compose service api mounts /tmp of this machine.',
  );
  assert.equal(
    approvalMessage({ ...approval, warnings: [] }),
    'axx host: run 2 (axx run --compact) needs services whose definition changed. Approve?',
  );
});

test('an approval the host asks as it starts has no run', () => {
  const approval = parseHostEvent('{"event":"approval","id":1,"approved":"services: {}\\n","proposed":"services:\\n  api: {}\\n","diff":"","warnings":[]}') as ApprovalEvent;
  assert.equal(approval?.run, 0);
  assert.equal(approvalMessage(approval), "axx host: the services' definition changed since you last approved it. Approve?");
  const state = stateOf([SERVING, '{"event":"approval","id":1,"approved":"","proposed":"services: {}\\n","diff":"","warnings":[]}']);
  assert.equal(hostStatus('serving', state, '').tooltip.split('\n').at(-1), 'Click to answer the approval.');
});

test("a followed run's output says what the host does for it", () => {
  const texts = EVENTS.map(parseHostEvent).flatMap((e) => (e?.event === 'run' && e.run === 1 ? [runStateText(e)] : []));
  assert.deepEqual(texts, [
    'axx run --compact (in /work/parcels)',
    'starting api',
    'ready in 2.0s',
    'stopping the services',
    '1 scenario: 1 passed, 0 failed; stopped and cleaned up in 3.3s',
  ]);
  const run = (fields: string): RunEvent => parseHostEvent(`{"event":"run","run":3,"command":"axx run",${fields}}`) as RunEvent;
  assert.equal(runStateText(run('"state":"waiting"')), 'waits for the run before it to finish with the services');
  assert.match(runStateText(run('"state":"approval"')), /waits for the person running the host to approve it/);
  const refused = run('"state":"ended","error":{"code":"AXX-E0501","message":"the person running the axx host did not approve it","hint":"ask them"}');
  assert.equal(runStateText(refused), 'the person running the axx host did not approve it [AXX-E0501] (ask them)');
  assert.equal(runError(refused), 'the person running the axx host did not approve it [AXX-E0501] (ask them)');
  assert.equal(runError(run('"state":"ended","seconds":1')), undefined);
  assert.equal(
    runStateText(run('"state":"ended","result":{"passed":2,"failed":1,"other":1},"error":{"message":"cleanup failed"}')),
    '4 scenarios: 2 passed, 1 failed; cleanup failed',
  );
});

test("maps a run's files from its project directory to this one", () => {
  const here = path.join(path.sep, 'home', 'ana', 'parcels');
  const file = path.join(here, 'features', 'health.feature');
  assert.equal(mapRunFile('/workspace/parcels/features/health.feature', '/workspace/parcels', here), file);
  assert.equal(mapRunFile('/workspace/parcels/features/health.feature', '/workspace/parcels/', here), file);
  assert.equal(mapRunFile('\\workspace\\parcels\\features\\health.feature', '/workspace/parcels', here), file); // read on Windows
  assert.equal(mapRunFile('/workspace/parcels', '/workspace/parcels', here), here);
  // The same directory, none, or files elsewhere: as they are.
  assert.equal(mapRunFile(file, here, here), file);
  assert.equal(mapRunFile('/workspace/parcels/features/a.feature', undefined, here), '/workspace/parcels/features/a.feature');
  assert.equal(mapRunFile('/workspace/parcels2/features/a.feature', '/workspace/parcels', here), '/workspace/parcels2/features/a.feature');
  assert.equal(mapRunFile('/elsewhere/a.feature', '/workspace/parcels', here), '/elsewhere/a.feature');
});

test('finds the services an axx.yaml declares', () => {
  assert.deepEqual(
    mappingKeys(
      ['version: 1', 'services:', '  # the parcels api', '  api:', '    command: docker compose up', '  "db": {command: x}', 'profiles:', '  web: {}'],
      'services',
    ),
    ['api', 'db'],
  );
  assert.deepEqual(mappingKeys(['services: {api: {command: x}, db: {}}  # two'], 'services'), ['api', 'db']);
  assert.deepEqual(mappingKeys(['version: 1', 'run:', '  services: [api]'], 'services'), []);

  const project = (files: Record<string, string>): string => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'axx-host-'));
    for (const [name, content] of Object.entries(files)) fs.writeFileSync(path.join(dir, name), content);
    return dir;
  };
  assert.equal(declaresServices(project({ 'axx.yaml': 'services:\n  api:\n    command: ./api\n' })), true);
  assert.equal(declaresServices(project({ 'axx.yml': 'services: {api: {command: ./api}}\n' })), true);
  assert.equal(declaresServices(project({ 'axx.yaml': 'version: 1\nrun:\n  paths: [features]\n' })), false);
  assert.equal(declaresServices(project({ 'axx.yaml': 'services: {}\n' })), false);
  assert.equal(declaresServices(project({})), false);
});

test('reads the error the host stopped on from its log', () => {
  assert.deepEqual(
    parseHostError(
      [
        'time=12:00:01 level=INFO msg="starting"',
        'error[AXX-E0503]: an axx host already serves /work/parcels (pid 4242, 127.0.0.1:51860)',
        '  hint: its runs are yours too: stop it first to start another',
        '  docs: https://axx.nimbusxr.us/reference/errors/#axx-e0503',
      ].join('\n'),
    ),
    {
      code: 'AXX-E0503',
      message: 'an axx host already serves /work/parcels (pid 4242, 127.0.0.1:51860)',
      hint: 'its runs are yours too: stop it first to start another',
    },
  );
  assert.deepEqual(parseHostError('error: no axx.yaml here\n'), { message: 'no axx.yaml here' });
  assert.equal(parseHostError('level=INFO msg="api: ready"\n'), undefined);
});

// --- The process ---

// A child process whose pipes the test holds.
class FakeChild extends EventEmitter {
  readonly stdin = new PassThrough();
  readonly stdout = new PassThrough();
  readonly stderr = new PassThrough();
  readonly pid = 4242;
  exitCode: number | null = null;
  signalCode: NodeJS.Signals | null = null;
  received = '';

  constructor() {
    super();
    this.stdin.setEncoding('utf8');
    this.stdin.on('data', (chunk: string) => (this.received += chunk));
  }

  exit(code: number | null): void {
    this.exitCode = code;
    this.stdout.end();
    this.stderr.end();
    this.emit('exit', code, null);
  }
}

function listen(): { events: HostEvent[]; log: string[]; exits: HostExit[]; listener: ConstructorParameters<typeof HostProcess>[1] } {
  const events: HostEvent[] = [];
  const log: string[] = [];
  const exits: HostExit[] = [];
  return { events, log, exits, listener: { event: (e) => events.push(e), log: (line) => log.push(line), exited: (e) => exits.push(e) } };
}

const tick = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

test('hands the events and the log on, answers, and stops by closing stdin', async () => {
  const child = new FakeChild();
  const { events, log, exits, listener } = listen();
  let killed = false;
  const host = new HostProcess(child as unknown as ChildProcess, listener, { kill: () => (killed = true) });
  child.stdout.write(`${EVENTS.slice(0, 3).join('\n')}\nnot an event\n`);
  child.stderr.write('level=INFO msg="api: starting"\n');
  child.stdout.write(`${EVENTS[10]}\n`);
  await tick();
  assert.deepEqual(
    events.map((e) => e.event),
    ['serving', 'run', 'run', 'approval'],
  );
  assert.deepEqual(log, ['not an event', 'level=INFO msg="api: starting"']);

  assert.equal(host.answer(1, true), true);
  assert.equal(host.answer(2, false), true);
  await tick();
  assert.equal(child.received, '{"approve":1}\n{"refuse":2}\n');

  host.stop();
  assert.equal(host.stopping, true);
  assert.equal(child.stdin.writableEnded, true);
  assert.equal(host.answer(1, true), false); // no answers once it stops
  child.exit(0);
  assert.deepEqual(await host.exited, { code: 0, served: true });
  assert.deepEqual(exits, [{ code: 0, served: true }]);
  assert.equal(killed, false);
});

test('kills a host that does not stop in time', async () => {
  const child = new FakeChild();
  const { listener } = listen();
  let killed: unknown;
  const host = new HostProcess(child as unknown as ChildProcess, listener, {
    stopMs: 10,
    kill: (c) => {
      killed = c;
      child.signalCode = 'SIGKILL';
      child.exit(null);
    },
  });
  host.stop();
  host.stop(); // once
  const exit = await host.exited;
  assert.equal(killed, child);
  assert.deepEqual(exit, { code: null, served: false });
});

test('says which error a host that did not start stopped on', async () => {
  const child = new FakeChild();
  const { events, listener } = listen();
  const host = new HostProcess(child as unknown as ChildProcess, listener);
  child.stderr.write('error[AXX-E0503]: an axx host already serves /work/parcels (pid 4242, 127.0.0.1:51860)\n');
  child.stderr.write('  hint: its runs are yours too: stop it first to start another\n');
  await tick();
  child.exit(1);
  assert.deepEqual(await host.exited, {
    code: 1,
    served: false,
    error: {
      code: 'AXX-E0503',
      message: 'an axx host already serves /work/parcels (pid 4242, 127.0.0.1:51860)',
      hint: 'its runs are yours too: stop it first to start another',
    },
  });
  assert.equal(events.length, 0);
});

test('stopping a host that exited does nothing', async () => {
  const child = new FakeChild();
  const { listener } = listen();
  let killed = false;
  const host = new HostProcess(child as unknown as ChildProcess, listener, { stopMs: 1, kill: () => (killed = true) });
  child.exit(2);
  await host.exited;
  host.stop();
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.equal(killed, false);
});

// A host in a real process: it announces itself, answers an approval, and stops when its stdin
// closes, as `axx host --ide` does.
const SCRIPT = `
const rl = require('node:readline').createInterface({ input: process.stdin });
console.log(JSON.stringify({ event: 'serving', addr: '127.0.0.1:1', dir: process.cwd(), services: ['api'] }));
console.log(JSON.stringify({ event: 'approval', id: 1, run: 1, command: 'axx run', proposed: 'services: {}' }));
rl.on('line', (line) => {
  const answer = JSON.parse(line);
  console.log(JSON.stringify({ event: 'answered', id: answer.approve ?? answer.refuse, run: 1, answer: answer.approve ? 'approved' : 'refused' }));
});
rl.on('close', () => {
  console.error('stopping api');
  process.exit(0);
});
`;

test('talks to a host process over its pipes', async () => {
  const child = spawn(process.execPath, ['-e', SCRIPT], { stdio: ['pipe', 'pipe', 'pipe'] });
  const { events, log, listener } = listen();
  const host = new HostProcess(child, listener);
  while (!events.some((e) => e.event === 'approval')) await tick();
  host.answer(1, false);
  while (!events.some((e) => e.event === 'answered')) await tick();
  host.stop();
  assert.deepEqual(await host.exited, { code: 0, served: true });
  assert.deepEqual(
    events.map((e) => (e.event === 'answered' ? `${e.event} ${e.answer}` : e.event)),
    ['serving', 'approval', 'answered refused'],
  );
  assert.deepEqual(log, ['stopping api']);
});

test("a host that can't start says why", async () => {
  const { listener } = listen();
  const child = spawn(path.join(os.tmpdir(), 'axx-host-missing', 'axx'), ['host', '--ide'], { stdio: ['pipe', 'pipe', 'pipe'] });
  const exit = await new HostProcess(child, listener).exited;
  assert.equal(exit.code, null);
  assert.match(exit.spawnError ?? '', /ENOENT/);
});

// SPDX-License-Identifier: Apache-2.0
// The axx host without VS Code: its process, its events, what the editor shows of its state, and
// where the files of the runs it serves are here (see axxHost.ts for what the editor does with it).
//
// `axx host` starts a project's services for the runs that cannot start them themselves, such as an
// agent's in a sandbox without Docker, on the person's machine. Started with --ide, it writes its
// events on stdout, one JSON object per line:
//   {"event":"serving","addr":"127.0.0.1:51860","dir":"/p","services":["api"],"sources":["compose.yaml"],"warnings":[...],"changed":true}
//   {"event":"run","run":4,"command":"axx run --compact","ide":"vscode","dir":"/p","state":"queued"}
//   {"event":"report","run":4,"line":"##teamcity[testStarted ...]"}
//   {"event":"approval","id":1,"run":4,"command":"axx run --compact","approved":"...","proposed":"...","diff":"...","warnings":[...]}
//   {"event":"answered","id":1,"run":4,"answer":"approved"}
// A run goes queued > (waiting) > (approval) > starting > started > stopping > ended; `ide` is the
// AXX_IDE of the process that started it (none for an agent's or a terminal's), and `dir`, on
// queued, its project directory as it sees it, which its report's locations are in. `report` is a
// line of its TeamCity report. An approval asks whether runs may start services whose definition
// changed since the person last approved one: for a run, or (no run) as the host starts. It stays
// open until the person answers, whether or not a run still waits; `answered` says how it ended
// (approved, refused, or abandoned when a newer change replaced it or the host stopped). Fields
// left out are empty.
//
// The host reads the answers on stdin, {"approve":1} or {"refuse":1}. Only the editor that started
// it holds that pipe; the host's TCP port, which runs (and agents) reach, never takes one. Closing
// stdin stops the host: it stops the services of the run in progress, cleans up, and exits. Its log
// is on stderr, and an error it stops on is its last lines there (`error[AXX-E0503]: ...`).

import { spawn, type ChildProcess } from 'node:child_process';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as readline from 'node:readline';
import { killProcessTree } from './processTree';
import { mappingKeys } from './profiles';

// How long the host may take to stop its services and clean up once its stdin closes; then it is
// killed.
export const STOP_MS = 60_000;

// The error the host stops on when another one already serves the project: one started in a
// terminal, whose runs are the editor's too.
export const ALREADY_SERVED = 'AXX-E0503';

const CONFIG_NAMES = ['axx.yaml', 'axx.yml'];
const LOG_LINES = 50; // the last lines of its log kept for the error it stops on
const COMMAND_LENGTH = 40; // of a run's command in the status bar

// --- The events ---

export type RunState = 'queued' | 'waiting' | 'approval' | 'starting' | 'started' | 'stopping' | 'ended';

const RUN_STATES: readonly string[] = ['queued', 'waiting', 'approval', 'starting', 'started', 'stopping', 'ended'];

export interface HostFailure {
  code?: string;
  message: string;
  hint?: string;
  exit?: number;
}

export interface HostResult {
  passed: number;
  failed: number;
  other: number;
}

export interface ServingEvent {
  event: 'serving';
  addr: string;
  dir: string;
  services: string[];
  sources: string[];
  warnings: string[];
  changed: boolean; // the definition changed since the person last approved one: the host asks about it as it starts
}

export interface RunEvent {
  event: 'run';
  run: number;
  command: string;
  ide?: string;
  dir?: string;
  state: RunState;
  services: string[];
  seconds?: number; // started: how long they took to be ready; ended: to stop and clean up
  error?: HostFailure;
  result?: HostResult;
}

export interface ReportEvent {
  event: 'report';
  run: number;
  line: string;
}

export interface ApprovalEvent {
  event: 'approval';
  id: number;
  run: number;
  command: string;
  approved: string; // the last approved definition ('' when there is none)
  proposed: string;
  diff: string;
  warnings: string[];
}

export interface AnsweredEvent {
  event: 'answered';
  id: number;
  run: number;
  answer: 'approved' | 'refused' | 'abandoned';
}

export type HostEvent = ServingEvent | RunEvent | ReportEvent | ApprovalEvent | AnsweredEvent;

// Parses a line of the host's stdout; undefined when it is not an event the editor knows.
export function parseHostEvent(line: string): HostEvent | undefined {
  let value: unknown;
  try {
    value = JSON.parse(line);
  } catch {
    return undefined;
  }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined;
  const o = value as Record<string, unknown>;
  const text = (key: string): string => (typeof o[key] === 'string' ? (o[key] as string) : '');
  const id = (key: string): number => (Number.isInteger(o[key]) && (o[key] as number) > 0 ? (o[key] as number) : 0);
  const texts = (key: string): string[] => (Array.isArray(o[key]) ? (o[key] as unknown[]).filter((s): s is string => typeof s === 'string') : []);
  switch (o.event) {
    case 'serving':
      if (!text('addr') || !text('dir')) return undefined;
      return {
        event: 'serving',
        addr: text('addr'),
        dir: text('dir'),
        services: texts('services'),
        sources: texts('sources'),
        warnings: texts('warnings'),
        changed: o.changed === true,
      };
    case 'run': {
      const state = text('state');
      if (!id('run') || !RUN_STATES.includes(state)) return undefined;
      const e: RunEvent = { event: 'run', run: id('run'), command: text('command'), state: state as RunState, services: texts('services') };
      if (text('ide')) e.ide = text('ide');
      if (text('dir')) e.dir = text('dir');
      if (typeof o.seconds === 'number' && Number.isFinite(o.seconds)) e.seconds = o.seconds;
      const error = failureOf(o.error);
      if (error) e.error = error;
      const result = resultOf(o.result);
      if (result) e.result = result;
      return e;
    }
    case 'report':
      return id('run') ? { event: 'report', run: id('run'), line: text('line') } : undefined;
    case 'approval':
      if (!id('id')) return undefined; // no run: the host asks as it starts
      return {
        event: 'approval',
        id: id('id'),
        run: id('run'),
        command: text('command'),
        approved: text('approved'),
        proposed: text('proposed'),
        diff: text('diff'),
        warnings: texts('warnings'),
      };
    case 'answered': {
      const answer = text('answer');
      if (!id('id') || !['approved', 'refused', 'abandoned'].includes(answer)) return undefined;
      return { event: 'answered', id: id('id'), run: id('run'), answer: answer as AnsweredEvent['answer'] };
    }
    default:
      return undefined;
  }
}

function failureOf(value: unknown): HostFailure | undefined {
  if (typeof value !== 'object' || value === null) return undefined;
  const o = value as Record<string, unknown>;
  if (typeof o.message !== 'string') return undefined;
  const f: HostFailure = { message: o.message };
  if (typeof o.code === 'string' && o.code) f.code = o.code;
  if (typeof o.hint === 'string' && o.hint) f.hint = o.hint;
  if (Number.isInteger(o.exit)) f.exit = o.exit as number;
  return f;
}

function resultOf(value: unknown): HostResult | undefined {
  if (typeof value !== 'object' || value === null) return undefined;
  const o = value as Record<string, unknown>;
  const n = (key: string): number => (Number.isInteger(o[key]) ? (o[key] as number) : 0);
  return { passed: n('passed'), failed: n('failed'), other: n('other') };
}

// The line that answers an approval on the host's stdin.
export function answerLine(id: number, approve: boolean): string {
  return `${JSON.stringify(approve ? { approve: id } : { refuse: id })}\n`;
}

// --- The state ---

// A run the host serves that has not ended, as its events say.
export interface HostRun {
  run: number;
  command: string;
  ide?: string;
  dir?: string;
  state: RunState;
  services: string[];
}

// What the editor knows of a host from its events: where it serves, the runs that have not ended,
// in their order, and the questions not answered yet.
export class HostState {
  serving: ServingEvent | undefined;
  changed = false; // the definition that started it waits for an approval
  readonly runs = new Map<number, HostRun>();
  readonly approvals = new Map<number, ApprovalEvent>(); // by id

  apply(e: HostEvent): void {
    switch (e.event) {
      case 'serving':
        this.serving = e;
        this.changed = e.changed;
        break;
      case 'run': {
        if (e.state === 'ended') {
          this.runs.delete(e.run);
          break;
        }
        if (e.state === 'starting') this.changed = false; // its definition is approved
        const before = this.runs.get(e.run);
        this.runs.set(e.run, {
          run: e.run,
          command: e.command || (before?.command ?? ''),
          ide: e.ide ?? before?.ide,
          dir: e.dir ?? before?.dir,
          state: e.state,
          services: e.services.length > 0 ? e.services : (before?.services ?? []),
        });
        break;
      }
      case 'approval':
        this.approvals.set(e.id, e);
        break;
      case 'answered':
        this.approvals.delete(e.id);
        break;
    }
  }

  // The run that has the services, or else the first that waits for them.
  current(): HostRun | undefined {
    const runs = [...this.runs.values()];
    return runs.find((r) => r.state !== 'queued' && r.state !== 'waiting') ?? runs[0];
  }
}

// --- What the editor shows ---

// What the host's process does: it starts, serves, stops (its stdin closed), has stopped, did not
// start because one started in a terminal serves the project already, or failed.
export type HostPhase = 'starting' | 'serving' | 'stopping' | 'stopped' | 'elsewhere' | 'failed';

export interface HostStatus {
  text: string; // for the status bar, with its icon
  tooltip: string;
  approval?: number; // the question clicking it shows again
}

const STATE_WORDS: Record<RunState, string> = {
  queued: 'queued',
  waiting: 'waits for its turn',
  approval: 'waits for your approval',
  starting: 'starting',
  started: 'running',
  stopping: 'cleaning up',
  ended: 'ended',
};

// The status bar item of a host. `detail` says why it stopped, failed or serves elsewhere; `name`
// tells hosts apart when the editor has several.
export function hostStatus(phase: HostPhase, state: HostState, detail = '', name?: string): HostStatus {
  const title = name ? `axx host (${name})` : 'axx host';
  switch (phase) {
    case 'starting':
      return { text: `$(loading~spin) ${title}: starting`, tooltip: 'Starting axx host --ide' };
    case 'stopping':
      return { text: `$(loading~spin) ${title}: stopping`, tooltip: 'Stopping the services of the run in progress and cleaning up' };
    case 'stopped':
      return { text: `$(debug-stop) ${title}: stopped`, tooltip: ['The axx host stopped.', detail, 'Click to start it again.'].filter(Boolean).join('\n') };
    case 'elsewhere':
      return {
        text: `$(terminal) ${title}: already running in a terminal`,
        tooltip: [detail, 'Its runs are yours too; it asks for approvals in its terminal.'].filter(Boolean).join('\n'),
      };
    case 'failed':
      return { text: `$(error) ${title}: failed`, tooltip: `${detail || 'The axx host stopped on an error.'}\nClick for its log.` };
  }
  const lines: string[] = [];
  const s = state.serving;
  if (s) {
    lines.push(`Serving ${s.dir} on ${s.addr}`, `Services: ${s.services.join(', ')}`);
    if (s.sources.length > 0) lines.push(`Sources: ${s.sources.join(', ')}`);
    if (state.changed) lines.push("The services' definition changed since you last approved one: the host asks you about it.");
  }
  for (const r of state.runs.values()) lines.push(`Run ${r.run}: ${r.command} (${STATE_WORDS[r.state]})`);
  const [approval] = state.approvals.values();
  if (approval) {
    lines.push(approval.run ? `Click to answer run ${approval.run}'s approval.` : 'Click to answer the approval.');
    return { text: `$(warning) ${title}: waiting for your approval`, tooltip: lines.join('\n'), approval: approval.id };
  }
  lines.push('Click for its log.');
  const run = state.current();
  const text = run ? `run ${run.run}: ${shorten(run.command, COMMAND_LENGTH)} (${STATE_WORDS[run.state]})` : 'ready';
  return { text: `$(server-process) ${title}: ${text}`, tooltip: lines.join('\n') };
}

// The question of an approval's notification.
export function approvalMessage(a: ApprovalEvent): string {
  const warnings = a.warnings.length > 0 ? ` Warnings: ${a.warnings.join('; ')}.` : '';
  if (!a.run) return `axx host: the services' definition changed since you last approved it. Approve?${warnings}`;
  return `axx host: run ${a.run} (${a.command}) needs services whose definition changed. Approve?${warnings}`;
}

// What a run's state says, after `run 4: `, as the host's terminal says it.
export function runStateText(e: RunEvent): string {
  switch (e.state) {
    case 'queued':
      return `${e.command}${e.dir ? ` (in ${e.dir})` : ''}`;
    case 'waiting':
      return 'waits for the run before it to finish with the services';
    case 'approval':
      return "the services' definition changed: waits for the person running the host to approve it";
    case 'starting':
      return `starting ${e.services.join(', ') || 'the services'}`;
    case 'started':
      return `ready in ${seconds(e.seconds)}`;
    case 'stopping':
      return 'stopping the services';
    case 'ended': {
      const parts: string[] = [];
      const r = e.result;
      if (r) {
        const n = r.passed + r.failed + r.other;
        parts.push(`${n} ${n === 1 ? 'scenario' : 'scenarios'}: ${r.passed} passed, ${r.failed} failed`);
      }
      parts.push(e.error ? failureText(e.error) : `stopped and cleaned up in ${seconds(e.seconds)}`);
      return parts.join('; ');
    }
  }
}

// The error a run that ended has, for its items that did not finish.
export function runError(e: RunEvent): string | undefined {
  return e.error ? failureText(e.error) : undefined;
}

function failureText(f: HostFailure): string {
  return `${f.message}${f.code ? ` [${f.code}]` : ''}${f.hint ? ` (${f.hint})` : ''}`;
}

function seconds(s: number | undefined): string {
  return `${(s ?? 0).toFixed(1)}s`;
}

function shorten(text: string, length: number): string {
  return text.length > length ? `${text.slice(0, length - 1)}…` : text;
}

// A file of a run's report as it is here: under `to`, the project directory, when it is under
// `from`, the run's project directory as the run sees it (a sandbox may mount the project
// elsewhere); else as it is.
export function mapRunFile(file: string, from: string | undefined, to: string): string {
  if (!from) return file;
  const slashed = (p: string): string => p.replace(/\\/g, '/').replace(/(.)\/+$/, '$1');
  const f = slashed(file);
  const base = slashed(from);
  if (base === slashed(to)) return file;
  if (f === base) return to;
  const prefix = base.endsWith('/') ? base : `${base}/`;
  if (!f.startsWith(prefix)) return file;
  return path.join(to, ...f.slice(prefix.length).split('/'));
}

// Whether the axx project in `dir` declares services in its axx.yaml: an axx host has nothing to
// start without.
export function declaresServices(dir: string): boolean {
  for (const name of CONFIG_NAMES) {
    let text: string;
    try {
      text = fs.readFileSync(path.join(dir, name), 'utf8');
    } catch {
      continue;
    }
    return mappingKeys(text.split(/\r?\n/), 'services').length > 0;
  }
  return false;
}

// --- The process ---

// An error the host stopped on, from its log.
export interface HostError {
  code?: string;
  message: string;
  hint?: string;
}

// How the host's process ended.
export interface HostExit {
  code: number | null; // null when it was killed, or did not start
  served: boolean; // whether it got to serve
  error?: HostError; // the error it stopped on
  spawnError?: string; // why it did not start
}

export interface HostListener {
  event(e: HostEvent): void;
  log(line: string): void; // a line of its log (stderr), or one of stdout that is no event
  exited(exit: HostExit): void;
}

// The last error a log has, as axx prints errors:
//   error[AXX-E0503]: an axx host already serves /p (pid 123, 127.0.0.1:51860)
//     hint: its runs are yours too: stop it first to start another
export function parseHostError(log: string): HostError | undefined {
  const lines = log.split(/\r?\n/);
  for (let i = lines.length - 1; i >= 0; i--) {
    const m = /^error(?:\[([A-Z]+-[A-Z]?\d+)\])?: (.*)$/.exec(lines[i]);
    if (!m) continue;
    const error: HostError = { message: m[2] };
    if (m[1]) error.code = m[1];
    const hint = /^\s+hint: (.*)$/.exec(lines[i + 1] ?? '');
    if (hint) error.hint = hint[1];
    return error;
  }
  return undefined;
}

// A running `axx host --ide`: it hands its events and log to a listener, writes the editor's
// answers to its stdin, and stops when that closes.
export class HostProcess {
  readonly exited: Promise<HostExit>;
  private served = false;
  private closing = false;
  private done = false; // it exited
  private killTimer: ReturnType<typeof setTimeout> | undefined;
  private readonly log: string[] = [];

  constructor(
    private readonly child: ChildProcess,
    listener: HostListener,
    private readonly options: { stopMs?: number; kill?: (child: ChildProcess) => void } = {},
  ) {
    const out = readline.createInterface({ input: child.stdout!, crlfDelay: Infinity });
    const err = readline.createInterface({ input: child.stderr!, crlfDelay: Infinity });
    out.on('line', (line) => {
      const e = parseHostEvent(line);
      if (!e) {
        if (line.trim() !== '') listener.log(line);
        return;
      }
      if (e.event === 'serving') this.served = true;
      listener.event(e);
    });
    err.on('line', (line) => {
      this.log.push(line);
      if (this.log.length > LOG_LINES) this.log.shift();
      listener.log(line);
    });
    child.stdin?.on('error', () => undefined); // a write after it exited
    const drained = Promise.all([out, err].map((lines) => new Promise<void>((resolve) => lines.once('close', resolve))));
    this.exited = (async () => {
      const ended = await new Promise<{ code: number | null; spawnError?: string }>((resolve) => {
        child.once('exit', (code) => resolve({ code }));
        child.once('error', (e) => {
          if (child.pid === undefined) resolve({ code: null, spawnError: e.message });
        });
      });
      this.done = true;
      clearTimeout(this.killTimer);
      // What it started could hold its pipes open; don't wait for that.
      await Promise.race([drained, new Promise((resolve) => setTimeout(resolve, 2000))]);
      out.close();
      err.close();
      const exit: HostExit = { code: ended.code, served: this.served };
      const error = parseHostError(this.log.join('\n'));
      if (error) exit.error = error;
      if (ended.spawnError) exit.spawnError = ended.spawnError;
      listener.exited(exit);
      return exit;
    })();
  }

  // Whether it was asked to stop.
  get stopping(): boolean {
    return this.closing;
  }

  // Answers an approval. Returns false when the host can no longer read answers.
  answer(id: number, approve: boolean): boolean {
    const stdin = this.child.stdin;
    if (!stdin || this.closing || stdin.destroyed || stdin.writableEnded) return false;
    stdin.write(answerLine(id, approve));
    return true;
  }

  // Closes its stdin: it stops the services of the run in progress, cleans up and exits. It is
  // killed, with everything it started, if it has not exited after STOP_MS.
  stop(): void {
    if (this.closing) return;
    this.closing = true;
    if (this.done) return;
    this.child.stdin?.end();
    const kill = this.options.kill ?? killProcessTree;
    this.killTimer = setTimeout(() => kill(this.child), this.options.stopMs ?? STOP_MS);
  }
}

// Starts `axx host --ide` in an axx project's directory.
export function startHost(axx: string, dir: string, listener: HostListener): HostProcess {
  const child = spawn(axx, ['host', '--ide'], {
    cwd: dir,
    detached: process.platform !== 'win32', // its own process group, to kill it with everything it started
    stdio: ['pipe', 'pipe', 'pipe'],
  });
  return new HostProcess(child, listener);
}

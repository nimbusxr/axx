// SPDX-License-Identifier: Apache-2.0
// Reads the TeamCity service messages that `axx run --format teamcity` prints (the ID-based tree
// form: a suite per feature, a suite per scenario or example row, a test per step). Scenarios run
// in parallel, so messages of different nodes interleave; node ids keep them apart.

export interface ServiceMessage {
  name: string;
  attrs: Record<string, string>;
  value?: string; // the single-value form: ##teamcity[name 'value']
}

const PREFIX = '##teamcity[';

// Parses one line; undefined when it is not a service message.
export function parseServiceMessage(line: string): ServiceMessage | undefined {
  const s = line.trim();
  if (!s.startsWith(PREFIX) || !s.endsWith(']')) return undefined;
  const body = s.slice(PREFIX.length, -1);
  const name = /^[^\s']+/.exec(body)?.[0];
  if (!name) return undefined;
  const msg: ServiceMessage = { name, attrs: {} };
  let i = name.length;
  while (i < body.length) {
    while (i < body.length && /\s/.test(body[i])) i++;
    if (i >= body.length) break;
    if (body[i] === "'") {
      const [raw, next] = quoted(body, i);
      if (next < 0) return undefined;
      msg.value = unescapeValue(raw);
      i = next;
      continue;
    }
    const eq = body.indexOf('=', i);
    if (eq < 0 || body[eq + 1] !== "'") return undefined;
    const key = body.slice(i, eq).trim();
    const [raw, next] = quoted(body, eq + 1);
    if (next < 0) return undefined;
    msg.attrs[key] = unescapeValue(raw);
    i = next;
  }
  return msg;
}

// Returns the raw text of the quoted value that starts at `open`, and the index after it
// (-1 when it is not terminated).
function quoted(s: string, open: number): [string, number] {
  for (let j = open + 1; j < s.length; j++) {
    if (s[j] === '|') {
      j++; // the escaped character (for |0xNNNN, the hex digits are no quotes)
    } else if (s[j] === "'") {
      return [s.slice(open + 1, j), j + 1];
    }
  }
  return ['', -1];
}

// Undoes TeamCity escaping: || |' |n |r |[ |] |x (U+0085) |l (U+2028) |p (U+2029) |0xNNNN.
export function unescapeValue(s: string): string {
  let out = '';
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c !== '|' || i + 1 >= s.length) {
      out += c;
      continue;
    }
    const n = s[++i];
    switch (n) {
      case 'n':
        out += '\n';
        break;
      case 'r':
        out += '\r';
        break;
      case 'x':
        out += '\u0085';
        break;
      case 'l':
        out += ' ';
        break;
      case 'p':
        out += ' ';
        break;
      case '0': {
        const hex = /^x([0-9a-fA-F]{4})/.exec(s.slice(i + 1));
        if (hex) {
          out += String.fromCharCode(parseInt(hex[1], 16));
          i += 5;
        } else {
          out += n;
        }
        break;
      }
      default:
        out += n; // | ' [ ]
    }
  }
  return out;
}

export interface Location {
  file: string; // a file system path
  line: number; // 1-based
}

// Parses a locationHint of the form file:///abs/path.feature:12.
export function parseLocationHint(hint: string | undefined, windows = process.platform === 'win32'): Location | undefined {
  const m = hint ? /^file:\/\/(.+):(\d+)$/.exec(hint) : null;
  if (!m) return undefined;
  let file = m[1];
  if (windows) {
    if (/^\/[A-Za-z]:/.test(file)) file = file.slice(1);
    file = file.replace(/\//g, '\\');
  }
  return { file, line: Number(m[2]) };
}

export type Outcome = 'passed' | 'failed' | 'skipped';

export interface Failure {
  message: string;
  details: string;
  expected?: string; // set for comparison failures, with actual
  actual?: string;
}

export interface TcNode {
  id: string;
  parentId: string; // '0' for a top-level suite
  kind: 'suite' | 'test';
  name: string;
  location?: Location;
  // Tests: the result, once known.
  outcome?: Outcome;
  failure?: Failure;
  ignored?: string; // the testIgnored message
  durationMs?: number;
  // Suites: the finished tests below them.
  counts: Record<Outcome, number>;
}

export interface TeamCityEvents {
  suiteStarted?(node: TcNode): void;
  // A suite failed if a test below it failed, else skipped if one was skipped, else passed.
  suiteFinished?(node: TcNode, outcome: Outcome): void;
  testStarted?(node: TcNode): void;
  testOutput?(node: TcNode, text: string): void;
  testFinished?(node: TcNode): void;
  output?(line: string): void; // a line that is not a service message
}

export class TeamCityReader {
  private readonly nodes = new Map<string, TcNode>();
  private readonly events: TeamCityEvents;
  private readonly windows: boolean;

  constructor(events: TeamCityEvents, windows = process.platform === 'win32') {
    this.events = events;
    this.windows = windows;
  }

  node(id: string): TcNode | undefined {
    return this.nodes.get(id);
  }

  // Feeds one line of output (without its line break).
  line(text: string): void {
    const msg = parseServiceMessage(text);
    if (msg) {
      this.message(msg);
    } else {
      this.events.output?.(text);
    }
  }

  private message({ name, attrs }: ServiceMessage): void {
    const id = attrs.nodeId;
    if (id === undefined) return; // enteredTheMatrix, testingStarted, testingFinished, ...
    switch (name) {
      case 'testSuiteStarted': {
        const node = this.create(id, 'suite', attrs); // not inside ?.(): it would not run without a listener
        this.events.suiteStarted?.(node);
        break;
      }
      case 'testSuiteFinished': {
        const node = this.nodes.get(id);
        if (node) this.events.suiteFinished?.(node, suiteOutcome(node));
        break;
      }
      case 'testStarted': {
        const node = this.create(id, 'test', attrs);
        this.events.testStarted?.(node);
        break;
      }
      case 'testStdOut':
      case 'testStdErr': {
        const node = this.nodes.get(id);
        if (node) this.events.testOutput?.(node, attrs.out ?? '');
        break;
      }
      case 'testFailed': {
        const node = this.nodes.get(id);
        if (!node) break;
        node.outcome = 'failed';
        node.failure = { message: attrs.message ?? '', details: attrs.details ?? '' };
        if (attrs.type === 'comparisonFailure' && attrs.expected !== undefined && attrs.actual !== undefined) {
          node.failure.expected = attrs.expected;
          node.failure.actual = attrs.actual;
        }
        break;
      }
      case 'testIgnored': {
        const node = this.nodes.get(id);
        if (!node || node.outcome === 'failed') break;
        node.outcome = 'skipped';
        node.ignored = attrs.message ?? '';
        break;
      }
      case 'testFinished': {
        const node = this.nodes.get(id);
        if (!node) break;
        node.outcome ??= 'passed';
        const ms = Number(attrs.duration);
        if (attrs.duration !== undefined && Number.isFinite(ms)) node.durationMs = ms;
        for (let p = this.nodes.get(node.parentId); p; p = this.nodes.get(p.parentId)) {
          p.counts[node.outcome]++;
          if (p.parentId === p.id) break;
        }
        this.events.testFinished?.(node);
        break;
      }
    }
  }

  private create(id: string, kind: TcNode['kind'], attrs: Record<string, string>): TcNode {
    const node: TcNode = {
      id,
      parentId: attrs.parentNodeId ?? '0',
      kind,
      name: attrs.name ?? '',
      location: parseLocationHint(attrs.locationHint, this.windows),
      counts: { passed: 0, failed: 0, skipped: 0 },
    };
    this.nodes.set(id, node);
    return node;
  }
}

function suiteOutcome(node: TcNode): Outcome {
  if (node.counts.failed > 0) return 'failed';
  if (node.counts.skipped > 0) return 'skipped';
  return 'passed';
}

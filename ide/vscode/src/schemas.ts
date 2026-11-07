// SPDX-License-Identifier: Apache-2.0
// The schemas of the axx a project runs on, as `axx schema` prints them, for axx.yaml and the
// fixture spec files: the YAML extension completes and checks them with these (yamlSchemas.ts), so
// they match the axx that reads the files, whichever build of this extension is installed. The axx
// a project runs on is the one axx.path names, run in the folder of the project's axx.yaml: it
// hands over to the version that axx.yaml names, when it names one. Until axx has given them, and
// when it can't, the schemas in this extension are used.

import { execFile } from 'node:child_process';
import * as crypto from 'node:crypto';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';

// The URI scheme the YAML extension asks this extension for schemas by.
export const SCHEME = 'axx-schema';

// How long axx may take to answer: asked first for a version a project names, it downloads it.
const TIMEOUT_MS = 120_000;

export interface Kind {
  kind: string; // as `axx schema` names it
  file: string; // the file in schemas/v0, and in this extension
  names: RegExp; // the YAML files it governs
}

export const KINDS: Kind[] = [
  { kind: 'config', file: 'axx.schema.json', names: /^axx(\.[^.]+)?\.ya?ml$/ },
  { kind: 'factory', file: 'axx-factory.schema.json', names: /^.+\.factory\.ya?ml$/ },
  { kind: 'fixture', file: 'axx-fixture.schema.json', names: /^.+\.fixture\.ya?ml$/ },
  { kind: 'prototype', file: 'axx-prototype.schema.json', names: /^.+\.prototype\.ya?ml$/ },
];

// The schema that governs a file, by its name, or undefined.
export function kindOf(fileName: string): Kind | undefined {
  return KINDS.find((k) => k.names.test(fileName));
}

// The folder of the axx.yaml a file belongs to: the nearest one above it, up to the repository
// root, as axx finds it.
export function configDir(file: string): string | undefined {
  let dir = path.dirname(path.resolve(file));
  for (;;) {
    if (['axx.yaml', 'axx.yml'].some((n) => fs.existsSync(path.join(dir, n)))) return dir;
    const parent = path.dirname(dir);
    if (parent === dir || fs.existsSync(path.join(dir, '.git'))) return undefined;
    dir = parent;
  }
}

// Runs a command in a folder: its standard output, or undefined when it fails.
export type Runner = (command: string, args: string[], cwd: string) => Promise<string | undefined>;

export const runCommand: Runner = (command, args, cwd) =>
  new Promise((resolve) => {
    execFile(command, args, { cwd, timeout: TIMEOUT_MS, maxBuffer: 64 << 20 }, (err, stdout) =>
      resolve(err ? undefined : stdout),
    );
  });

// Schemas an axx gave: which axx (its id, in the URIs), and each kind's text.
interface Given {
  id: string;
  texts: Map<string, string>;
}

export class ProjectSchemas {
  private readonly given = new Map<string, Given>(); // by config folder
  private readonly asked = new Map<string, string>(); // the axx each folder asked last
  private readonly running = new Map<string, Promise<void>>();
  private lastFront = 0;

  // bundled: the folder of this extension's schemas. findAxx: the axx for a folder, or undefined.
  // changed: called when a folder's schemas change.
  constructor(
    private readonly bundled: string,
    private readonly findAxx: (dir: string) => string | undefined,
    private readonly run: Runner = runCommand,
    private readonly changed: (dir: string) => void = () => {},
  ) {}

  // The schema URI for a YAML file, or undefined when none of axx's governs it. The first time a
  // folder's file asks, the folder's axx is asked for its schemas, in the background.
  uri(file: string): string | undefined {
    const kind = kindOf(path.basename(file));
    if (kind === undefined) return undefined;
    const dir = configDir(file);
    if (dir !== undefined && !this.asked.has(dir) && !this.running.has(dir)) void this.refresh(dir);
    const given = dir === undefined ? undefined : this.given.get(dir);
    return given === undefined ? `${SCHEME}://bundled/${kind.file}` : `${SCHEME}://axx/${given.id}/${kind.file}`;
  }

  // The text of a schema URI that uri gave.
  content(uri: string): string | undefined {
    const m = new RegExp(`^${SCHEME}://(bundled|axx/([0-9a-f]+))/([^/]+)$`).exec(uri);
    if (m === null) return undefined;
    const kind = KINDS.find((k) => k.file === m[3]);
    if (kind === undefined) return undefined;
    if (m[1] === 'bundled') return fs.readFileSync(path.join(this.bundled, kind.file), 'utf8');
    for (const given of this.given.values()) {
      if (given.id === m[2]) return given.texts.get(kind.kind);
    }
    return undefined;
  }

  // Asks the axx of a config folder for its schemas, if it isn't the one asked last.
  refresh(dir: string): Promise<void> {
    const running = this.running.get(dir);
    if (running !== undefined) return running;
    const done = this.update(dir).finally(() => this.running.delete(dir));
    this.running.set(dir, done);
    return done;
  }

  // Refreshes every folder asked so far.
  refreshAll(): Promise<void> {
    return Promise.all([...this.asked.keys()].map((dir) => this.refresh(dir))).then(() => undefined);
  }

  // As refreshAll, when the window comes back to the front: at most every 30 seconds.
  refreshOnFront(now = Date.now()): Promise<void> {
    if (now - this.lastFront < 30_000) return Promise.resolve();
    this.lastFront = now;
    return this.refreshAll();
  }

  private async update(dir: string): Promise<void> {
    const axx = this.findAxx(dir);
    const which = axx === undefined ? undefined : await this.identify(axx, dir);
    if (axx === undefined || which === undefined) {
      this.asked.set(dir, '');
      this.use(dir, undefined);
      return;
    }
    if (this.asked.get(dir) === which) return;
    this.asked.set(dir, which);
    const texts = await this.readAll(axx, dir);
    this.use(dir, texts === undefined ? undefined : { id: sha256(which), texts });
  }

  // Which axx this is: its --version (with the version an axx.yaml names, the one it hands over
  // to), and its file as it is now, for builds of one version.
  private async identify(axx: string, dir: string): Promise<string | undefined> {
    const version = await this.run(axx, ['--version'], dir);
    if (version === undefined) return undefined;
    let file = axx;
    try {
      const real = fs.realpathSync(axx);
      const st = fs.statSync(real);
      file = `${real} ${st.size} ${st.mtimeMs}`;
    } catch {
      // the version alone, then
    }
    return `${version.trim()}\n${file}`;
  }

  // Has axx write every schema; undefined when it can't give one.
  private async readAll(axx: string, dir: string): Promise<Map<string, string> | undefined> {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'axx-schemas-'));
    try {
      const texts = new Map<string, string>();
      for (const kind of KINDS) {
        const out = path.join(tmp, kind.file);
        if ((await this.run(axx, ['schema', '--kind', kind.kind, '--out', out], dir)) === undefined) return undefined;
        const text = fs.readFileSync(out, 'utf8');
        const parsed: unknown = JSON.parse(text);
        if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) return undefined;
        texts.set(kind.kind, text);
      }
      return texts;
    } catch {
      return undefined;
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  }

  private use(dir: string, given: Given | undefined): void {
    if (this.given.get(dir)?.id === given?.id) return;
    if (given === undefined) this.given.delete(dir);
    else this.given.set(dir, given);
    this.changed(dir);
  }
}

function sha256(s: string): string {
  return crypto.createHash('sha256').update(s).digest('hex').slice(0, 24);
}

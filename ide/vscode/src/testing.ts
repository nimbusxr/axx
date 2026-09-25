// SPDX-License-Identifier: Apache-2.0
// Runs and debugs features, scenarios and example rows from the editor gutter and the Testing
// view. The feature files of a trusted workspace that belong to an axx project (an axx.yaml or
// axx.yml above them) become test items: feature > rule > scenario > example row. A run starts
// `axx run --format teamcity <file:line>...` in the project directory and maps the TeamCity
// service messages back to the items; the steps that ran become children of their scenario.

import { spawn, type ChildProcess } from 'node:child_process';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as readline from 'node:readline';
import * as vscode from 'vscode';
import { resolveAxx } from './axx';
import { parseFeature, type GherkinNode } from './gherkin';
import { parseDebugRequest, type DebugRequest } from './ideMarker';
import { stopProcessTree } from './processTree';
import { TeamCityReader, type Failure, type TcNode } from './teamcity';

const CONFIG_FILES = ['axx.yaml', 'axx.yml'];
const FEATURE_EXCLUDE = '**/{node_modules,.git}/**';
const GO_EXTENSION = 'golang.go';
const NO_GO =
  'Debugging step code needs the Go extension (golang.go), which provides the Go debugger. Install it and debug again.';

interface ItemData {
  kind: GherkinNode['kind'] | 'step';
  file: string;
  line: number; // 1-based; 0 for a step without a line (a hook)
}

interface FileEntry {
  item: vscode.TestItem; // the feature
  file: string;
  real: string; // the real path, which the service messages may use
  project: string; // the directory of the axx.yaml that applies
  text: string;
  byLine: Map<number, vscode.TestItem>; // scenarios and example rows
}

// One `axx run`: the files and lines it runs in one project, and the items it reports on.
interface Batch {
  project: string;
  targets: Map<string, Set<number> | 'all'>;
  queued: Set<vscode.TestItem>;
}

export class AxxTests implements vscode.Disposable {
  private readonly controller: vscode.TestController;
  private readonly output: vscode.LogOutputChannel;
  private readonly files = new Map<string, FileEntry>(); // by path
  private readonly realFiles = new Map<string, FileEntry>(); // by real path
  private readonly data = new WeakMap<vscode.TestItem, ItemData>();
  private readonly projects = new Map<string, string | undefined>(); // directory -> project
  private readonly running = new Set<ChildProcess>();
  private readonly disposables: vscode.Disposable[] = [];
  private discovered = false;

  constructor(output: vscode.LogOutputChannel) {
    this.output = output;
    this.controller = vscode.tests.createTestController('axx', 'axx');
    this.controller.resolveHandler = async (item) => {
      if (!item) await this.discover();
    };
    this.controller.refreshHandler = () => this.discover();
    this.controller.createRunProfile('Run', vscode.TestRunProfileKind.Run, (r, t) => this.run(r, t, false), true);
    this.controller.createRunProfile('Debug', vscode.TestRunProfileKind.Debug, (r, t) => this.run(r, t, true), true);

    const features = vscode.workspace.createFileSystemWatcher('**/*.feature');
    const configs = vscode.workspace.createFileSystemWatcher('**/axx.{yaml,yml}');
    this.disposables.push(
      this.controller,
      features,
      configs,
      features.onDidCreate((uri) => {
        if (this.discovered) void this.update(uri);
      }),
      features.onDidChange((uri) => {
        if (this.discovered || this.files.has(uri.fsPath)) void this.update(uri);
      }),
      features.onDidDelete((uri) => this.remove(uri.fsPath)),
      configs.onDidCreate(() => this.projectsChanged()),
      configs.onDidDelete(() => this.projectsChanged()),
      vscode.workspace.onDidOpenTextDocument((doc) => this.updateDocument(doc)),
      vscode.workspace.onDidChangeTextDocument((e) => this.updateDocument(e.document)),
      vscode.workspace.onDidGrantWorkspaceTrust(() => this.projectsChanged()),
    );
    for (const doc of vscode.workspace.textDocuments) this.updateDocument(doc);
  }

  dispose(): void {
    for (const child of this.running) stopProcessTree(child);
    for (const d of this.disposables) d.dispose();
  }

  // --- The test tree ---

  private async discover(): Promise<void> {
    if (!vscode.workspace.isTrusted) return;
    this.discovered = true;
    const seen = new Set<string>();
    for (const uri of await vscode.workspace.findFiles('**/*.feature', FEATURE_EXCLUDE)) {
      if (uri.scheme !== 'file') continue;
      seen.add(uri.fsPath);
      await this.update(uri);
    }
    for (const file of [...this.files.keys()]) {
      if (!seen.has(file) && !vscode.workspace.textDocuments.some((d) => d.uri.fsPath === file)) this.remove(file);
    }
  }

  private projectsChanged(): void {
    this.projects.clear();
    for (const entry of [...this.files.values()]) void this.update(vscode.Uri.file(entry.file), true);
    for (const doc of vscode.workspace.textDocuments) this.updateDocument(doc);
    if (this.discovered) void this.discover();
  }

  private async update(uri: vscode.Uri, force = false): Promise<void> {
    const doc = vscode.workspace.textDocuments.find((d) => d.uri.toString() === uri.toString());
    if (doc) {
      this.updateText(doc.uri, doc.getText(), force);
      return;
    }
    try {
      this.updateText(uri, await fs.promises.readFile(uri.fsPath, 'utf8'), force);
    } catch {
      this.remove(uri.fsPath);
    }
  }

  private updateDocument(doc: vscode.TextDocument): void {
    if (doc.uri.scheme === 'file' && doc.uri.fsPath.endsWith('.feature')) this.updateText(doc.uri, doc.getText());
  }

  // Rebuilds a file's items. Returns its entry, or undefined when it is not an axx feature.
  private updateText(uri: vscode.Uri, text: string, force = false): FileEntry | undefined {
    if (!vscode.workspace.isTrusted) return undefined;
    const file = uri.fsPath;
    const existing = this.files.get(file);
    if (existing && existing.text === text && !force) return existing;
    const project = this.projectOf(path.dirname(file));
    const feature = project === undefined ? undefined : parseFeature(text);
    if (project === undefined || feature === undefined) {
      this.remove(file);
      return undefined;
    }
    const lines = text.split(/\r?\n/);
    const id = uri.toString();
    let item = this.controller.items.get(id);
    if (!item) {
      item = this.controller.createTestItem(id, feature.name, uri);
      this.controller.items.add(item);
    }
    item.label = feature.name || path.basename(file);
    item.description = vscode.workspace.asRelativePath(uri);
    item.range = rangeOf(feature, lines);
    this.data.set(item, { kind: 'feature', file, line: feature.line });
    const byLine = new Map<number, vscode.TestItem>();
    const build = (node: GherkinNode): vscode.TestItem => {
      const child = this.controller.createTestItem(`${id}#${node.line}`, labelOf(node), uri);
      child.range = rangeOf(node, lines);
      child.description = node.examples;
      this.data.set(child, { kind: node.kind, file, line: node.line });
      if (node.kind !== 'rule') byLine.set(node.line, child);
      child.children.replace(node.children.map(build));
      return child;
    };
    item.children.replace(feature.children.map(build));
    if (existing) this.realFiles.delete(existing.real);
    const entry: FileEntry = { item, file, real: realPath(file), project, text, byLine };
    this.files.set(file, entry);
    this.realFiles.set(entry.real, entry);
    return entry;
  }

  private remove(file: string): void {
    const entry = this.files.get(file);
    if (!entry) return;
    this.controller.items.delete(entry.item.id);
    this.files.delete(file);
    this.realFiles.delete(entry.real);
  }

  // The nearest directory at or above `dir` with an axx.yaml, which is where axx runs.
  private projectOf(dir: string): string | undefined {
    const visited: string[] = [];
    let found: string | undefined;
    for (let d = dir; ; d = path.dirname(d)) {
      if (this.projects.has(d)) {
        found = this.projects.get(d);
        break;
      }
      visited.push(d);
      if (CONFIG_FILES.some((name) => fs.existsSync(path.join(d, name)))) {
        found = d;
        break;
      }
      if (path.dirname(d) === d) break;
    }
    for (const v of visited) this.projects.set(v, found);
    return found;
  }

  // --- Runs ---

  private async run(request: vscode.TestRunRequest, token: vscode.CancellationToken, debug: boolean): Promise<void> {
    if (!request.include && !this.discovered) await this.discover();
    const run = this.controller.createTestRun(request);
    try {
      const batches = this.plan(request);
      const fail = (text: string): void => {
        for (const batch of batches) for (const item of batch.queued) run.errored(item, new vscode.TestMessage(text));
      };
      if (!vscode.workspace.isTrusted) {
        fail('axx runs tests only in trusted workspaces.');
        return;
      }
      if (debug && batches.length > 0 && !vscode.extensions.getExtension(GO_EXTENSION)) {
        this.reportNoGo();
        fail(NO_GO);
        return;
      }
      for (const batch of batches) {
        if (token.isCancellationRequested) {
          for (const item of batch.queued) run.skipped(item);
          continue;
        }
        await this.runBatch(run, batch, token, debug);
      }
    } finally {
      run.end();
    }
  }

  // Groups the requested items by axx project, as files and lines to run.
  private plan(request: vscode.TestRunRequest): Batch[] {
    const exclude = new Set(request.exclude ?? []);
    const batches = new Map<string, Batch>();
    // Steps don't count: a step can't run without the rest of its scenario.
    const hasExcluded = (item: vscode.TestItem): boolean => {
      let found = false;
      item.children.forEach((c) => {
        if (this.data.get(c)?.kind !== 'step') found ||= exclude.has(c) || hasExcluded(c);
      });
      return found;
    };
    const leaves = (item: vscode.TestItem, into: Set<vscode.TestItem>): void => {
      if (exclude.has(item)) return;
      const kind = this.data.get(item)?.kind;
      let rows = false;
      item.children.forEach((c) => {
        const childKind = this.data.get(c)?.kind;
        if (childKind === 'example') rows = true;
        if (childKind !== 'step') leaves(c, into);
      });
      if ((kind === 'scenario' && !rows) || kind === 'example') into.add(item);
    };
    const add = (item: vscode.TestItem): void => {
      const d = this.data.get(item);
      if (!d || exclude.has(item)) return;
      if (d.kind === 'step') {
        if (item.parent) add(item.parent); // a step runs with its scenario
        return;
      }
      const entry = this.files.get(d.file);
      if (!entry) return;
      if (d.kind === 'rule' || hasExcluded(item)) {
        item.children.forEach(add); // `axx run` selects scenarios and rows, not rules
        return;
      }
      let batch = batches.get(entry.project);
      if (!batch) {
        batch = { project: entry.project, targets: new Map(), queued: new Set() };
        batches.set(entry.project, batch);
      }
      const lines = batch.targets.get(d.file);
      if (d.kind === 'feature') {
        batch.targets.set(d.file, 'all');
      } else if (lines !== 'all') {
        batch.targets.set(d.file, (lines ?? new Set()).add(d.line));
      }
      leaves(item, batch.queued);
    };
    if (request.include) {
      request.include.forEach(add);
    } else {
      this.controller.items.forEach(add);
    }
    return [...batches.values()];
  }

  private async runBatch(run: vscode.TestRun, batch: Batch, token: vscode.CancellationToken, debug: boolean): Promise<void> {
    const { project, queued } = batch;
    const axx = resolveAxx(vscode.Uri.file(project), project, this.output);
    if (axx === undefined) {
      for (const item of queued) run.errored(item, new vscode.TestMessage("Can't find the axx executable: install axx or set axx.path."));
      return;
    }
    for (const item of queued) run.enqueued(item);
    const args = ['run', '--format', 'teamcity', ...(debug ? ['--debug-steps'] : []), ...targetArgs(batch)];
    this.output.info(`Running "${axx} ${args.join(' ')}" in ${project}`);
    run.appendOutput(`$ axx ${args.join(' ')}\r\n(in ${project})\r\n\r\n`);

    const child = spawn(axx, args, {
      cwd: project,
      detached: process.platform !== 'win32', // its own process group, to stop it with everything it started
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    this.running.add(child);
    const cancel = token.onCancellationRequested(() => stopProcessTree(child));

    const finished = new Set<vscode.TestItem>(); // scenarios and rows with a result
    const suites = new Map<string, { item: vscode.TestItem; start: number; messages: vscode.TestMessage[] }>();
    const steps = new Map<string, vscode.TestItem>();
    const runningSteps = new Set<vscode.TestItem>();
    let attached = false;
    const reader = new TeamCityReader({
      output: (line) => {
        run.appendOutput(`${line}\r\n`);
        if (debug && !attached) {
          const request = parseDebugRequest(line);
          if (request?.kind === 'attach' && request.type === 'go') {
            attached = true;
            void this.attach(request, run, project, child);
          }
        }
      },
      suiteStarted: (node) => {
        if (node.parentId === '0') return; // a feature: its state comes from its scenarios
        const item = this.scenarioItem(node);
        if (!item) return;
        suites.set(node.id, { item, start: Date.now(), messages: [] });
        run.started(item);
      },
      testStarted: (node) => {
        const suite = suites.get(node.parentId);
        if (!suite) return;
        const step = this.stepItem(suite.item, node);
        steps.set(node.id, step);
        runningSteps.add(step);
        run.started(step);
      },
      testOutput: (node, text) => {
        const step = steps.get(node.id);
        run.appendOutput(text.replace(/\r?\n/g, '\r\n'), step && this.locationOf(step), step);
      },
      testFinished: (node) => {
        const step = steps.get(node.id);
        if (!step) return;
        runningSteps.delete(step);
        if (node.outcome === 'failed') {
          this.logResult('failed', step, node.failure?.message);
          const message = this.failureMessage(node.failure, step);
          suites.get(node.parentId)?.messages.push(message);
          run.failed(step, message, node.durationMs);
        } else if (node.outcome === 'skipped') {
          if (node.ignored && node.ignored !== 'skipped') {
            run.appendOutput(`${node.name}: ${node.ignored}\r\n`, this.locationOf(step), step);
          }
          run.skipped(step);
        } else {
          run.passed(step, node.durationMs);
        }
      },
      suiteFinished: (node, outcome) => {
        const suite = suites.get(node.id);
        if (!suite) return;
        finished.add(suite.item);
        this.logResult(outcome, suite.item);
        const ms = Date.now() - suite.start;
        if (outcome === 'failed') {
          run.failed(suite.item, suite.messages, ms);
        } else if (outcome === 'skipped') {
          run.skipped(suite.item);
        } else {
          run.passed(suite.item, ms);
        }
      },
    });

    child.stderr?.setEncoding('utf8');
    child.stderr?.on('data', (chunk: string) => run.appendOutput(chunk.replace(/\r?\n/g, '\r\n')));
    const lines = readline.createInterface({ input: child.stdout!, crlfDelay: Infinity });
    lines.on('line', (line) => reader.line(line));
    const drained = new Promise<void>((resolve) => lines.once('close', resolve));
    let spawnError: Error | undefined;
    const code = await new Promise<number | null>((resolve) => {
      child.once('exit', (exitCode) => resolve(exitCode));
      child.once('error', (err) => {
        spawnError = err;
        resolve(null);
      });
    });
    // An app that inherited the pipes could keep them open; don't wait for it.
    await Promise.race([drained, new Promise((resolve) => setTimeout(resolve, 2000))]);
    lines.close();
    cancel.dispose();
    this.running.delete(child);
    this.output.info(`axx run exited with code ${code ?? '(none)'}`);

    for (const step of runningSteps) run.skipped(step);
    const unfinished = new Set([...queued, ...[...suites.values()].map((s) => s.item)].filter((i) => !finished.has(i)));
    // Items without a result did not run: the run was cancelled, or axx stopped early.
    const error = spawnError
      ? `Couldn't start axx: ${spawnError.message}`
      : token.isCancellationRequested || [0, 1, 3, 130].includes(code ?? -1)
        ? undefined
        : exitMessage(code);
    for (const item of unfinished) {
      this.logResult(error ? 'errored' : 'skipped', item, error);
      if (error) {
        run.errored(item, new vscode.TestMessage(error));
      } else {
        run.skipped(item);
      }
    }
  }

  private logResult(state: string, item: vscode.TestItem, detail?: string): void {
    const d = this.data.get(item);
    const where = d ? `${path.basename(d.file)}:${d.line}` : item.id;
    this.output.debug(`${state}: ${item.label} (${where})${detail ? `: ${detail}` : ''}`);
  }

  // The item for a scenario or example row that the run reports, by its file and line.
  private scenarioItem(node: TcNode): vscode.TestItem | undefined {
    const loc = node.location;
    if (!loc) return undefined;
    const entry = this.entryAt(loc.file);
    if (!entry) return undefined;
    let item = entry.byLine.get(loc.line);
    if (!item) {
      // Not in the tree (the file changed since it was read): add it to the feature.
      item = this.controller.createTestItem(`${entry.item.id}#${loc.line}`, node.name, entry.item.uri);
      item.range = new vscode.Range(loc.line - 1, 0, loc.line - 1, 0);
      this.data.set(item, { kind: 'scenario', file: entry.file, line: loc.line });
      entry.item.children.add(item);
      entry.byLine.set(loc.line, item);
    }
    return item;
  }

  private entryAt(file: string): FileEntry | undefined {
    const known = this.files.get(file) ?? this.realFiles.get(realPath(file));
    if (known) return known;
    try {
      return this.updateText(vscode.Uri.file(file), fs.readFileSync(file, 'utf8'));
    } catch {
      return undefined;
    }
  }

  // A step of a scenario that ran. Steps have no range, so the gutter shows no run buttons on
  // them; their line is kept for failure messages and output.
  private stepItem(parent: vscode.TestItem, node: TcNode): vscode.TestItem {
    const line = node.location?.line ?? 0;
    const id = `${parent.id}/${line > 0 ? line : node.name}`;
    let step = parent.children.get(id);
    if (step) {
      step.label = node.name;
    } else {
      step = this.controller.createTestItem(id, node.name, parent.uri);
      parent.children.add(step);
    }
    this.data.set(step, { kind: 'step', file: this.data.get(parent)?.file ?? '', line });
    return step;
  }

  private locationOf(step: vscode.TestItem): vscode.Location | undefined {
    const line = this.data.get(step)?.line ?? 0;
    return step.uri && line > 0 ? new vscode.Location(step.uri, new vscode.Position(line - 1, 0)) : undefined;
  }

  private failureMessage(failure: Failure | undefined, step: vscode.TestItem): vscode.TestMessage {
    const f = failure ?? { message: 'failed', details: '' };
    const message =
      f.expected !== undefined && f.actual !== undefined
        ? vscode.TestMessage.diff(f.message, f.expected, f.actual)
        : new vscode.TestMessage(f.details && f.details !== f.message ? `${f.message}\n\n${f.details}` : f.message);
    message.location = this.locationOf(step);
    return message;
  }

  // --- Debugging ---

  private async attach(request: DebugRequest, run: vscode.TestRun, project: string, child: ChildProcess): Promise<void> {
    const folder = vscode.workspace.getWorkspaceFolder(vscode.Uri.file(project));
    const config: vscode.DebugConfiguration = {
      type: 'go',
      request: 'attach',
      mode: 'remote',
      name: 'axx: step code',
      host: request.host,
      port: request.port,
    };
    this.output.info(`Attaching the Go debugger to ${request.host}:${request.port}`);
    let started = false;
    try {
      started = await vscode.debug.startDebugging(folder, config, { testRun: run });
    } catch (err) {
      this.output.error(`Starting the Go debugger failed: ${String(err)}`);
    }
    if (!started) {
      void vscode.window.showErrorMessage(
        `axx: couldn't attach the Go debugger to ${request.host}:${request.port}. The axx output channel has details.`,
      );
      stopProcessTree(child);
    }
  }

  private reportNoGo(): void {
    this.output.error(NO_GO);
    void vscode.window.showErrorMessage(`axx: ${NO_GO}`, 'Install the Go extension').then((choice) => {
      if (choice) void vscode.commands.executeCommand('workbench.extensions.installExtension', GO_EXTENSION);
    });
  }
}

function labelOf(node: GherkinNode): string {
  return node.name || node.keyword || `line ${node.line}`;
}

function rangeOf(node: GherkinNode, lines: string[]): vscode.Range {
  return new vscode.Range(node.line - 1, 0, node.endLine - 1, lines[node.endLine - 1]?.length ?? 0);
}

function targetArgs(batch: Batch): string[] {
  const args: string[] = [];
  for (const [file, lines] of batch.targets) {
    const rel = path.relative(batch.project, file) || file;
    if (lines === 'all') {
      args.push(rel);
    } else {
      for (const line of [...lines].sort((a, b) => a - b)) args.push(`${rel}:${line}`);
    }
  }
  return args;
}

function exitMessage(code: number | null): string {
  switch (code) {
    case null:
      return 'axx run was stopped.';
    case 2:
      return 'axx run stopped on a usage or configuration error (exit code 2). The test output has the details.';
    case 4:
      return 'An app failed to start (exit code 4). The test output has the details.';
    default:
      return `axx run exited with code ${code}. The test output has the details.`;
  }
}

function realPath(file: string): string {
  try {
    return fs.realpathSync.native(file);
  } catch {
    return file;
  }
}

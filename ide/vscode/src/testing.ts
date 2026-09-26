// SPDX-License-Identifier: Apache-2.0
// Runs and debugs features, scenarios and example rows from the editor gutter and the Testing
// view. The feature files of a trusted workspace that belong to an axx project (an axx.yaml or
// axx.yml above them) become test items: feature > rule > scenario > example row. A run starts
// `axx run --format teamcity <file:line>...` in the project directory and maps the TeamCity
// service messages back to the items; the steps that ran become children of their scenario.
// Watch runs open the web pack's browsers in windows on the desktop as they go, slowed down, one
// scenario at a time; the traces and videos scenarios keep are linked in their output and open from
// the test's context menu. Debug runs pause the web pack's scenarios where they fail and before the
// steps that have a breakpoint (`axx run --pause-at <file>:<line>`), with the page in Playwright's
// Inspector, and say in the run's output that they ignore the breakpoints on other lines; what the
// editor does while a scenario is paused is in pausedScenario.ts. With the Go extension, they also
// stop at breakpoints in step code (`axx run --debug-steps`, with VS Code's Go debugger attached);
// without it, they run the same way without that, and say so once.

import { spawn, type ChildProcess } from 'node:child_process';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as readline from 'node:readline';
import * as vscode from 'vscode';
import { resolveAxx } from './axx';
import { BrowserPanels } from './browserPanel';
import { notAStep, pauseSteps, runArguments, traceViewerUrl, viewerFolder, type StepBreakpoint } from './browsers';
import { FileServer } from './fileServer';
import { parseFeature, type GherkinNode } from './gherkin';
import { parseDebugRequest, parseIdeAnnouncement, parseScenarioPause, type DebugRequest, type KeptFileAnnouncement } from './ideMarker';
import { PausedScenario } from './pausedScenario';
import { linesOf } from './pausedStep';
import { stopProcessTree } from './processTree';
import { TeamCityReader, type Failure, type TcNode } from './teamcity';

const CONFIG_FILES = ['axx.yaml', 'axx.yml'];
const FEATURE_EXCLUDE = '**/{node_modules,.git}/**';
const GO_EXTENSION = 'golang.go';
const NO_GO =
  'Stopping at breakpoints in step code needs the Go extension (golang.go), which provides the Go debugger. Debug runs the scenarios without it.';
const TRACE_SITE = 'https://trace.playwright.dev';

type Mode = 'run' | 'debug' | 'watch';

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

// A trace or video a scenario kept, and the folder of the trace viewer's files its run announced,
// which the extension serves itself.
interface Kept {
  kind: 'trace' | 'video';
  path: string;
  label: string; // the scenario's
  viewerDir?: string;
}

export class AxxTests implements vscode.Disposable {
  private readonly controller: vscode.TestController;
  private readonly output: vscode.LogOutputChannel;
  private readonly files = new Map<string, FileEntry>(); // by path
  private readonly realFiles = new Map<string, FileEntry>(); // by real path
  private readonly data = new WeakMap<vscode.TestItem, ItemData>();
  private readonly projects = new Map<string, string | undefined>(); // directory -> project
  private readonly running = new Set<ChildProcess>();
  private readonly kept = new Map<string, { item: vscode.TestItem; files: Kept[] }>(); // by scenario id
  private viewerDir: string | undefined; // the trace viewer's folder a run last announced
  private toldNoGo = false; // whether a Debug run said it can't debug step code without the Go extension
  private readonly fileServer = new FileServer();
  private readonly panels = new BrowserPanels();
  private readonly paused: PausedScenario;
  private readonly disposables: vscode.Disposable[] = [];
  private discovered = false;

  constructor(output: vscode.LogOutputChannel) {
    this.output = output;
    this.controller = vscode.tests.createTestController('axx', 'axx');
    this.paused = new PausedScenario(output);
    this.controller.resolveHandler = async (item) => {
      if (!item) await this.discover();
    };
    this.controller.refreshHandler = () => this.discover();
    this.controller.createRunProfile('Run', vscode.TestRunProfileKind.Run, (r, t) => this.run(r, t, 'run'), true);
    this.controller.createRunProfile('Debug', vscode.TestRunProfileKind.Debug, (r, t) => this.run(r, t, 'debug'), true);
    this.controller.createRunProfile('Watch', vscode.TestRunProfileKind.Run, (r, t) => this.run(r, t, 'watch'), false);

    const features = vscode.workspace.createFileSystemWatcher('**/*.feature');
    const configs = vscode.workspace.createFileSystemWatcher('**/axx.{yaml,yml}');
    this.disposables.push(
      this.controller,
      this.panels,
      this.paused,
      { dispose: () => this.fileServer.dispose() },
      vscode.commands.registerCommand('axx.openTrace', (item?: vscode.TestItem) => this.openKept(item, 'trace')),
      vscode.commands.registerCommand('axx.playVideo', (item?: vscode.TestItem) => this.openKept(item, 'video')),
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

  private async run(request: vscode.TestRunRequest, token: vscode.CancellationToken, mode: Mode): Promise<void> {
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
      // Step code needs the Go debugger; the rest of Debug does not.
      const debugSteps = mode === 'debug' && vscode.extensions.getExtension(GO_EXTENSION) !== undefined;
      if (mode === 'debug' && !debugSteps && batches.length > 0) this.tellNoGo();
      for (const batch of batches) {
        if (token.isCancellationRequested) {
          for (const item of batch.queued) run.skipped(item);
          continue;
        }
        await this.runBatch(run, batch, token, mode, debugSteps);
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

  // `debugSteps`: debug step code too, in VS Code's Go debugger (Debug only).
  private async runBatch(
    run: vscode.TestRun,
    batch: Batch,
    token: vscode.CancellationToken,
    mode: Mode,
    debugSteps: boolean,
  ): Promise<void> {
    const { project, queued } = batch;
    const debug = mode === 'debug';
    const axx = resolveAxx(vscode.Uri.file(project), project, this.output);
    if (axx === undefined) {
      for (const item of queued) run.errored(item, new vscode.TestMessage("Can't find the axx executable: install axx or set axx.path."));
      return;
    }
    for (const item of queued) run.enqueued(item);
    const watchSlowdownMs =
      mode === 'watch' ? vscode.workspace.getConfiguration('axx', vscode.Uri.file(project)).get<number>('watch.slowdown', 300) : undefined;
    const { pauseAt, notSteps } = debug ? pauseSteps(stepBreakpoints(), project, this.featureLines(batch)) : { pauseAt: [], notSteps: [] };
    const args = runArguments(targetArgs(batch), { debug, debugSteps, watchSlowdownMs, pauseAt });
    this.output.info(`Running "${axx} ${args.join(' ')}" in ${project}`);
    run.appendOutput(`$ axx ${args.join(' ')}\r\n(in ${project})\r\n`);
    // At the breakpoint's line too, where the editor shows it.
    for (const b of notSteps) {
      run.appendOutput(`${notAStep(project, b)}\r\n`, new vscode.Location(vscode.Uri.file(b.file), new vscode.Position(b.line - 1, 0)));
    }
    run.appendOutput('\r\n');

    const child = spawn(axx, args, {
      cwd: project,
      env: { ...process.env, AXX_IDE: 'vscode' }, // packs print what the IDE can show
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
    // What packs announce: the folder of the trace viewer's files, and the traces and videos
    // being linked.
    const announced = {
      viewerDir: undefined as string | undefined,
      pending: [] as Promise<void>[],
    };
    const reader = new TeamCityReader({
      output: (line) => {
        const pause = parseScenarioPause(line);
        if (pause) {
          if (pause.kind === 'paused') this.paused.paused(child, pause);
          else this.paused.resumed(child, pause.url);
          return;
        }
        const announcement = parseIdeAnnouncement(line);
        switch (announcement?.kind) {
          case 'trace-viewer':
            announced.viewerDir = announcement.dir;
            this.viewerDir = announcement.dir; // for the traces of later runs too
            return;
          case 'trace':
          case 'video':
            announced.pending.push(this.keep(announcement, run, project, announced.viewerDir));
            return;
        }
        run.appendOutput(`${line}\r\n`);
        if (debugSteps && !attached) {
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
        this.forget(item);
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
    this.paused.ended(child);
    await Promise.all(announced.pending);
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

  // The lines of the feature files a run runs, as the test tree has them.
  private featureLines(batch: Batch): Map<string, string[]> {
    const lines = new Map<string, string[]>();
    for (const file of batch.targets.keys()) {
      const entry = this.files.get(file);
      if (entry) lines.set(file, linesOf(entry.text));
    }
    return lines;
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

  // --- Browsers ---

  // Keeps a trace or video for its scenario, and links it in the scenario's output (see
  // traceLink). `viewerDir`: the folder of the trace viewer's files the run announced.
  private async keep(announcement: KeptFileAnnouncement, run: vscode.TestRun, project: string, viewerDir: string | undefined): Promise<void> {
    const item = this.entryAt(path.resolve(project, announcement.file))?.byLine.get(announcement.line);
    const { kind } = announcement;
    const label = item?.label ?? path.basename(announcement.path);
    const kept: Kept = { kind, path: announcement.path, label, viewerDir };
    if (item) {
      this.kept.set(item.id, { item, files: [...(this.kept.get(item.id)?.files ?? []), kept] });
      this.updateContext();
    }
    const rel = path.relative(project, announcement.path);
    const shown = rel && !rel.startsWith('..') && !path.isAbsolute(rel) ? rel : announcement.path;
    let link = '';
    try {
      if (kind === 'video') {
        link = await this.fileServer.url(announcement.path);
      } else {
        link = (await this.traceLink(kept)) ?? TRACE_SITE;
      }
    } catch (err) {
      this.output.error(`Serving ${announcement.path} failed: ${String(err)}`);
    }
    run.appendOutput(`${kind === 'trace' ? 'Trace' : 'Video'}: ${shown}${link ? `\r\n  ${link}` : ''}\r\n`, undefined, item);
  }

  // Drops what an earlier run kept for a scenario that runs again.
  private forget(item: vscode.TestItem): void {
    if (this.kept.delete(item.id)) this.updateContext();
  }

  // The context keys that show Open Trace and Play Video on the scenarios that kept one, and on
  // their steps.
  private updateContext(): void {
    const ids = (kind: Kept['kind']): string[] =>
      [...this.kept.values()]
        .filter(({ files }) => files.some((k) => k.kind === kind))
        .flatMap(({ item }) => {
          const steps: string[] = [];
          item.children.forEach((c) => steps.push(c.id));
          return [item.id, ...steps];
        });
    void vscode.commands.executeCommand('setContext', 'axx.testsWithTrace', ids('trace'));
    void vscode.commands.executeCommand('setContext', 'axx.testsWithVideo', ids('video'));
  }

  // Open Trace and Play Video, from a test's context menu.
  private async openKept(item: vscode.TestItem | undefined, kind: Kept['kind']): Promise<void> {
    let scenario = item;
    while (scenario && !this.kept.has(scenario.id)) scenario = scenario.parent; // a step: its scenario
    const all = scenario ? (this.kept.get(scenario.id)?.files ?? []).filter((k) => k.kind === kind) : [];
    let kept: Kept | undefined = all[0];
    if (all.length > 1) {
      const picks = all.map((k) => ({ label: path.basename(k.path), description: path.dirname(k.path), kept: k }));
      kept = (await vscode.window.showQuickPick(picks, { placeHolder: kind === 'trace' ? 'Open which trace?' : 'Play which video?' }))?.kept;
    }
    if (!kept) return;
    if (!fs.existsSync(kept.path)) {
      void vscode.window.showErrorMessage(`axx: ${kept.path} no longer exists. Run the scenario again.`);
      return;
    }
    try {
      const page = kind === 'video' ? await this.external(await this.fileServer.url(kept.path)) : await this.traceLink(kept, true);
      if (page && kind === 'video') {
        this.panels.show('video', `Video: ${kept.label}`, page);
      } else if (page) {
        this.panels.show('trace', `Trace: ${kept.label}`, page);
      } else {
        await vscode.commands.executeCommand('revealFileInOS', vscode.Uri.file(kept.path));
        const choice = await vscode.window.showInformationMessage(
          'axx: no Playwright trace viewer to open the trace in. Drop it on trace.playwright.dev to open it.',
          'Open trace.playwright.dev',
        );
        if (choice) void vscode.env.openExternal(vscode.Uri.parse(TRACE_SITE));
      }
    } catch (err) {
      this.output.error(`Opening ${kept.path} failed: ${String(err)}`);
      void vscode.window.showErrorMessage(`axx: couldn't open ${kept.path}. The axx output channel has details.`);
    }
  }

  // The page that opens a trace in the trace viewer's files, served with the trace, when a run
  // announced their folder (they stay on disk, so this works after the run, offline); else none.
  // `external`: as this window reaches it.
  private async traceLink(kept: Kept, external = false): Promise<string | undefined> {
    const folder = viewerFolder(kept.viewerDir, this.viewerDir);
    if (!folder) return undefined;
    const reach = (url: string): Promise<string> => (external ? this.external(url) : Promise.resolve(url));
    const trace = await reach(await this.fileServer.url(kept.path));
    return traceViewerUrl(await reach(await this.fileServer.folderUrl(folder)), trace);
  }

  // A URL on the machine that runs axx, as this window reaches it (forwarded when that machine
  // is a remote one).
  private async external(url: string): Promise<string> {
    return (await vscode.env.asExternalUri(vscode.Uri.parse(url))).toString(true);
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

  // Says once that Debug does not stop in step code without the Go extension, and offers it.
  private tellNoGo(): void {
    if (this.toldNoGo) return;
    this.toldNoGo = true;
    this.output.info(NO_GO);
    void vscode.window.showInformationMessage(`axx: ${NO_GO}`, 'Install the Go extension').then((choice) => {
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

// The enabled breakpoints in feature files on disk. Debug runs pause before those on steps (see
// pauseSteps).
function stepBreakpoints(): StepBreakpoint[] {
  return vscode.debug.breakpoints.flatMap((b) =>
    b instanceof vscode.SourceBreakpoint &&
    b.enabled &&
    b.location.uri.scheme === 'file' &&
    b.location.uri.fsPath.endsWith('.feature')
      ? [{ file: b.location.uri.fsPath, line: b.location.range.start.line + 1 }]
      : [],
  );
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

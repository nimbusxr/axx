// SPDX-License-Identifier: Apache-2.0
// The axx host in the editor. It starts `axx host --ide` in an axx project (the folder of its
// axx.yaml), with the axx executable runs use: when a workspace folder that is a project with
// services opens and axx.host.start is on, or from axx: Start axx host. It stops it by closing its
// stdin: from axx: Stop axx host, and when the folder closes or the extension deactivates. While it
// runs:
// - A run whose services' definition changed asks in a notification, with the definition's
//   warnings: Show Diff opens the approved and proposed definitions side by side and asks again;
//   Approve and Refuse answer on the host's stdin. Only a click there answers: no command approves,
//   since other extensions and an agent's editor tools can run commands. axx: Show axx host
//   approval asks again.
// - The runs it serves that the editor did not start (an agent's) show as test runs, from their
//   TeamCity reports, with their files mapped to the project here (see AxxTests.follow).
// - The status bar says what it does, and the axx host output channel has its log.
// The protocol, and what the editor shows of it, are in host.ts.

import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';
import { resolveAxx } from './axx';
import {
  ALREADY_SERVED,
  approvalMessage,
  declaresServices,
  HostState,
  hostStatus,
  mapRunFile,
  runError,
  runStateText,
  startHost,
  STOP_MS,
  type ApprovalEvent,
  type HostEvent,
  type HostExit,
  type HostPhase,
  type HostProcess,
  type RunEvent,
} from './host';
import type { AxxTests, FollowedRun } from './testing';

const CONFIG_FILES = ['axx.yaml', 'axx.yml'];
const IDE = 'vscode'; // the AXX_IDE of the editor's own runs, which show already
const SCHEME = 'axx-host'; // the definitions an approval shows, read-only
const SHOW_DIFF = 'Show Diff';
const APPROVE = 'Approve';
const REFUSE = 'Refuse';

// A project the editor started a host in.
interface Hosted {
  dir: string;
  process: HostProcess;
  generation: number; // which of the editor's starts it is: its definitions' documents are under /<generation>/
  state: HostState;
  phase: HostPhase;
  detail: string; // why it stopped, failed, or serves elsewhere
  status: vscode.StatusBarItem;
  runs: Map<number, FollowedRun>; // the runs the editor follows, by number
  forget: boolean; // its folder closed: drop it once it exits
}

export class AxxHost implements vscode.Disposable, vscode.TextDocumentContentProvider {
  private readonly hosts = new Map<string, Hosted>(); // by project directory
  private readonly documents = new Map<string, string>(); // the definitions' texts, by URI path
  private readonly log: vscode.OutputChannel;
  private readonly disposables: vscode.Disposable[] = [];
  private generations = 0;
  private disposed = false;
  private context = ''; // the context keys last set

  constructor(
    private readonly output: vscode.LogOutputChannel,
    private readonly tests: AxxTests,
  ) {
    this.log = vscode.window.createOutputChannel('axx host');
    this.disposables.push(
      this.log,
      vscode.workspace.registerTextDocumentContentProvider(SCHEME, this),
      vscode.commands.registerCommand('axx.startHost', (dir?: string) => this.startCommand(dir)),
      vscode.commands.registerCommand('axx.stopHost', (dir?: string) => this.stopCommand(dir)),
      vscode.commands.registerCommand('axx.showHostApproval', (dir?: string) => this.showApproval(dir)),
      vscode.commands.registerCommand('axx.showHostLog', () => this.log.show(true)),
      vscode.workspace.onDidGrantWorkspaceTrust(() => this.autoStart()),
      vscode.workspace.onDidChangeWorkspaceFolders((e) => this.foldersChanged(e)),
      vscode.workspace.onDidChangeConfiguration((e) => {
        if (e.affectsConfiguration('axx.host.start')) this.autoStart();
      }),
    );
    this.updateContext();
    this.autoStart();
  }

  // Stops every host: each stops the services of its run in progress and cleans up, also after the
  // editor is gone.
  dispose(): void {
    this.disposed = true;
    for (const host of this.hosts.values()) {
      host.process.stop();
      host.status.dispose();
    }
    this.hosts.clear();
    for (const d of this.disposables) d.dispose();
  }

  provideTextDocumentContent(uri: vscode.Uri): string {
    return this.documents.get(uri.path) ?? '';
  }

  // --- Starting and stopping ---

  // Starts a host in each workspace folder that is an axx project with services, where
  // axx.host.start is on, unless the editor started one there before.
  private autoStart(): void {
    if (!vscode.workspace.isTrusted) return;
    for (const folder of vscode.workspace.workspaceFolders ?? []) {
      if (folder.uri.scheme !== 'file' || this.hosts.has(folder.uri.fsPath)) continue;
      if (!vscode.workspace.getConfiguration('axx', folder.uri).get<boolean>('host.start', false)) continue;
      if (declaresServices(folder.uri.fsPath)) this.start(folder.uri.fsPath);
    }
  }

  private async startCommand(dir?: string): Promise<void> {
    if (!vscode.workspace.isTrusted) {
      void vscode.window.showWarningMessage('axx: the axx host runs only in trusted workspaces.');
      return;
    }
    const project = dir ?? (await this.pickProject());
    if (project === undefined) return;
    if (!isProject(project)) {
      void vscode.window.showWarningMessage(`axx host: ${project} is no axx project of this workspace.`);
      return;
    }
    if (this.running(project)) {
      void vscode.window.showInformationMessage(`axx host already runs in ${project}.`);
      return;
    }
    this.start(project);
  }

  // The axx project to start a host in: the workspace's only one without a host, or the one picked.
  private async pickProject(): Promise<string | undefined> {
    const projects = new Map<string, vscode.Uri>(); // directory -> its axx.yaml
    for (const uri of await vscode.workspace.findFiles('**/axx.{yaml,yml}', '**/{node_modules,.git}/**')) {
      const dir = path.dirname(uri.fsPath);
      if (uri.scheme === 'file' && !projects.has(dir) && !this.running(dir)) projects.set(dir, uri);
    }
    if (projects.size <= 1) {
      if (projects.size === 0) {
        void vscode.window.showInformationMessage(
          this.hosts.size > 0 ? 'axx host already runs in every axx project of this workspace.' : 'axx host: no axx project (axx.yaml) in this workspace.',
        );
      }
      return [...projects.keys()][0];
    }
    const picks = [...projects]
      .map(([dir, uri]) => ({ label: vscode.workspace.asRelativePath(uri), dir }))
      .sort((a, b) => a.label.localeCompare(b.label));
    return (await vscode.window.showQuickPick(picks, { placeHolder: 'Start the axx host in which project?' }))?.dir;
  }

  private start(dir: string): void {
    const axx = resolveAxx(vscode.Uri.file(dir), dir, this.output);
    if (axx === undefined) return;
    const previous = this.hosts.get(dir);
    const generation = ++this.generations;
    this.output.info(`Starting "${axx} host --ide" in ${dir}`);
    this.log.appendLine(`$ axx host --ide\n(in ${dir})`);
    let host: Hosted | undefined;
    const hostProcess = startHost(axx, dir, {
      event: (e) => {
        if (host) this.event(host, e);
      },
      log: (line) => this.log.appendLine(line),
      exited: (exit) => {
        if (host) this.exited(host, exit);
      },
    });
    host = {
      dir,
      process: hostProcess,
      generation,
      state: new HostState(),
      phase: 'starting',
      detail: '',
      status: previous?.status ?? vscode.window.createStatusBarItem(`axx.host.${dir}`, vscode.StatusBarAlignment.Left, 0),
      runs: new Map(),
      forget: false,
    };
    host.status.name = 'axx host';
    this.hosts.set(dir, host);
    this.updateAll();
  }

  private async stopCommand(dir?: string): Promise<void> {
    const running = [...this.hosts.values()].filter((h) => (dir === undefined || h.dir === dir) && (h.phase === 'starting' || h.phase === 'serving'));
    if (running.length === 0) {
      void vscode.window.showInformationMessage('axx host: not running.');
      return;
    }
    const host =
      running.length === 1
        ? running[0]
        : (await vscode.window.showQuickPick(
            running.map((h) => ({ label: path.basename(h.dir), description: h.dir, host: h })),
            { placeHolder: 'Stop the axx host of which project?' },
          ))?.host;
    if (host) this.stop(host);
  }

  // Closes its stdin: it stops the services of the run in progress, cleans up and exits.
  // `forget`: its folder closed, so it goes once it exits.
  private stop(host: Hosted, forget = false): void {
    host.forget ||= forget;
    if (host.phase === 'starting' || host.phase === 'serving') {
      host.phase = 'stopping';
      this.log.appendLine(`axx host: stopping (in ${host.dir})`);
      host.process.stop();
      this.update(host);
    } else if (host.forget && host.phase !== 'stopping') {
      this.drop(host);
    }
  }

  private drop(host: Hosted): void {
    host.status.dispose();
    this.hosts.delete(host.dir);
    this.updateAll();
  }

  private foldersChanged(e: vscode.WorkspaceFoldersChangeEvent): void {
    for (const host of [...this.hosts.values()]) {
      if (!vscode.workspace.getWorkspaceFolder(vscode.Uri.file(host.dir))) this.stop(host, true);
    }
    if (e.added.length > 0) this.autoStart();
  }

  private running(dir: string): boolean {
    const phase = this.hosts.get(dir)?.phase;
    return phase === 'starting' || phase === 'serving' || phase === 'stopping';
  }

  // --- What the host says ---

  private event(host: Hosted, e: HostEvent): void {
    if (this.disposed || this.hosts.get(host.dir) !== host) return;
    host.state.apply(e);
    switch (e.event) {
      case 'serving':
        if (host.phase === 'starting') host.phase = 'serving';
        this.log.appendLine(`axx host: serving ${e.dir} on ${e.addr}: ${e.services.join(', ')}${e.changed ? ' (changed since you last approved them: it asks you)' : ''}`);
        if (e.warnings.length > 0) {
          void vscode.window.showWarningMessage(`axx host: the services reach past their containers: ${e.warnings.join('; ')}`);
        }
        break;
      case 'run':
        this.log.appendLine(`run ${e.run}: ${runStateText(e)}`);
        this.follow(host, e);
        break;
      case 'report':
        host.runs.get(e.run)?.line(e.line);
        return; // no news for the status bar
      case 'approval':
        this.log.appendLine(`${e.run ? `run ${e.run}: asks` : 'asks'} you to approve the services' changed definition`);
        void this.ask(host, e.id);
        break;
      case 'answered':
        this.log.appendLine(`${e.run ? `run ${e.run}: ` : ''}${e.answer}`);
        // What the answer does: a question the host asks as it starts has no run to go on, so
        // nothing else shows it took.
        if (e.answer === 'approved') {
          void vscode.window.showInformationMessage('axx host: approved. Runs start the services as they are now.');
        } else if (e.answer === 'refused') {
          void vscode.window.showInformationMessage(
            'axx host: refused. Runs with this definition are refused until the host restarts; a newer change is asked about.',
          );
        }
        break;
    }
    this.update(host);
  }

  // Shows the runs the editor did not start as test runs of their own.
  private follow(host: Hosted, e: RunEvent): void {
    if (e.ide === IDE) return;
    let run = host.runs.get(e.run);
    if (!run) {
      const from = e.dir; // on the first event, queued
      const to = host.state.serving?.dir ?? host.dir;
      run = this.tests.follow(`axx host: run ${e.run} (${e.command})`, (file) => mapRunFile(file, from, to));
      host.runs.set(e.run, run);
    }
    run.output(`axx host: run ${e.run}: ${runStateText(e)}`);
    if (e.state === 'ended') {
      run.end(runError(e));
      host.runs.delete(e.run);
    }
  }

  private exited(host: Hosted, exit: HostExit): void {
    if (this.disposed || this.hosts.get(host.dir) !== host) return;
    this.output.info(`axx host exited with code ${exit.code ?? '(none)'}`);
    for (const run of host.runs.values()) run.end('The axx host stopped.');
    host.runs.clear();
    host.state.runs.clear();
    host.state.approvals.clear();
    for (const key of [...this.documents.keys()]) if (key.startsWith(`/${host.generation}/`)) this.documents.delete(key);
    if (host.forget) {
      this.drop(host);
      return;
    }
    const error = exit.error;
    if (exit.spawnError !== undefined) {
      host.phase = 'failed';
      host.detail = `Couldn't start axx host: ${exit.spawnError}`;
      this.showError(`axx: ${host.detail}`);
    } else if (error?.code === ALREADY_SERVED && !exit.served) {
      // One the person started in a terminal: the editor's runs go through it too.
      host.phase = 'elsewhere';
      host.detail = error.message;
      this.log.appendLine(`axx host: ${error.message}`);
    } else if (host.process.stopping || exit.code === 0) {
      host.phase = 'stopped';
      host.detail = host.process.stopping && exit.code !== 0 ? `It did not stop within ${STOP_MS / 1000} s, and was killed.` : '';
    } else {
      const text = error ? `${error.message}${error.code ? ` [${error.code}]` : ''}` : `it exited with code ${exit.code ?? '(none)'}`;
      host.phase = 'failed';
      host.detail = error?.hint ? `${text}\n${error.hint}` : text;
      this.showError(`axx host ${exit.served ? 'stopped' : "didn't start"}: ${text}${error?.hint ? ` (${error.hint})` : ''}`);
    }
    this.update(host);
  }

  private showError(message: string): void {
    void vscode.window.showErrorMessage(message, 'Show Log').then((choice) => {
      if (choice) this.log.show(true);
    });
  }

  // --- Approvals ---

  // Asks the person about an approval until they answer it, the host no longer asks (a newer change
  // replaced it), or they close the notification: axx: Show axx host approval asks again.
  private async ask(host: Hosted, id: number): Promise<void> {
    for (;;) {
      const approval = this.waiting(host, id);
      if (!approval) return;
      const choice = await vscode.window.showWarningMessage(approvalMessage(approval), SHOW_DIFF, APPROVE, REFUSE);
      if (choice === undefined) return;
      if (!this.waiting(host, id)) {
        if (choice !== SHOW_DIFF) void vscode.window.showInformationMessage('axx host: that question is no longer asked.');
        return;
      }
      if (choice === SHOW_DIFF) {
        await this.showDiff(host, approval);
        continue;
      }
      const approve = choice === APPROVE;
      if (host.process.answer(id, approve)) {
        this.log.appendLine(`${approval.run ? `run ${approval.run}: ` : ''}you ${approve ? 'approved' : 'refused'} the services' changed definition`);
      }
      return;
    }
  }

  // The approval `id` of a host that serves, while it waits for an answer.
  private waiting(host: Hosted, id: number): ApprovalEvent | undefined {
    return !this.disposed && this.hosts.get(host.dir) === host && host.phase === 'serving' ? host.state.approvals.get(id) : undefined;
  }

  // Opens the approved and the proposed definition side by side.
  private async showDiff(host: Hosted, a: ApprovalEvent): Promise<void> {
    const base = `/${host.generation}/${a.id}`;
    const approved = vscode.Uri.from({ scheme: SCHEME, path: `${base}/approved.yaml` });
    const proposed = vscode.Uri.from({ scheme: SCHEME, path: `${base}/proposed.yaml` });
    this.documents.set(approved.path, a.approved);
    this.documents.set(proposed.path, a.proposed);
    const title = `axx host: ${a.run ? `run ${a.run}'s services` : 'the services'} (${a.approved ? 'approved' : 'none approved'} ↔ proposed)`;
    await vscode.commands.executeCommand('vscode.diff', approved, proposed, title, { preview: true });
  }

  // Asks again about the approvals that wait for an answer: of the host in `dir` (from its status
  // bar item), or of every host. It answers none.
  private showApproval(dir?: string): void {
    const waiting = [...this.hosts.values()]
      .filter((h) => dir === undefined || h.dir === dir)
      .flatMap((h) => [...h.state.approvals.keys()].map((id) => ({ host: h, id })));
    if (waiting.length === 0) {
      void vscode.window.showInformationMessage('axx host: no run waits for your approval.');
      return;
    }
    for (const { host, id } of waiting) void this.ask(host, id);
  }

  // --- The status bar ---

  private updateAll(): void {
    for (const host of this.hosts.values()) this.update(host);
    this.updateContext();
  }

  private update(host: Hosted): void {
    const status = hostStatus(host.phase, host.state, host.detail, this.hosts.size > 1 ? path.basename(host.dir) : undefined);
    host.status.text = status.text;
    host.status.tooltip = status.tooltip;
    host.status.backgroundColor = status.approval !== undefined ? new vscode.ThemeColor('statusBarItem.warningBackground') : undefined;
    if (status.approval !== undefined) {
      host.status.command = { title: 'Show axx host approval', command: 'axx.showHostApproval', arguments: [host.dir] };
    } else if (host.phase === 'stopped') {
      host.status.command = { title: 'Start axx host', command: 'axx.startHost', arguments: [host.dir] };
    } else {
      host.status.command = { title: 'Show axx host log', command: 'axx.showHostLog' };
    }
    host.status.show();
    this.updateContext();
  }

  // The context keys that show Stop axx host and Show axx host approval in the Command Palette.
  private updateContext(): void {
    const hosts = [...this.hosts.values()];
    const running = hosts.some((h) => h.phase === 'starting' || h.phase === 'serving');
    const approval = hosts.some((h) => h.state.approvals.size > 0);
    if (this.context === `${running} ${approval}`) return;
    this.context = `${running} ${approval}`;
    void vscode.commands.executeCommand('setContext', 'axx.hostRunning', running);
    void vscode.commands.executeCommand('setContext', 'axx.hostApproval', approval);
  }
}

// Whether a directory is an axx project (it has an axx.yaml) in one of the workspace's folders.
function isProject(dir: string): boolean {
  return vscode.workspace.getWorkspaceFolder(vscode.Uri.file(dir)) !== undefined && CONFIG_FILES.some((name) => fs.existsSync(path.join(dir, name)));
}

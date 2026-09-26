// SPDX-License-Identifier: Apache-2.0
// The scenarios runs have paused (the paused lines packs print, see testing.ts): the paused one
// is the one that paused last and has not resumed. While one is paused, the element of the step
// under the cursor in a feature file is highlighted on its page, with the answer in the status bar;
// a CodeLens on each step line and axx.runStepInPausedScenario run a step in it. The steps recorded
// in Playwright's Inspector insert with axx.insertRecordedSteps: from the run while it runs, then
// from the recording file of its last pause. See pausedStep.ts for the requests.

import * as vscode from 'vscode';
import type { ScenarioPause } from './ideMarker';
import { featureSteps, highlightStep, indentOf, linesOf, recordedSteps, reindent, runStep, stepAt, stepWithTable } from './pausedStep';

const HIGHLIGHT_DELAY_MS = 200;
const STATUS_LENGTH = 80;

interface Pause {
  url: string;
  location?: string;
}

export class PausedScenario implements vscode.Disposable, vscode.CodeLensProvider {
  // By run, in the order they paused: the paused scenarios, and the URLs of the runs that paused
  // and still run. The last entry is the one that counts.
  private readonly pauses = new Map<object, Pause>();
  private readonly live = new Map<object, string>();
  private recording: string | undefined; // the recording file of the last pause
  private readonly status: vscode.StatusBarItem;
  private readonly lensesChanged = new vscode.EventEmitter<void>();
  readonly onDidChangeCodeLenses = this.lensesChanged.event;
  private readonly disposables: vscode.Disposable[] = [];
  private timer: ReturnType<typeof setTimeout> | undefined;
  private highlighted: string | undefined; // the URL and step last highlighted
  private asked = 0;

  constructor(private readonly output: vscode.LogOutputChannel) {
    this.status = vscode.window.createStatusBarItem('axx.pausedScenario', vscode.StatusBarAlignment.Left, 0);
    this.status.name = 'axx: paused scenario';
    this.disposables.push(
      this.status,
      this.lensesChanged,
      vscode.languages.registerCodeLensProvider({ scheme: 'file', pattern: '**/*.feature' }, this),
      vscode.commands.registerCommand('axx.runStepInPausedScenario', (uri?: vscode.Uri, line?: number) => this.runStep(uri, line)),
      vscode.commands.registerCommand('axx.insertRecordedSteps', () => this.insertRecordedSteps()),
      vscode.window.onDidChangeTextEditorSelection((e) => this.scheduleHighlight(e.textEditor)),
      vscode.window.onDidChangeActiveTextEditor((editor) => editor && this.scheduleHighlight(editor)),
    );
    this.changed();
  }

  dispose(): void {
    clearTimeout(this.timer);
    for (const d of this.disposables) d.dispose();
  }

  // A run's scenario paused.
  paused(run: object, pause: ScenarioPause): void {
    this.pauses.delete(run);
    this.pauses.set(run, { url: pause.url, location: pause.location });
    this.live.delete(run);
    this.live.set(run, pause.url);
    if (pause.recording) this.recording = pause.recording;
    this.output.info(`A scenario paused${pause.location ? ` at ${pause.location}` : ''} (${pause.url})`);
    this.changed();
  }

  // A run's scenario resumed.
  resumed(run: object, url: string): void {
    if (this.pauses.get(run)?.url !== url) return;
    this.pauses.delete(run);
    this.changed();
  }

  // A run's process ended.
  ended(run: object): void {
    const was = this.pauses.delete(run);
    if (this.live.delete(run) || was) this.changed();
  }

  private current(): Pause | undefined {
    return [...this.pauses.values()].at(-1);
  }

  private liveUrl(): string | undefined {
    return [...this.live.values()].at(-1);
  }

  private changed(): void {
    const pause = this.current();
    void vscode.commands.executeCommand('setContext', 'axx.paused', pause !== undefined);
    void vscode.commands.executeCommand('setContext', 'axx.hasRecording', this.liveUrl() !== undefined || this.recording !== undefined);
    this.lensesChanged.fire();
    this.highlighted = undefined;
    this.asked++;
    this.showPaused();
    const editor = vscode.window.activeTextEditor;
    if (pause && editor) this.scheduleHighlight(editor);
  }

  // --- The status bar ---

  private showPaused(): void {
    const pause = this.current();
    if (!pause) {
      this.status.hide();
      return;
    }
    this.status.text = `$(debug-pause) axx: paused${pause.location ? ` at ${pause.location}` : ''}`;
    this.status.tooltip =
      "A scenario is paused in Playwright's Inspector. Put the cursor on a step to highlight the element it names; run a step with Run in paused scenario.";
    this.status.show();
  }

  private showAnswer(text: string, found: boolean): void {
    const short = text.length > STATUS_LENGTH ? `${text.slice(0, STATUS_LENGTH - 1)}…` : text;
    this.status.text = `${found ? '$(target)' : '$(warning)'} ${short}`;
    this.status.tooltip = text;
    this.status.show();
  }

  // --- Highlighting the step under the cursor ---

  private scheduleHighlight(editor: vscode.TextEditor): void {
    if (!this.current() || !isFeature(editor.document)) return;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.highlight(editor), HIGHLIGHT_DELAY_MS);
  }

  private highlight(editor: vscode.TextEditor): void {
    const pause = this.current();
    if (!pause || editor.document.isClosed) return;
    const step = stepAt(linesOf(editor.document.getText()), editor.selection.active.line);
    if (step === undefined) {
      if (this.highlighted !== undefined) {
        this.highlighted = undefined;
        this.asked++;
        this.showPaused();
      }
      return;
    }
    const asked = `${pause.url}\n${step}`;
    if (asked === this.highlighted) return;
    this.highlighted = asked;
    const ask = ++this.asked;
    highlightStep(pause.url, step).then(
      (answer) => {
        if (ask !== this.asked || !this.current() || (answer.status !== 200 && answer.status !== 404)) return;
        this.showAnswer(answer.text.trim(), answer.status === 200);
      },
      () => undefined, // quiet: the scenario may have resumed in between
    );
  }

  // --- Running a step ---

  provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
    if (!this.current()) return [];
    const lenses: vscode.CodeLens[] = [];
    featureSteps(linesOf(document.getText())).forEach((step, line) => {
      if (step === undefined) return;
      lenses.push(
        new vscode.CodeLens(new vscode.Range(line, 0, line, 0), {
          title: 'Run in paused scenario',
          tooltip: 'Run this step, with its table, in the scenario paused in Playwright\'s Inspector',
          command: 'axx.runStepInPausedScenario',
          arguments: [document.uri, line],
        }),
      );
    });
    return lenses;
  }

  // Runs a step (from a CodeLens: its file and line; else the one under the cursor) in the paused
  // scenario.
  private async runStep(uri?: vscode.Uri, line?: number): Promise<void> {
    const pause = this.current();
    if (!pause) {
      void vscode.window.showWarningMessage('axx: no scenario is paused.');
      return;
    }
    const editor = vscode.window.activeTextEditor;
    const document = uri ? await vscode.workspace.openTextDocument(uri) : editor?.document;
    const at = uri && line !== undefined ? line : editor?.selection.active.line;
    const step = document && at !== undefined ? stepWithTable(linesOf(document.getText()), at) : undefined;
    if (step === undefined) {
      void vscode.window.showWarningMessage('axx: put the cursor on a step to run it in the paused scenario.');
      return;
    }
    const name = step.split('\n')[0];
    try {
      const answer = await vscode.window.withProgress(
        { location: vscode.ProgressLocation.Notification, title: `axx: running "${name}"`, cancellable: true },
        (_progress, token) => {
          const abort = new AbortController();
          token.onCancellationRequested(() => abort.abort());
          return runStep(pause.url, step, abort.signal);
        },
      );
      switch (answer.status) {
        case 200:
          void vscode.window.showInformationMessage(`Passed: ${name}`);
          break;
        case 422:
          void vscode.window.showErrorMessage(`Failed: ${name}. ${answer.text.trim()}`);
          break;
        case 409:
          void vscode.window.showWarningMessage('axx: no scenario is paused any more.');
          break;
        default:
          void vscode.window.showErrorMessage(`axx: the paused scenario answered ${answer.status}: ${answer.text.trim()}`);
      }
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      this.output.error(`Running "${name}" in the paused scenario failed: ${String(err)}`);
      void vscode.window.showErrorMessage(`axx: the paused scenario did not answer: ${(err as Error).message}`);
    }
  }

  // --- Inserting recorded steps ---

  // Inserts the recorded steps below the cursor's line, indented as the step there (else as
  // recorded).
  private async insertRecordedSteps(): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor || !isFeature(editor.document)) return;
    const recorded = await recordedSteps(this.liveUrl(), this.recording);
    if (recorded === undefined || recorded.trim() === '') {
      void vscode.window.showInformationMessage(
        "axx: no steps recorded yet. Record them in Playwright's Inspector while the scenario is paused.",
      );
      return;
    }
    const document = editor.document;
    const line = editor.selection.active.line;
    const lines = linesOf(document.getText());
    const indent = stepAt(lines, line) !== undefined ? indentOf(lines[line]) : undefined;
    const eol = document.eol === vscode.EndOfLine.CRLF ? '\r\n' : '\n';
    const steps = reindent(recorded, indent).split('\n').join(eol);
    const at = document.lineAt(line).range.end;
    const text = document.getText().length === 0 ? steps : eol + steps;
    if (!(await editor.edit((edit) => edit.insert(at, text)))) return;
    const end = document.positionAt(document.offsetAt(at) + text.length);
    editor.selection = new vscode.Selection(end, end);
    editor.revealRange(new vscode.Range(end, end));
  }
}

function isFeature(document: vscode.TextDocument): boolean {
  return document.uri.scheme === 'file' && document.uri.fsPath.endsWith('.feature');
}

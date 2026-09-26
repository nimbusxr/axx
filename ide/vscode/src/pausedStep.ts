// SPDX-License-Identifier: Apache-2.0
// What the editor does with a scenario a run paused (see pausedScenario.ts), without VS Code: the
// steps in a feature file's text, and the requests to the paused scenario.
//
// A step's line starts with Given, When, Then, And, But or `*` and more text, in a Background,
// Scenario, Scenario Outline or Example, outside doc strings. English keywords only, as in
// gherkin.ts. Lines are 0-based.
//
// The paused scenario answers at the URL its run announced (paused, see ideMarker.ts), on the
// loopback interface, with plain text bodies:
//   POST <url>highlight  a step's text: highlights the element it names on the paused page. 200 with
//                        a description of it, 404 with what the step would fail with, 409 when no
//                        scenario is paused.
//   POST <url>run        a step's text, and the rows of its table on the lines below: runs it in the
//                        paused scenario. 200 "passed", 422 with its failure, 409 when none is paused.
//   GET <url>recorded    the steps recorded in Playwright's Inspector so far.

import * as fs from 'node:fs';
import * as http from 'node:http';
import { isLoopbackUrl } from './ideMarker';

const STEP = /^\s*(?:Given|When|Then|And|But|\*)\s+\S.*$/;
const WITH_STEPS = /^\s*(?:Background|Scenario Outline|Scenario Template|Scenario|Example)\s*:/;
const WITHOUT_STEPS = /^\s*(?:Feature|Business Need|Ability|Rule|Examples|Scenarios)\s*:/;

// How long a highlight or the recorded steps may take.
export const QUICK_MS = 2000;

// The lines of a text.
export function linesOf(text: string): string[] {
  return text.split(/\r\n|\r|\n/);
}

// The steps of a feature file's lines: for each line, its step, trimmed, or undefined. Debug pauses
// only at the breakpoints on their lines (see pauseSteps in browsers.ts).
export function featureSteps(lines: readonly string[], upTo = lines.length - 1): (string | undefined)[] {
  const steps: (string | undefined)[] = [];
  let inSteps = false;
  let docString: string | undefined; // the open doc string's delimiter
  for (let i = 0; i <= upTo && i < lines.length; i++) {
    const trimmed = lines[i].trim();
    steps.push(undefined);
    if (docString !== undefined) {
      if (trimmed.startsWith(docString)) docString = undefined;
    } else if (trimmed.startsWith('"""') || trimmed.startsWith('```')) {
      docString = trimmed.slice(0, 3);
    } else if (WITH_STEPS.test(trimmed)) {
      inSteps = true;
    } else if (WITHOUT_STEPS.test(trimmed)) {
      inSteps = false;
    } else if (inSteps && STEP.test(trimmed)) {
      steps[i] = trimmed;
    }
  }
  return steps;
}

// The step on a line, trimmed, such as `And the "Get a quote" button is clicked`; else undefined.
export function stepAt(lines: readonly string[], line: number): string | undefined {
  return line < 0 || line >= lines.length ? undefined : featureSteps(lines, line)[line];
}

// The step on a line with the rows of its data table, the lines starting with `|` right below it,
// each trimmed, one per line; undefined when the line is not a step's.
export function stepWithTable(lines: readonly string[], line: number): string | undefined {
  const step = stepAt(lines, line);
  if (step === undefined) return undefined;
  const rows: string[] = [];
  for (let i = line + 1; i < lines.length && lines[i].trim().startsWith('|'); i++) rows.push(lines[i].trim());
  return [step, ...rows].join('\n');
}

// The whitespace a line starts with.
export function indentOf(line: string): string {
  return /^\s*/.exec(line)![0];
}

// Recorded steps, re-indented: their lines lose the indentation they share and start with `indent`
// instead, keeping the rest; blank lines become empty, and blank lines at the start and end are
// dropped. Without an indent, the lines keep theirs.
export function reindent(steps: string, indent?: string): string {
  const lines = linesOf(steps);
  while (lines.length > 0 && lines[0].trim() === '') lines.shift();
  while (lines.length > 0 && lines[lines.length - 1].trim() === '') lines.pop();
  const common = Math.min(...lines.filter((l) => l.trim() !== '').map((l) => indentOf(l).length));
  return lines
    .map((l) => (l.trim() === '' ? '' : indent === undefined ? l.trimEnd() : indent + l.slice(common).trimEnd()))
    .join('\n');
}

// The recorded steps in the text of the recording file a pause names: without its first line when
// that is a comment (which says what the file is).
export function fromRecordingFile(text: string): string {
  const eol = text.indexOf('\n');
  const first = eol < 0 ? text : text.slice(0, eol);
  if (!first.trim().startsWith('#')) return text;
  return eol < 0 ? '' : text.slice(eol + 1);
}

// An answer of the paused scenario: its HTTP status and text.
export interface Answer {
  status: number;
  text: string;
}

// Highlights the element a step names on the paused page.
export function highlightStep(url: string, step: string): Promise<Answer> {
  return send(url, 'highlight', step, QUICK_MS);
}

// Runs a step, with the rows of its table on the lines below it, in the paused scenario. It takes as
// long as the step does; `signal` gives up waiting.
export function runStep(url: string, step: string, signal?: AbortSignal): Promise<Answer> {
  return send(url, 'run', step, undefined, signal);
}

// The steps recorded in Playwright's Inspector so far.
export function recorded(url: string): Promise<Answer> {
  return send(url, 'recorded', undefined, QUICK_MS);
}

// The recorded steps to insert: from the run while it runs (`liveUrl`), else from the recording file
// of its last pause, without the file's first comment line; undefined when neither has them.
export async function recordedSteps(liveUrl: string | undefined, recording: string | undefined): Promise<string | undefined> {
  if (liveUrl !== undefined) {
    try {
      const answer = await recorded(liveUrl);
      if (answer.status === 200) return answer.text;
    } catch {
      // The run has ended: its file has the steps.
    }
  }
  if (recording === undefined) return undefined;
  try {
    return fromRecordingFile(await fs.promises.readFile(recording, 'utf8'));
  } catch {
    return undefined;
  }
}

function send(url: string, path: string, body: string | undefined, timeoutMs?: number, signal?: AbortSignal): Promise<Answer> {
  if (!isLoopbackUrl(url)) return Promise.reject(new Error(`not a paused scenario's URL: ${url}`));
  return new Promise((resolve, reject) => {
    const data = body === undefined ? undefined : Buffer.from(body, 'utf8');
    const req = http.request(
      `${url}${path}`,
      {
        method: data ? 'POST' : 'GET',
        headers: data ? { 'Content-Type': 'text/plain; charset=utf-8', 'Content-Length': data.length } : {},
        signal,
      },
      (res) => {
        res.setEncoding('utf8');
        let text = '';
        res.on('data', (chunk: string) => (text += chunk));
        res.on('end', () => {
          clearTimeout(timer);
          resolve({ status: res.statusCode ?? 0, text });
        });
        res.on('error', (err) => {
          clearTimeout(timer);
          reject(err);
        });
      },
    );
    const timer = timeoutMs === undefined ? undefined : setTimeout(() => req.destroy(new Error(`no answer in ${timeoutMs} ms`)), timeoutMs);
    req.on('error', (err) => {
      clearTimeout(timer);
      reject(err);
    });
    req.end(data);
  });
}

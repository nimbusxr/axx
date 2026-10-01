// SPDX-License-Identifier: Apache-2.0
// Watching and debugging the web pack's browsers, and opening what their scenarios keep: the
// `axx run` arguments, with the steps to pause before, and the trace viewer's page for a trace.

import * as fs from 'node:fs';
import * as path from 'node:path';
import { featureSteps } from './pausedStep';

// The arguments of an `axx run` from the editor. Watching (watchSlowdownMs set) only shows the
// browsers: one scenario at a time, in windows on the desktop, with a wait after every action (none
// for 0). Debugging pauses the web pack's scenarios in Playwright's Inspector, one scenario at a
// time: where a scenario fails (`packs.web-core.pauseOnFailure`, which shows its browser), and
// before each step of pauseAt (see pauseSteps), which only debugging uses. With debugSteps, it also
// adds `--debug-steps` to debug step code, for the Go debugger to attach to. Projects without the
// web pack ignore these settings. Profiles apply in their order (`--profile`).
export function runArguments(
  targets: string[],
  options: { debug?: boolean; debugSteps?: boolean; watchSlowdownMs?: number; pauseAt?: string[]; profiles?: string[] } = {},
): string[] {
  const slowdownMs = options.watchSlowdownMs;
  const debug = options.debug === true;
  const pauseAt = debug ? (options.pauseAt ?? []).flatMap((step) => ['--pause-at', step]) : [];
  return [
    'run',
    '--format',
    'teamcity',
    ...(debug && options.debugSteps === true ? ['--debug-steps'] : []),
    ...(slowdownMs !== undefined || debug ? ['--workers', '1'] : []),
    ...(slowdownMs !== undefined ? watchSettings(slowdownMs) : []),
    ...(debug ? ['--set', 'packs.web-core.pauseOnFailure=true', ...pauseAt] : []),
    ...targets,
    ...(options.profiles !== undefined && options.profiles.length > 0 ? ['--profile', options.profiles.join(',')] : []),
  ];
}

// A breakpoint in a feature file.
export interface StepBreakpoint {
  file: string;
  line: number; // 1-based
}

// What a debugging run in `project` does with the breakpoints in feature files. `files`: the lines
// of the feature files it runs. It pauses before the steps that have one (pauseAt, as `--pause-at`
// values written like its targets), and ignores the ones on the other lines of those files
// (notSteps). A step's line starts with a step's keyword, in a background or a scenario (outlines
// included), outside doc strings (see featureSteps), as in the IntelliJ plugin. Both are in file and
// line order. The breakpoints on other lines, and those in the files it does not run, could never
// pause the run, and would still make axx run as if it could: with the browsers shown, and no
// timeouts.
export function pauseSteps(
  breakpoints: StepBreakpoint[],
  project: string,
  files: ReadonlyMap<string, readonly string[]>,
): { pauseAt: string[]; notSteps: StepBreakpoint[] } {
  const steps = new Map([...files].map(([file, lines]) => [file, featureSteps(lines)]));
  const pauseAt: StepBreakpoint[] = [];
  const notSteps: StepBreakpoint[] = [];
  for (const b of breakpoints) {
    const fileSteps = steps.get(b.file);
    if (!fileSteps || b.line < 1) continue;
    (fileSteps[b.line - 1] !== undefined ? pauseAt : notSteps).push(b);
  }
  return {
    pauseAt: inOrder(project, pauseAt).map(([where]) => where),
    notSteps: inOrder(project, notSteps).map(([, b]) => b),
  };
}

// What a debugging run says about a breakpoint it ignores, which is not on a step.
export function notAStep(project: string, b: StepBreakpoint): string {
  return `Ignored the breakpoint at ${written(project, b)}: it is not on a step. Debug pauses only at breakpoints on steps.`;
}

// Breakpoints in file and line order, once each, with where they are as a run's targets write it.
function inOrder(project: string, breakpoints: StepBreakpoint[]): [string, StepBreakpoint][] {
  const sorted = [...breakpoints].sort((x, y) => {
    const [a, b] = [relative(project, x.file), relative(project, y.file)];
    return a < b ? -1 : a > b ? 1 : x.line - y.line;
  });
  return [...new Map(sorted.map((b): [string, StepBreakpoint] => [written(project, b), b]))];
}

// A breakpoint's file and line, as a run's targets write them.
function written(project: string, b: StepBreakpoint): string {
  return `${relative(project, b.file)}:${b.line}`;
}

function relative(project: string, file: string): string {
  return path.relative(project, file) || file;
}

// Watching shows what the scenarios do, browsers and devices in windows on the desktop, and waits
// after every action (not for 0).
function watchSettings(slowdownMs: number): string[] {
  const args = ['--watch'];
  const ms = Math.floor(slowdownMs);
  if (ms > 0) args.push('--slowdown', `${ms}ms`);
  return args;
}

// The page of the trace viewer's files, served at a URL, that loads the trace at a URL.
export function traceViewerUrl(files: string, trace: string): string {
  return `${files.replace(/\/*$/, '/')}index.html?trace=${encodeURIComponent(trace)}`;
}

// The first of the trace viewer's folders that still has the viewer.
export function viewerFolder(...dirs: (string | undefined)[]): string | undefined {
  return dirs.find((dir) => dir !== undefined && fs.existsSync(path.join(dir, 'index.html')));
}

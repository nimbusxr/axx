// SPDX-License-Identifier: Apache-2.0
// Finds the axx executable (the axx.path setting) for the language server and for test runs.

import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import * as vscode from 'vscode';

const INSTALL_URL = 'https://axx.nimbusxr.us/guides/install/';

let missingReported = false;

// Returns the axx executable for a directory, or undefined after reporting that it is missing
// (the notification once, until resetMissingReport).
export function resolveAxx(scope: vscode.Uri, dir: string, output: vscode.LogOutputChannel): string | undefined {
  const setting = vscode.workspace.getConfiguration('axx', scope).get<string>('path', 'axx');
  const command = findExecutable(setting, dir);
  if (command === undefined) reportMissing(setting, output);
  return command;
}

export function resetMissingReport(): void {
  missingReported = false;
}

// Resolves axx.path the way spawning it would: a bare name is looked up on PATH, a relative path
// is relative to `dir`, and a leading ~ is the home directory.
export function findExecutable(setting: string, dir: string): string | undefined {
  let value = setting.trim() || 'axx';
  if (value === '~' || value.startsWith('~/') || value.startsWith('~\\')) {
    value = path.join(os.homedir(), value.slice(1));
  }
  const candidates = /[\\/]/.test(value)
    ? [path.resolve(dir, value)]
    : (process.env.PATH ?? '')
        .split(path.delimiter)
        .filter(Boolean)
        .map((d) => path.join(d, value));
  const extensions = process.platform === 'win32' ? ['.exe', ''] : [''];
  for (const candidate of candidates) {
    for (const ext of extensions) {
      if (isExecutableFile(candidate + ext)) {
        return candidate + ext;
      }
    }
  }
  return undefined;
}

function isExecutableFile(file: string): boolean {
  try {
    fs.accessSync(file, fs.constants.X_OK);
    return fs.statSync(file).isFile();
  } catch {
    return false;
  }
}

function reportMissing(setting: string, output: vscode.LogOutputChannel): void {
  const message =
    `axx: can't find the axx executable "${setting}". Set axx.path to it, or install axx and ` +
    `run "axx: Restart language server".`;
  output.error(message);
  if (missingReported) {
    return;
  }
  missingReported = true;
  void vscode.window.showErrorMessage(message, 'Install axx', 'Open Settings').then((choice) => {
    if (choice === 'Install axx') {
      void vscode.env.openExternal(vscode.Uri.parse(INSTALL_URL));
    } else if (choice === 'Open Settings') {
      void vscode.commands.executeCommand('workbench.action.openSettings', 'axx.path');
    }
  });
}

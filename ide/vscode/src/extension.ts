// SPDX-License-Identifier: Apache-2.0
// The axx extension for VS Code. Highlighting for .feature files is declarative (package.json,
// syntaxes/, language-configuration.json). The code adds the axx language server (lsp.ts) and
// running and debugging scenarios from the editor and the Testing view (testing.ts).

import * as vscode from 'vscode';
import { activateLanguageServer, deactivateLanguageServer } from './lsp';
import { AxxTests } from './testing';

export function activate(context: vscode.ExtensionContext): void {
  const output = vscode.window.createOutputChannel('axx', { log: true });
  context.subscriptions.push(output);
  activateLanguageServer(context, output);
  context.subscriptions.push(new AxxTests(output));
}

export function deactivate(): Promise<void> {
  return deactivateLanguageServer();
}

// SPDX-License-Identifier: Apache-2.0
// The axx extension for VS Code. Highlighting for .feature files is declarative (package.json,
// syntaxes/, language-configuration.json). The code adds the axx language server (lsp.ts),
// running and debugging scenarios from the editor and the Testing view (testing.ts), the axx
// host, which starts the services of an agent's runs (axxHost.ts), and axx.yaml's schemas, those
// of the axx the project runs on, for the YAML extension (yamlSchemas.ts).

import * as vscode from 'vscode';
import { AxxHost } from './axxHost';
import { activateLanguageServer, deactivateLanguageServer } from './lsp';
import { AxxTests } from './testing';
import { activateSchemas } from './yamlSchemas';

export function activate(context: vscode.ExtensionContext): void {
  const output = vscode.window.createOutputChannel('axx', { log: true });
  context.subscriptions.push(output);
  activateLanguageServer(context, output);
  void activateSchemas(context, output);
  const tests = new AxxTests(output);
  context.subscriptions.push(tests, new AxxHost(output, tests));
}

export function deactivate(): Promise<void> {
  return deactivateLanguageServer();
}

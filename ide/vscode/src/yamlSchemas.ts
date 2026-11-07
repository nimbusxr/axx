// SPDX-License-Identifier: Apache-2.0
// Gives Red Hat's YAML extension axx's schemas for axx.yaml and the fixture spec files: those of
// the axx the project runs on (schemas.ts), or this extension's own until that axx has given them.
// They are read again when the axx may have changed: when the window comes back to the front
// (after an upgrade in a terminal, say), when an axx.yaml is saved, and when axx.path changes.

import * as path from 'node:path';
import * as vscode from 'vscode';
import { findExecutable } from './axx';
import { ProjectSchemas, SCHEME } from './schemas';

// The part of the YAML extension's API this uses (its schema-extension-api.ts). The URIs
// requestSchema gives have the scheme registered, so the YAML extension asks requestSchemaContent
// for their text.
interface YamlExtensionApi {
  registerContributor(
    schema: string,
    requestSchema: (resource: string) => string | undefined,
    requestSchemaContent: (uri: string) => string | undefined,
  ): boolean;
}

export async function activateSchemas(context: vscode.ExtensionContext, output: vscode.LogOutputChannel): Promise<void> {
  const yaml = vscode.extensions.getExtension<YamlExtensionApi>('redhat.vscode-yaml');
  if (yaml === undefined) return;
  let api: YamlExtensionApi;
  try {
    api = await yaml.activate();
  } catch (err) {
    output.warn(`axx: the YAML extension did not start, so axx.yaml has no completion: ${String(err)}`);
    return;
  }
  const schemas = new ProjectSchemas(
    path.join(context.extensionPath, 'dist', 'schemas'),
    (dir) => findExecutable(vscode.workspace.getConfiguration('axx', vscode.Uri.file(dir)).get<string>('path', 'axx'), dir),
    undefined,
    (dir) => output.info(`axx: the schemas of the axx that ${dir} runs on changed; axx.yaml's completion follows them`),
  );
  api.registerContributor(
    SCHEME,
    (resource) => {
      const uri = vscode.Uri.parse(resource);
      return uri.scheme === 'file' ? schemas.uri(uri.fsPath) : undefined;
    },
    (uri) => schemas.content(uri),
  );
  context.subscriptions.push(
    vscode.window.onDidChangeWindowState((state) => {
      if (state.focused) void schemas.refreshOnFront();
    }),
    vscode.workspace.onDidSaveTextDocument((doc) => {
      if (/(^|[\\/])axx\.ya?ml$/.test(doc.fileName)) void schemas.refreshAll();
    }),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('axx.path')) void schemas.refreshAll();
    }),
  );
  for (const file of await vscode.workspace.findFiles('**/axx.{yaml,yml}', '**/node_modules/**', 20)) {
    void schemas.refresh(path.dirname(file.fsPath));
  }
}

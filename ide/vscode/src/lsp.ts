// SPDX-License-Identifier: Apache-2.0
// Runs the axx language server, `axx lsp`, which knows every step compiled into the axx binary,
// custom packs included.

import * as path from 'node:path';
import * as vscode from 'vscode';
import { LanguageClient, type LanguageClientOptions, type ServerOptions } from 'vscode-languageclient/node';
import { resetMissingReport, resolveAxx } from './axx';

const LANGUAGE_ID = 'feature';

// Where one server runs: the outermost workspace folder that holds the document, or the
// document's own directory when it is in no folder. `axx lsp` finds the axx project (axx.yaml)
// from its working directory.
interface Root {
  dir: string;
  folder?: vscode.WorkspaceFolder;
}

interface Server {
  client: LanguageClient;
  started: Promise<unknown>; // settles once the start attempt has succeeded or failed
}

// Servers by root directory. `undefined` marks a root whose server could not be started (no axx
// executable), so opening more files does not repeat the error. Restarting clears the map.
const servers = new Map<string, Server | undefined>();
let output: vscode.LogOutputChannel;

export function activateLanguageServer(context: vscode.ExtensionContext, channel: vscode.LogOutputChannel): void {
  output = channel;
  context.subscriptions.push(
    vscode.commands.registerCommand('axx.restartLanguageServer', () => restart(true)),
    vscode.workspace.onDidOpenTextDocument(ensureServer),
    vscode.workspace.onDidGrantWorkspaceTrust(startForOpenDocuments),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('axx.path')) void restart(false);
    }),
    vscode.workspace.onDidChangeWorkspaceFolders(() => void restart(false)),
  );
  if (!vscode.workspace.isTrusted) {
    output.info('The workspace is not trusted: the axx language server starts once you trust it.');
  }
  startForOpenDocuments();
}

export function deactivateLanguageServer(): Promise<void> {
  return stopAll();
}

function startForOpenDocuments(): number {
  let started = 0;
  for (const doc of vscode.workspace.textDocuments) {
    if (ensureServer(doc)) started++;
  }
  return started;
}

// Starts the server for the document's root unless it has one. Returns whether it started one.
function ensureServer(doc: vscode.TextDocument): boolean {
  // `axx lsp` may build and run the project's custom packs, so only in trusted workspaces.
  if (doc.languageId !== LANGUAGE_ID || doc.uri.scheme !== 'file' || !vscode.workspace.isTrusted) {
    return false;
  }
  const root = rootOf(doc.uri);
  if (servers.has(root.dir)) {
    return false;
  }
  const server = startServer(root);
  servers.set(root.dir, server);
  return server !== undefined;
}

function startServer(root: Root): Server | undefined {
  const command = resolveAxx(root.folder?.uri ?? vscode.Uri.file(root.dir), root.dir, output);
  if (command === undefined) {
    return undefined;
  }
  // Stdio is the default transport. It is not named on purpose: with `transport` set, the
  // client would append `--stdio` to the arguments.
  const serverOptions: ServerOptions = { command, args: ['lsp'], options: { cwd: root.dir } };
  const clientOptions: LanguageClientOptions = {
    documentSelector: [
      {
        scheme: 'file',
        language: LANGUAGE_ID,
        pattern: { baseUri: vscode.Uri.file(root.dir).toString(), pattern: root.folder ? '**/*' : '*' },
      },
    ],
    workspaceFolder: root.folder,
    outputChannel: output,
  };
  const client = new LanguageClient('axx', 'axx', serverOptions, clientOptions);
  output.info(`Starting "${command} lsp" in ${root.dir}`);
  // The client reports a failed start itself, in a notification and the axx output channel.
  const started = client.start().catch(() => undefined);
  return { client, started };
}

async function restart(manual: boolean): Promise<void> {
  await stopAll();
  if (manual) {
    resetMissingReport(); // automatic restarts (settings edits) don't repeat the notification
  }
  if (!vscode.workspace.isTrusted) {
    if (manual) {
      void vscode.window.showWarningMessage('axx: the language server runs only in trusted workspaces.');
    }
    return;
  }
  const started = startForOpenDocuments();
  if (manual && started > 0) {
    vscode.window.setStatusBarMessage('axx: language server restarted', 3000);
  }
}

async function stopAll(): Promise<void> {
  const running = [...servers.values()].filter((s): s is Server => s !== undefined);
  servers.clear();
  await Promise.all(running.map(stopServer));
}

async function stopServer(server: Server): Promise<void> {
  // A client can be stopped only once it runs, so let a start in progress finish first. A server
  // that never answers is killed.
  await Promise.race([server.started, new Promise((resolve) => setTimeout(resolve, 15_000))]);
  try {
    await server.client.dispose(2000);
  } catch {
    server.client.serverProcess?.kill();
  }
}

function rootOf(uri: vscode.Uri): Root {
  // Nested workspace folders share the server of the outermost one.
  let folder: vscode.WorkspaceFolder | undefined;
  for (const f of vscode.workspace.workspaceFolders ?? []) {
    if (f.uri.scheme === 'file' && isInside(uri.fsPath, f.uri.fsPath)) {
      if (folder === undefined || f.uri.fsPath.length < folder.uri.fsPath.length) {
        folder = f;
      }
    }
  }
  return folder ? { dir: folder.uri.fsPath, folder } : { dir: path.dirname(uri.fsPath) };
}

function isInside(file: string, dir: string): boolean {
  const rel = path.relative(dir, file);
  return rel !== '' && rel !== '..' && !rel.startsWith(`..${path.sep}`) && !path.isAbsolute(rel);
}

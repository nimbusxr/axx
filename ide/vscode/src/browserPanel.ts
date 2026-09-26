// SPDX-License-Identifier: Apache-2.0
// Shows web pages in editor tabs: Playwright's trace viewer, and videos. Each kind has one tab,
// reused: showing another page there replaces the one it shows. Pages run in a frame of a webview,
// as in the Simple Browser, under a bar with the address, Reload and Open in Browser.

import { randomBytes } from 'node:crypto';
import * as vscode from 'vscode';

export type Tab = 'trace' | 'video';

interface Shown {
  panel: vscode.WebviewPanel;
  url: string;
}

export class BrowserPanels implements vscode.Disposable {
  private readonly tabs = new Map<Tab, Shown>();

  // Shows a page (a video: plays it) in the tab of its kind, beside the editor.
  show(tab: Tab, title: string, url: string): void {
    let shown = this.tabs.get(tab);
    if (shown) {
      shown.panel.reveal();
    } else {
      const panel = vscode.window.createWebviewPanel(
        `axx.${tab}`,
        title,
        vscode.ViewColumn.Beside,
        // Kept while hidden: the trace viewer would load the trace again, and a video start over.
        { enableScripts: true, retainContextWhenHidden: true },
      );
      const created: Shown = { panel, url };
      panel.onDidDispose(() => this.tabs.delete(tab));
      panel.webview.onDidReceiveMessage((message: { type?: string }) => {
        if (message?.type === 'open') void vscode.env.openExternal(vscode.Uri.parse(created.url));
      });
      this.tabs.set(tab, created);
      shown = created;
    }
    shown.url = url;
    shown.panel.title = title;
    shown.panel.webview.html = pageHtml(url, tab === 'video');
  }

  dispose(): void {
    for (const { panel } of [...this.tabs.values()]) panel.dispose();
  }
}

function pageHtml(url: string, video: boolean): string {
  const nonce = randomBytes(16).toString('base64');
  const src = escapeHtml(url);
  const content = video
    ? `<video id="page" src="${src}" controls autoplay muted></video>`
    : `<iframe id="page" src="${src}" allow="clipboard-read; clipboard-write; fullscreen"></iframe>`;
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; frame-src http: https:; media-src http: https:; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';">
<style>
  html, body { height: 100%; margin: 0; padding: 0; overflow: hidden; }
  body { display: flex; flex-direction: column; background: var(--vscode-editor-background); }
  header { display: flex; align-items: center; gap: 6px; padding: 4px 8px; border-bottom: 1px solid var(--vscode-panel-border);
    color: var(--vscode-foreground); font-family: var(--vscode-font-family); font-size: var(--vscode-font-size); }
  header span { flex: 1; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; opacity: 0.8; }
  button { border: none; padding: 3px 8px; cursor: pointer; font: inherit;
    color: var(--vscode-button-secondaryForeground); background: var(--vscode-button-secondaryBackground); }
  button:hover { background: var(--vscode-button-secondaryHoverBackground); }
  #page { flex: 1; width: 100%; min-height: 0; border: none; background: ${video ? '#000' : '#fff'}; }
</style>
</head>
<body>
<header><span title="${src}">${src}</span><button id="reload">Reload</button><button id="open">Open in Browser</button></header>
${content}
<script nonce="${nonce}">
  const vscode = acquireVsCodeApi();
  const page = document.getElementById('page');
  document.getElementById('reload').addEventListener('click', () => {
    if (page.load) page.load();
    else page.src = page.src;
  });
  document.getElementById('open').addEventListener('click', () => vscode.postMessage({ type: 'open' }));
</script>
</body>
</html>`;
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

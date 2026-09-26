// SPDX-License-Identifier: Apache-2.0
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as http from 'node:http';
import * as os from 'node:os';
import * as path from 'node:path';
import { after, before, test } from 'node:test';
import { byteRange, contentType, FileServer } from '../../src/fileServer';

let dir: string;
let trace: string;
let video: string;
let unannounced: string;
let viewer: string;
const server = new FileServer();

before(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), 'axx-files-'));
  fs.mkdirSync(path.join(dir, 'traces'));
  trace = path.join(dir, 'traces', 'register parcel.zip');
  video = path.join(dir, 'parcel.webm');
  unannounced = path.join(dir, 'traces', 'notes.txt');
  fs.writeFileSync(trace, 'PK trace bytes');
  fs.writeFileSync(video, '0123456789');
  fs.writeFileSync(unannounced, 'not announced');
  // The trace viewer's files, as the Playwright driver has them.
  viewer = path.join(dir, 'package', 'lib', 'vite', 'traceViewer');
  fs.mkdirSync(path.join(viewer, 'assets'), { recursive: true });
  for (const name of ['index.html', 'snapshot.html', 'sw.bundle.js', 'manifest.webmanifest', 'playwright-logo.svg']) {
    fs.writeFileSync(path.join(viewer, name), name);
  }
  fs.writeFileSync(path.join(viewer, 'assets', 'index-Bq3f.css'), 'css');
  fs.writeFileSync(path.join(viewer, 'assets', 'codicon.ttf'), 'ttf');
  fs.writeFileSync(path.join(viewer, 'assets', 'data.json'), '{}');
  fs.symlinkSync(unannounced, path.join(viewer, 'assets', 'link.txt')); // leads out of the folder
});

// A request exactly as written: fetch() would resolve dot segments before sending.
function raw(url: string, rawPath: string): Promise<number> {
  const { hostname, port } = new URL(url);
  return new Promise((resolve, reject) => {
    http
      .get({ hostname, port, path: rawPath }, (res) => {
        res.resume();
        resolve(res.statusCode ?? 0);
      })
      .on('error', reject);
  });
}

after(() => {
  server.dispose();
  fs.rmSync(dir, { recursive: true, force: true });
});

test('serves an announced file on 127.0.0.1, to any origin', async () => {
  const url = await server.url(trace);
  assert.match(url, /^http:\/\/127\.0\.0\.1:\d+\/[0-9a-f]{32}\/register_parcel\.zip$/);
  const res = await fetch(url, { headers: { Origin: 'https://trace.playwright.dev' } });
  assert.equal(res.status, 200);
  assert.equal(res.headers.get('access-control-allow-origin'), '*');
  assert.equal(res.headers.get('content-type'), 'application/zip');
  assert.equal(await res.text(), 'PK trace bytes');
  assert.equal(await server.url(trace), url, 'a file keeps its URL');
});

test('serves nothing else', async () => {
  const url = new URL(await server.url(trace));
  const base = `${url.protocol}//${url.host}`;
  const token = url.pathname.split('/')[1];
  for (const p of [
    '/',
    `/${token}/`,
    `/${token}/notes.txt`,
    `/${token}/../notes.txt`,
    `/${token}/register_parcel.zip/..`,
    '/0f1e2d3c4b5a69788796a5b4c3d2e1f0/register_parcel.zip',
    unannounced,
    `/${token}/%2e%2e/notes.txt`,
  ]) {
    const res = await fetch(base + p);
    assert.equal(res.status, 404, p);
    assert.equal(res.headers.get('access-control-allow-origin'), null, p);
    await res.arrayBuffer();
  }
  const other = await fetch(`${base}${url.pathname}`, { method: 'POST', body: 'x' });
  assert.equal(other.status, 405);
});

test('answers CORS preflights and HEAD', async () => {
  const url = await server.url(trace);
  const preflight = await fetch(url, {
    method: 'OPTIONS',
    headers: { Origin: 'https://trace.playwright.dev', 'Access-Control-Request-Method': 'GET', 'Access-Control-Request-Headers': 'range' },
  });
  assert.equal(preflight.status, 204);
  assert.equal(preflight.headers.get('access-control-allow-origin'), '*');
  const head = await fetch(url, { method: 'HEAD' });
  assert.equal(head.status, 200);
  assert.equal(head.headers.get('content-length'), String('PK trace bytes'.length));
});

test('serves byte ranges, for seeking in videos', async () => {
  const url = await server.url(video);
  const part = await fetch(url, { headers: { Range: 'bytes=2-5' } });
  assert.equal(part.status, 206);
  assert.equal(part.headers.get('content-range'), 'bytes 2-5/10');
  assert.equal(part.headers.get('content-type'), 'video/webm');
  assert.equal(await part.text(), '2345');
  const outside = await fetch(url, { headers: { Range: 'bytes=10-' } });
  assert.equal(outside.status, 416);
  assert.equal(outside.headers.get('content-range'), 'bytes */10');
  await outside.arrayBuffer();
});

test('a file deleted after it was announced is gone', async () => {
  const gone = path.join(dir, 'gone.webm');
  fs.writeFileSync(gone, 'x');
  const url = await server.url(gone);
  fs.rmSync(gone);
  const res = await fetch(url);
  assert.equal(res.status, 404);
  await res.arrayBuffer();
});

test("serves the trace viewer's files, with their content types", async () => {
  const base = await server.folderUrl(viewer);
  assert.match(base, /^http:\/\/127\.0\.0\.1:\d+\/[0-9a-f]{32}\/traceViewer\/$/);
  assert.equal(await server.folderUrl(viewer), base, 'a folder keeps its URL');
  for (const [name, type] of [
    ['index.html', 'text/html; charset=utf-8'],
    ['snapshot.html', 'text/html; charset=utf-8'],
    ['sw.bundle.js', 'text/javascript; charset=utf-8'], // a service worker needs a JavaScript type
    ['manifest.webmanifest', 'application/manifest+json'],
    ['playwright-logo.svg', 'image/svg+xml'],
    ['assets/index-Bq3f.css', 'text/css; charset=utf-8'],
    ['assets/codicon.ttf', 'font/ttf'],
    ['assets/data.json', 'application/json'],
  ]) {
    const res = await fetch(`${base}${name}?trace=x`);
    assert.equal(res.status, 200, name);
    assert.equal(res.headers.get('content-type'), type, name);
    assert.equal(await res.text(), fs.readFileSync(path.join(viewer, name), 'utf8'), name);
  }
  assert.equal(contentType('x.mjs'), 'text/javascript; charset=utf-8');
  assert.equal(contentType('x.woff2'), 'font/woff2');
  assert.equal(contentType('x.unknown'), 'application/octet-stream');
});

test('serves nothing outside the folder', async () => {
  const base = await server.folderUrl(viewer);
  const prefix = new URL(base).pathname; // /<token>/traceViewer/
  const token = prefix.split('/')[1];
  for (const p of [
    prefix,
    `${prefix}assets`,
    `${prefix}missing.js`,
    `${prefix}../../../../traces/notes.txt`,
    `${prefix}%2e%2e/%2e%2e/%2e%2e/%2e%2e/traces/notes.txt`,
    `${prefix}..%2f..%2f..%2f..%2ftraces%2fnotes.txt`,
    `${prefix}assets/..%5c..%5c..%5c..%5c..%5ctraces%5cnotes.txt`,
    `${prefix}${encodeURIComponent(unannounced)}`,
    `${prefix}/${unannounced}`,
    `${prefix}assets/link.txt`, // a link out of the folder
    `${prefix}index.html%00.js`,
    `${prefix}%E0%A4%A`,
    `/${token}/other/index.html`,
    `/${token}/index.html`,
    '/0f1e2d3c4b5a69788796a5b4c3d2e1f0/traceViewer/index.html',
  ]) {
    assert.equal(await raw(base, p), 404, p);
  }
  assert.equal(await raw(base, `${prefix}assets/../index.html`), 200, 'dot segments inside the folder are fine');
});

test('reads Range headers', () => {
  assert.equal(byteRange(undefined, 10), 'all');
  assert.deepEqual(byteRange('bytes=0-', 10), { start: 0, end: 9 });
  assert.deepEqual(byteRange('bytes=3-100', 10), { start: 3, end: 9 });
  assert.deepEqual(byteRange('bytes=-4', 10), { start: 6, end: 9 });
  assert.deepEqual(byteRange('bytes=-40', 10), { start: 0, end: 9 });
  assert.equal(byteRange('bytes=0-1,4-5', 10), 'all'); // several ranges: the whole file
  assert.equal(byteRange('items=0-1', 10), 'all');
  assert.equal(byteRange('bytes=5-2', 10), 'all');
  assert.equal(byteRange('bytes=10-', 10), undefined);
  assert.equal(byteRange('bytes=-0', 10), undefined);
  assert.equal(byteRange('bytes=0-', 0), undefined);
});

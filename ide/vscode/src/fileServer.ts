// SPDX-License-Identifier: Apache-2.0
// Serves the traces and videos that runs announce, and the trace viewer's files, over HTTP on
// 127.0.0.1: Playwright's trace viewer loads a trace only from a URL. It serves only the files and
// folders it is given, each at a path with a random token, a folder only the files under it, to any
// origin, and nothing else. It starts on first use, on a random port, and stops with the extension.

import { randomBytes } from 'node:crypto';
import * as fs from 'node:fs';
import * as http from 'node:http';
import type { AddressInfo } from 'node:net';
import * as path from 'node:path';

const TYPES: Record<string, string> = {
  '.zip': 'application/zip',
  '.webm': 'video/webm',
  '.mp4': 'video/mp4',
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json',
  '.map': 'application/json',
  '.webmanifest': 'application/manifest+json',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.ico': 'image/x-icon',
  '.ttf': 'font/ttf',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
};

// The content type of a file, by its extension.
export function contentType(file: string): string {
  return TYPES[path.extname(file).toLowerCase()] ?? 'application/octet-stream';
}

export class FileServer {
  private server: http.Server | undefined;
  private port: Promise<number> | undefined;
  private readonly files = new Map<string, string>(); // URL path -> file
  private readonly paths = new Map<string, string>(); // file -> URL path
  private readonly folders = new Map<string, string>(); // URL path prefix -> folder
  private readonly prefixes = new Map<string, string>(); // folder -> URL path prefix

  // The URL a file is served at. The same file keeps its URL. The URL has no characters that need
  // escaping, so it can be put in a query as it is.
  async url(file: string): Promise<string> {
    let urlPath = this.paths.get(file);
    if (urlPath === undefined) {
      urlPath = `/${randomBytes(16).toString('hex')}/${path.basename(file).replace(/[^\w.-]/g, '_')}`;
      this.paths.set(file, urlPath);
      this.files.set(urlPath, file);
    }
    return `http://127.0.0.1:${await this.start()}${urlPath}`;
  }

  // The URL the files under a folder are served below, ending with /. The same folder keeps it.
  async folderUrl(dir: string): Promise<string> {
    let prefix = this.prefixes.get(dir);
    if (prefix === undefined) {
      prefix = `/${randomBytes(16).toString('hex')}/${path.basename(dir).replace(/[^\w.-]/g, '_')}/`;
      this.prefixes.set(dir, prefix);
      this.folders.set(prefix, dir);
    }
    return `http://127.0.0.1:${await this.start()}${prefix}`;
  }

  dispose(): void {
    this.server?.close();
    this.server?.closeAllConnections();
    this.server = undefined;
    this.port = undefined;
  }

  private start(): Promise<number> {
    this.port ??= new Promise<number>((resolve, reject) => {
      const server = http.createServer((req, res) => this.serve(req, res));
      server.on('error', reject);
      server.listen(0, '127.0.0.1', () => resolve((server.address() as AddressInfo).port));
      this.server = server;
    }).catch((err: unknown) => {
      this.port = undefined;
      throw err;
    });
    return this.port;
  }

  private serve(req: http.IncomingMessage, res: http.ServerResponse): void {
    const urlPath = (req.url ?? '').split('?')[0];
    const file = this.files.get(urlPath) ?? this.inFolder(urlPath);
    let size = -1;
    try {
      const stat = file === undefined ? undefined : fs.statSync(file);
      if (stat?.isFile()) size = stat.size;
    } catch {
      // gone since it was announced
    }
    if (file === undefined || size < 0) {
      res.writeHead(404).end();
      return;
    }
    res.setHeader('Access-Control-Allow-Origin', '*');
    if (req.method === 'OPTIONS') {
      res.writeHead(204, { 'Access-Control-Allow-Methods': 'GET, HEAD', 'Access-Control-Allow-Headers': 'Range' }).end();
      return;
    }
    if (req.method !== 'GET' && req.method !== 'HEAD') {
      res.writeHead(405, { Allow: 'GET, HEAD, OPTIONS' }).end();
      return;
    }
    const range = byteRange(req.headers.range, size);
    if (range === undefined) {
      res.writeHead(416, { 'Content-Range': `bytes */${size}` }).end();
      return;
    }
    const { start, end } = range === 'all' ? { start: 0, end: size - 1 } : range;
    res.writeHead(range === 'all' ? 200 : 206, {
      'Content-Type': contentType(file),
      'X-Content-Type-Options': 'nosniff',
      'Content-Length': String(end - start + 1),
      'Accept-Ranges': 'bytes',
      'Cache-Control': 'no-store',
      ...(range === 'all' ? {} : { 'Content-Range': `bytes ${start}-${end}/${size}` }),
    });
    if (req.method === 'HEAD' || end < start) {
      res.end();
      return;
    }
    fs.createReadStream(file, { start, end })
      .on('error', () => res.destroy())
      .pipe(res);
  }

  private inFolder(urlPath: string): string | undefined {
    const m = /^(\/[0-9a-f]{32}\/[^/]+\/)(.*)$/.exec(urlPath);
    const dir = m ? this.folders.get(m[1]) : undefined;
    return dir === undefined ? undefined : fileIn(dir, m![2]);
  }
}

// The file under a served folder a URL path names, if it is in the folder, following no link out
// of it.
function fileIn(dir: string, rest: string): string | undefined {
  let name: string;
  try {
    name = decodeURIComponent(rest);
  } catch {
    return undefined;
  }
  if (name === '' || name.includes('\0')) return undefined;
  try {
    const root = fs.realpathSync(dir);
    const file = fs.realpathSync(path.resolve(root, name));
    const rel = path.relative(root, file);
    const outside = rel === '' || rel === '..' || rel.startsWith(`..${path.sep}`) || path.isAbsolute(rel);
    return outside ? undefined : file;
  } catch {
    return undefined; // no such file
  }
}

// The bytes a Range header asks for: 'all' without one, or with one this server answers with the
// whole file (several ranges, another unit, a malformed one); undefined when none of the file
// is in the range.
export function byteRange(header: string | undefined, size: number): { start: number; end: number } | 'all' | undefined {
  const m = header ? /^bytes=(\d*)-(\d*)$/.exec(header.trim()) : null;
  if (!m || (m[1] === '' && m[2] === '')) return 'all';
  if (m[1] === '') {
    const suffix = Number(m[2]);
    return suffix > 0 && size > 0 ? { start: Math.max(0, size - suffix), end: size - 1 } : undefined;
  }
  const start = Number(m[1]);
  const end = m[2] === '' ? size - 1 : Math.min(Number(m[2]), size - 1);
  if (m[2] !== '' && Number(m[2]) < start) return 'all';
  return start < size ? { start, end } : undefined;
}

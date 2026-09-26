// SPDX-License-Identifier: Apache-2.0
// Parses the lines axx prints for IDEs, one per line: the debugger requests,
//   [AXX-IDE] debug-attach-request name=axx-steps type=go host=127.0.0.1 port=2345
// and, when AXX_IDE is set, what packs print: the folder of Playwright's trace viewer and what
// their scenarios keep,
//   [AXX-IDE] trace-viewer dir=/cache/axx/web/playwright-.../traceViewer
//   [AXX-IDE] trace path=/p/.axx/web/traces/x.zip location=features/shop-portal.feature:42
//   [AXX-IDE] video path=/p/.axx/web/videos/x.webm location=features/shop-portal.feature:42
// and when a scenario pauses (before a step with a breakpoint, or at the step that failed) and resumes:
//   [AXX-IDE] paused url=http://127.0.0.1:53211/3f9c/ location=features/shop-portal.feature:24 recording=/p/.axx/web/recording.txt
//   [AXX-IDE] resumed url=http://127.0.0.1:53211/3f9c/
// The rules match the IntelliJ plugin's: the marker may follow other text, ANSI colors are
// ignored, fields come in any order, unknown keys and tokens without "=" are skipped, and the
// first value of a repeated key wins. A debugger request needs name, type, host and port
// (1-65535). In the packs' lines, spaces in values are written %20.

import * as path from 'node:path';

export interface DebugRequest {
  kind: 'attach' | 'listener';
  name: string;
  type: string;
  host: string;
  port: number;
}

// The folder of Playwright's trace viewer's files, which the IDE serves itself to open traces.
export interface TraceViewerAnnouncement {
  kind: 'trace-viewer';
  dir: string;
}

// A trace or a video a scenario kept. `file` is the scenario's feature file, relative to the
// project directory, and `line` its line.
export interface KeptFileAnnouncement {
  kind: 'trace' | 'video';
  path: string;
  file: string;
  line: number;
}

export type IdeAnnouncement = TraceViewerAnnouncement | KeptFileAnnouncement;

// A scenario that paused or resumed. `url` is where the paused scenario answers (see pausedStep.ts):
// an http URL on the loopback interface, ending with /. A pause may say where it paused (`location`,
// its feature file relative to the run's working directory, and line) and the absolute path of the
// file Playwright's Inspector writes the steps it records to (`recording`).
export interface ScenarioPause {
  kind: 'paused' | 'resumed';
  url: string;
  location?: string;
  recording?: string;
}

const MARKER = /\[AXX-IDE\]\s+(\S+)(.*)$/;

// The kind and fields of a marker line, or undefined when the line has no marker.
function parseMarker(line: string): { kind: string; fields: Map<string, string> } | undefined {
  const m = MARKER.exec(line.replace(/\x1b\[[0-9;]*[A-Za-z]/g, ''));
  if (!m) return undefined;
  const fields = new Map<string, string>();
  for (const token of m[2].trim().split(/\s+/)) {
    const eq = token.indexOf('=');
    if (eq <= 0) continue;
    const key = token.slice(0, eq);
    if (!fields.has(key)) fields.set(key, token.slice(eq + 1));
  }
  return { kind: m[1], fields };
}

export function parseDebugRequest(line: string): DebugRequest | undefined {
  const m = parseMarker(line);
  const kind = /^debug-(attach|listener)-request$/.exec(m?.kind ?? '')?.[1];
  if (!m || !kind) return undefined;
  const name = m.fields.get('name');
  const type = m.fields.get('type');
  const host = m.fields.get('host');
  const port = Number(m.fields.get('port'));
  if (!name || !type || !host || !Number.isInteger(port) || port < 1 || port > 65535) return undefined;
  return { kind: kind as DebugRequest['kind'], name, type, host, port };
}

export function parseIdeAnnouncement(line: string): IdeAnnouncement | undefined {
  const m = parseMarker(line);
  if (!m) return undefined;
  const value = (key: string): string | undefined => m.fields.get(key)?.replaceAll('%20', ' ') || undefined;
  switch (m.kind) {
    case 'trace-viewer': {
      const dir = value('dir');
      return dir && path.isAbsolute(dir) ? { kind: m.kind, dir } : undefined;
    }
    case 'trace':
    case 'video': {
      const file = value('path');
      const location = /^(.+):(\d+)$/.exec(value('location') ?? '');
      const line = Number(location?.[2]);
      if (!file || !path.isAbsolute(file) || !location || line < 1) return undefined;
      return { kind: m.kind, path: file, file: location[1], line };
    }
    default:
      return undefined;
  }
}

export function parseScenarioPause(line: string): ScenarioPause | undefined {
  const m = parseMarker(line);
  if (!m || (m.kind !== 'paused' && m.kind !== 'resumed')) return undefined;
  const value = (key: string): string | undefined => m.fields.get(key)?.replaceAll('%20', ' ') || undefined;
  const url = value('url');
  if (!url || !isLoopbackUrl(url)) return undefined;
  if (m.kind === 'resumed') return { kind: m.kind, url };
  const pause: ScenarioPause = { kind: m.kind, url };
  const location = value('location');
  const recording = value('recording');
  if (location) pause.location = location;
  if (recording && path.isAbsolute(recording)) pause.recording = recording;
  return pause;
}

// Whether a URL is one the editor sends a paused scenario's requests to: http, on the loopback
// interface, ending with /.
export function isLoopbackUrl(url: string): boolean {
  let u: URL;
  try {
    u = new URL(url);
  } catch {
    return false;
  }
  return (
    u.protocol === 'http:' &&
    ['127.0.0.1', 'localhost', '[::1]'].includes(u.hostname) &&
    !u.username &&
    !u.password &&
    !u.search &&
    !u.hash &&
    url.endsWith('/')
  );
}

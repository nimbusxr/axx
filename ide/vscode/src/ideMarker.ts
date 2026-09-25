// SPDX-License-Identifier: Apache-2.0
// Parses the debugger requests axx prints for IDEs, one per line:
//   [AXX-IDE] debug-attach-request name=axx-steps type=go host=127.0.0.1 port=2345
// The rules match the IntelliJ plugin's: the marker may follow other text, ANSI colors are
// ignored, fields come in any order, unknown keys and tokens without "=" are skipped, the first
// value of a repeated key wins, and name, type, host and port (1-65535) are required.

export interface DebugRequest {
  kind: 'attach' | 'listener';
  name: string;
  type: string;
  host: string;
  port: number;
}

const MARKER = /\[AXX-IDE\]\s+debug-(attach|listener)-request(?=\s|$)(.*)$/;

export function parseDebugRequest(line: string): DebugRequest | undefined {
  const m = MARKER.exec(line.replace(/\x1b\[[0-9;]*[A-Za-z]/g, ''));
  if (!m) return undefined;
  const fields = new Map<string, string>();
  for (const token of m[2].trim().split(/\s+/)) {
    const eq = token.indexOf('=');
    if (eq <= 0) continue;
    const key = token.slice(0, eq);
    if (!fields.has(key)) fields.set(key, token.slice(eq + 1));
  }
  const name = fields.get('name');
  const type = fields.get('type');
  const host = fields.get('host');
  const port = Number(fields.get('port'));
  if (!name || !type || !host || !Number.isInteger(port) || port < 1 || port > 65535) return undefined;
  return { kind: m[1] as DebugRequest['kind'], name, type, host, port };
}

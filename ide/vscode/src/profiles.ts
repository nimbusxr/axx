// SPDX-License-Identifier: Apache-2.0
// The profiles of an axx project, as `--profile` takes them: the keys of `profiles` in its config
// file, in their order, then the names of the `axx.<name>.yaml` files next to it (`axx.local.yaml`
// always applies, so it is none). The IntelliJ plugin finds them the same way (AxxProfiles.java).
import * as fs from 'node:fs';
import * as path from 'node:path';

const CONFIG_NAMES = ['axx.yaml', 'axx.yml'];
const BLOCK = /^profiles\s*:\s*(#.*)?$/;
const FLOW = /^profiles\s*:\s*\{(.*)\}\s*(#.*)?$/;
const FILE = /^axx\.(.+)\.yaml$/;

// The profiles of the axx project whose axx.yaml is in `dir`, without duplicates.
export function findProfiles(dir: string): string[] {
  const out = new Set<string>();
  for (const name of CONFIG_NAMES) {
    const config = path.join(dir, name);
    if (isFile(config)) {
      try {
        for (const key of profileKeys(fs.readFileSync(config, 'utf8').split(/\r?\n/))) out.add(key);
      } catch {
        // An unreadable config has no profiles to offer; axx run says what is wrong.
      }
      break;
    }
  }
  let entries: string[] = [];
  try {
    entries = fs.readdirSync(dir);
  } catch {
    // No directory, no profile files.
  }
  const files = entries
    .map((e) => FILE.exec(e)?.[1])
    .filter((name): name is string => name !== undefined && name !== 'local' && isFile(path.join(dir, `axx.${name}.yaml`)))
    .sort();
  for (const name of files) out.add(name);
  return [...out];
}

// The keys of the top-level `profiles` mapping of a config file's lines.
export function profileKeys(lines: string[]): string[] {
  let i = 0;
  for (; i < lines.length; i++) {
    const flow = FLOW.exec(lines[i]);
    if (flow) return flowKeys(flow[1]);
    if (BLOCK.test(lines[i])) break;
  }
  const keys: string[] = [];
  let indent = -1;
  for (i++; i < lines.length; i++) {
    const line = lines[i];
    const text = line.trim();
    if (text === '' || text.startsWith('#')) continue;
    const at = line.length - line.trimStart().length;
    if (at === 0) break; // the next top-level key
    if (indent < 0) indent = at;
    if (at < indent) break;
    if (at === indent && !text.startsWith('-')) {
      const k = key(text);
      if (k !== '') keys.push(k);
    }
  }
  return keys;
}

// The keys of a one-line flow mapping's content: `a: {..}, b: x`.
function flowKeys(content: string): string[] {
  const keys: string[] = [];
  let depth = 0;
  let entry = '';
  for (const c of content + ',') {
    if (c === '{' || c === '[') depth++;
    else if (c === '}' || c === ']') depth--;
    else if (c === ',' && depth === 0) {
      const k = key(entry.trim());
      if (k !== '') keys.push(k);
      entry = '';
      continue;
    }
    entry += c;
  }
  return keys;
}

// The key of a `key: value` entry, unquoted; '' when it has none.
function key(entry: string): string {
  if (entry.startsWith('"') || entry.startsWith("'")) {
    const end = entry.indexOf(entry[0], 1);
    return end > 0 ? entry.slice(1, end) : '';
  }
  const colon = entry.indexOf(':');
  return colon > 0 ? entry.slice(0, colon).trim() : '';
}

// The chosen profiles a project has, in their order, and the ones it lacks.
export function profilesFor(chosen: string[], available: string[]): { apply: string[]; missing: string[] } {
  return {
    apply: chosen.filter((p) => available.includes(p)),
    missing: chosen.filter((p) => !available.includes(p)),
  };
}

function isFile(p: string): boolean {
  try {
    return fs.statSync(p).isFile();
  } catch {
    return false;
  }
}

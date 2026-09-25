// SPDX-License-Identifier: Apache-2.0
// Finds the runnable parts of a feature file: the feature, its rules, scenarios (outlines
// included) and the rows of their Examples tables, with their lines. It is not a full Gherkin
// parser: `axx run` and `axx lsp` report syntax errors. English keywords only.

export type NodeKind = 'feature' | 'rule' | 'scenario' | 'example';

export interface GherkinNode {
  kind: NodeKind;
  keyword: string; // as written, e.g. "Scenario Outline"; empty for an example row
  name: string; // for an example row, its cells joined with " | "
  line: number; // 1-based
  endLine: number; // 1-based, inclusive; trailing blank and comment lines are left out
  examples?: string; // an example row's Examples name
  children: GherkinNode[];
}

const SECTION =
  /^\s*(Feature|Business Need|Ability|Rule|Background|Scenario Outline|Scenario Template|Scenario|Example|Examples|Scenarios):\s*(.*?)\s*$/;

const KINDS: Record<string, NodeKind | 'background' | 'examples'> = {
  Feature: 'feature',
  'Business Need': 'feature',
  Ability: 'feature',
  Rule: 'rule',
  Background: 'background',
  'Scenario Outline': 'scenario',
  'Scenario Template': 'scenario',
  Scenario: 'scenario',
  Example: 'scenario',
  Examples: 'examples',
  Scenarios: 'examples',
};

// Where a block starts (its first tag line, or its keyword line), for ending the one before.
interface Marker {
  kind: NodeKind | 'background';
  start: number;
  node?: GherkinNode;
}

// Returns the feature in `text`, or undefined when it has no Feature line.
export function parseFeature(text: string): GherkinNode | undefined {
  const lines = text.split(/\r?\n/);
  const markers: Marker[] = [];
  let feature: GherkinNode | undefined;
  let rule: GherkinNode | undefined;
  let scenario: GherkinNode | undefined;
  let examples: { name: string; header: boolean } | undefined;
  let docString: string | undefined; // the open doc string's delimiter
  let tagsStart: number | undefined;

  for (let i = 0; i < lines.length; i++) {
    const lineNo = i + 1;
    const trimmed = lines[i].trim();
    if (docString !== undefined) {
      if (trimmed === docString) docString = undefined;
      continue;
    }
    if (trimmed.startsWith('"""') || trimmed.startsWith('```')) {
      docString = trimmed.slice(0, 3);
      continue;
    }
    if (trimmed === '' || trimmed.startsWith('#')) continue;
    if (trimmed.startsWith('@')) {
      tagsStart ??= lineNo;
      continue;
    }
    if (trimmed.startsWith('|')) {
      if (scenario && examples) {
        if (!examples.header) {
          examples.header = true;
        } else {
          scenario.children.push({
            kind: 'example',
            keyword: '',
            name: tableCells(trimmed).join(' | '),
            line: lineNo,
            endLine: lineNo,
            examples: examples.name || undefined,
            children: [],
          });
        }
      }
      continue;
    }
    const m = SECTION.exec(lines[i]);
    const start = tagsStart ?? lineNo;
    tagsStart = undefined;
    if (!m) continue; // a step or a description line
    const [, keyword, name] = m;
    const kind = KINDS[keyword];
    if (kind === 'examples') {
      if (scenario) examples = { name, header: false };
      continue;
    }
    examples = undefined;
    if (kind === 'feature') {
      if (feature) continue; // one feature per file
      feature = { kind, keyword, name, line: lineNo, endLine: lines.length, children: [] };
      markers.push({ kind, start, node: feature });
      continue;
    }
    if (!feature) continue;
    if (kind === 'background') {
      scenario = undefined;
      markers.push({ kind, start });
      continue;
    }
    const node: GherkinNode = { kind, keyword, name, line: lineNo, endLine: lines.length, children: [] };
    if (kind === 'rule') {
      feature.children.push(node);
      rule = node;
      scenario = undefined;
    } else {
      (rule ?? feature).children.push(node);
      scenario = node;
    }
    markers.push({ kind, start, node });
  }

  if (!feature) return undefined;
  // A block ends where the next block of its level or above starts.
  const ends: Record<NodeKind | 'background', ReadonlySet<string>> = {
    feature: new Set(),
    rule: new Set(['rule']),
    scenario: new Set(['scenario', 'background', 'rule']),
    background: new Set(),
    example: new Set(),
  };
  markers.forEach((marker, k) => {
    const node = marker.node;
    if (!node) return;
    const next = markers.slice(k + 1).find((m) => ends[marker.kind].has(m.kind));
    let end = next ? next.start - 1 : lines.length;
    while (end > node.line && isBlankOrComment(lines[end - 1])) end--;
    node.endLine = end;
  });
  return feature;
}

function isBlankOrComment(line: string): boolean {
  const t = line.trim();
  return t === '' || t.startsWith('#');
}

// The cells of a table row (Gherkin escapes: \| \\ \n). Text after the last pipe is ignored.
export function tableCells(row: string): string[] {
  const s = row.trim();
  const cells: string[] = [];
  let cell = '';
  for (let i = 1; i < s.length; i++) {
    const c = s[i];
    if (c === '\\' && i + 1 < s.length) {
      const n = s[++i];
      cell += n === 'n' ? '\n' : n === '|' || n === '\\' ? n : `\\${n}`;
    } else if (c === '|') {
      cells.push(cell.trim());
      cell = '';
    } else {
      cell += c;
    }
  }
  return cells;
}

// The coverage a run with coverage (axx run --coverage) measured, from the lcov report axx
// announces when it ends ("[AXX-IDE] coverage path=..."): what the editor shows of each file.

// A file's coverage: each line's hits, each function's, and each line's branches' hits.
export interface LcovFile {
  path: string;
  lines: Map<number, number>; // line (1-based) → hits
  functions: { name: string; line: number; hits: number }[];
  branches: Map<number, number[]>; // line → each branch's hits
}

// parseLcov reads an lcov tracefile, as axx writes it: SF, FN/FNDA, DA, BRDA records, a file a
// record. A file listed twice adds up.
export function parseLcov(text: string): LcovFile[] {
  const files = new Map<string, LcovFile>();
  let current: LcovFile | undefined;
  const fnHits = new Map<string, number[]>();
  const end = (): void => {
    if (!current) return;
    for (const fn of current.functions) {
      const hits = fnHits.get(fn.name);
      if (hits && hits.length > 0) fn.hits = hits.shift()!;
    }
    const prev = files.get(current.path);
    if (prev) {
      for (const [line, hits] of current.lines) prev.lines.set(line, (prev.lines.get(line) ?? 0) + hits);
      prev.functions.push(...current.functions);
      for (const [line, hits] of current.branches) prev.branches.set(line, [...(prev.branches.get(line) ?? []), ...hits]);
    } else {
      files.set(current.path, current);
    }
    current = undefined;
    fnHits.clear();
  };
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    const colon = line.indexOf(':');
    const key = colon < 0 ? line : line.slice(0, colon);
    const fields = colon < 0 ? [] : line.slice(colon + 1).split(',');
    switch (key) {
      case 'SF':
        end();
        current = { path: line.slice(3), lines: new Map(), functions: [], branches: new Map() };
        break;
      case 'end_of_record':
        end();
        break;
      case 'DA':
        if (current && fields.length >= 2) {
          const l = Number(fields[0]);
          current.lines.set(l, (current.lines.get(l) ?? 0) + Number(fields[1]));
        }
        break;
      case 'FN':
        // FN:line,name, or lcov 2's FN:line,end line,name.
        if (current && fields.length >= 2) {
          const named = fields.length > 2 && /^\d+$/.test(fields[1]) ? fields.slice(2) : fields.slice(1);
          current.functions.push({ name: named.join(','), line: Number(fields[0]), hits: 0 });
        }
        break;
      case 'FNDA':
        if (fields.length >= 2) {
          const name = fields.slice(1).join(',');
          fnHits.set(name, [...(fnHits.get(name) ?? []), Number(fields[0])]);
        }
        break;
      case 'BRDA':
        if (current && fields.length === 4) {
          const l = Number(fields[0]);
          const taken = fields[3] === '-' ? 0 : Number(fields[3]);
          current.branches.set(l, [...(current.branches.get(l) ?? []), taken]);
        }
        break;
    }
  }
  end();
  return [...files.values()];
}

// covered counts what ran of a file's lines, branches and functions, of how many.
export function covered(file: LcovFile): {
  lines: [number, number];
  branches: [number, number];
  functions: [number, number];
} {
  const count = (hits: number[]): [number, number] => [hits.filter((h) => h > 0).length, hits.length];
  return {
    lines: count([...file.lines.values()]),
    branches: count([...file.branches.values()].flat()),
    functions: count(file.functions.map((f) => f.hits)),
  };
}

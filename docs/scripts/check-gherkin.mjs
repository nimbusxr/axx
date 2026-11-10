#!/usr/bin/env node
// Executable docs: every ```gherkin block in src/content/docs, the skills
// (internal/skills/assets), the README and AGENTS.md must be real Gherkin
// made of real steps. Each block is written to a temporary .feature
// file and checked with `axx validate --json`.
//
//   node scripts/check-gherkin.mjs
//
// Blocks fenced as ```gherkin nocheck are skipped (use it for deliberately
// broken examples and for custom steps that only exist in an example axx.yaml).
// A block without a `Feature:` line is wrapped in one (and in a `Scenario:`
// when it has no scenario of its own), so pages can show a few steps.
//
// Failures (exit 1): Gherkin syntax errors, undefined steps (a typo, or
// invented step text), ambiguous steps and step argument problems (a missing
// data table, say); and a scenario, or a block of a few steps, with a second
// When or a second Then, in any block, generated pages' and nocheck ones too. A
// scenario is one behavior, And adds to its When or its Then, and a page that
// lists a step's forms gives each a block of its own. And a block that is not
// laid out as `axx fmt` lays out a feature (a snippet as it would sit in one,
// at the indent it starts at: an excerpt may keep its file's), but those
// fenced as ```gherkin unformatted, which show a layout on purpose: `--fix`
// lays them out in place.
//
//   node scripts/check-gherkin.mjs --fix
//
// Uses ../bin/axx-all, axx with every pack (built by `npm run gen`), unless
// AXX_BIN is set.

import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { baseIndent, blocks, fromLaidOut, markdownFiles, toFeature, toLaidOut } from './gherkin-blocks.mjs';

const docsDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const repoDir = path.resolve(docsDir, '..');
const contentDir = path.join(docsDir, 'src/content/docs');
const axxBin = path.resolve(process.env.AXX_BIN ?? path.join(repoDir, 'bin', process.platform === 'win32' ? 'axx-all.exe' : 'axx-all'));

if (!fs.existsSync(axxBin)) {
	console.error(`check-gherkin: ${axxBin} not found; run \`npm run gen\` first or set AXX_BIN`);
	process.exit(2);
}

// ------------------------------------------------------------- the blocks

function validate(dir, name) {
	try {
		return JSON.parse(execFileSync(axxBin, ['validate', '--json', name], { cwd: dir, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }));
	} catch (err) {
		if (err.stdout) return JSON.parse(err.stdout);
		throw err;
	}
}

// The line of each second When and second Then, by scenario, outside doc strings.
function secondClauses(body) {
	const found = [];
	let seen = new Set();
	let quote = null;
	body.forEach((line, i) => {
		const text = line.trim();
		const mark = text.match(/^("""|```)/)?.[1];
		if (mark && (quote === null || quote === mark)) {
			quote = quote === null ? mark : null;
			return;
		}
		if (quote !== null) return;
		if (/^(Scenario Outline|Scenario Template|Scenario|Example|Background|Rule|Feature):/.test(text)) {
			seen = new Set();
			return;
		}
		const keyword = text.match(/^(When|Then)\s/)?.[1];
		if (!keyword) return;
		if (seen.has(keyword)) found.push({ offset: i, keyword });
		seen.add(keyword);
	});
	return found;
}

// ------------------------------------------------------------- run

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'axx-docs-gherkin-'));
// The snippets may use any pack: the project lists them all.
const packs = JSON.parse(execFileSync(axxBin, ['pack', 'list', '--json'], { cwd: tmp, encoding: 'utf8' })).data.packs.map((p) => p.pack);
fs.writeFileSync(path.join(tmp, 'axx-packs.yaml'), `packs:\n${packs.map((p) => `  - ${p}\n`).join('')}`);
const fix = process.argv.includes('--fix');
const counts = { blocks: 0, skipped: 0, syntax: 0, undefined: 0, argument: 0, ambiguous: 0, clauses: 0, layout: 0 };
// Each checked block, laid out in a folder of its own: axx fmt lays them all out at once.
const layoutDir = path.join(tmp, 'layout');
fs.mkdirSync(layoutDir);
const laidOut = [];
const failures = [];
let n = 0;

// The site, plus every other place an agent or a human copies steps from:
// the skills shipped in the binary, the README and AGENTS.md.
const sources = [
	...markdownFiles(contentDir),
	...markdownFiles(path.join(repoDir, 'internal/skills/assets')),
	...['README.md', 'AGENTS.md']
		.map((f) => path.join(repoDir, f))
		.filter((f) => fs.existsSync(f)),
];

for (const file of sources.sort()) {
	const rel = path.relative(docsDir, file);
	for (const block of blocks(file)) {
		if (block.nocheck) {
			counts.skipped++;
			continue;
		}
		counts.blocks++;
		const { text, offset } = toFeature(block.body);
		const name = `block-${++n}.feature`;
		fs.writeFileSync(path.join(tmp, name), text + '\n');
		if (!block.unformatted) {
			const wrap = toLaidOut(block.body);
			fs.writeFileSync(path.join(layoutDir, name), wrap.text + '\n');
			laidOut.push({ file, rel, block, name, wrap });
		}
		const out = validate(tmp, name);
		const at = (loc) => {
			const line = Number(String(loc ?? '').split(':').pop());
			return `${rel}:${Number.isFinite(line) && line > offset ? block.line + line - 1 - offset : block.line}`;
		};
		for (const e of out.errors ?? []) {
			counts.syntax++;
			const detail = e.message.split('\n').slice(-1)[0].trim();
			const pos = detail.match(/\((\d+):\d+\)/);
			const where = pos ? at(`x:${pos[1]}`) : `${rel}:${block.line}`;
			failures.push(`${where}: ${e.code}: ${detail.replace(/^.*?\(\d+:\d+\):\s*/, '')}`);
		}
		for (const p of out.data?.problems ?? []) {
			if (p.kind === 'undefined') {
				counts.undefined++;
				const hint = p.suggestions?.[0] ? ` (did you mean: ${p.suggestions[0].expr})` : '';
				failures.push(`${at(p.location)}: undefined step: ${p.text}${hint}`);
			} else if (p.kind === 'ambiguous') {
				counts.ambiguous++;
				failures.push(`${at(p.location)}: ambiguous: ${p.text}`);
			} else {
				counts.argument++;
				failures.push(`${at(p.location)}: ${p.kind}: ${p.text}${p.message ? ` (${p.message})` : ''}`);
			}
		}
	}
}

// The layout: axx fmt lays out every block's file; a block whose file it
// changed is not laid out. A block that does not parse is left as it is,
// and reported above.
try {
	execFileSync(axxBin, ['fmt', layoutDir], { cwd: layoutDir, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
} catch (err) {
	for (const l of String(err.stderr ?? '').split('\n')) {
		if (l.includes('AXX-E0208')) failures.push(`axx fmt: ${l.trim()}`);
	}
}
const trimEnd = (lines) => {
	const out = [...lines];
	while (out.length && out[out.length - 1].trim() === '') out.pop();
	return out;
};
const fixes = new Map();
for (const { file, rel, block, name, wrap } of laidOut) {
	const want = trimEnd(fromLaidOut(fs.readFileSync(path.join(layoutDir, name), 'utf8'), wrap, baseIndent(block.body)));
	const have = trimEnd(block.body);
	const at = want.findIndex((l, i) => l !== have[i]);
	const k = at >= 0 ? at : want.length < have.length ? want.length : -1;
	if (k < 0) continue;
	counts.layout++;
	if (fix) {
		if (!fixes.has(file)) fixes.set(file, []);
		fixes.get(file).push({ block, want });
		continue;
	}
	const show = (l) => (l === undefined ? '(no line)' : JSON.stringify(l));
	failures.push(`${rel}:${block.line + k}: not laid out as axx fmt lays it out: ${show(have[k])} should be ${show(want[k])} (npm run check:gherkin -- --fix lays it out)`);
}
// --fix: each block's lines replaced by its layout, last block first, in the fence's indent.
for (const [file, list] of fixes) {
	const lines = fs.readFileSync(file, 'utf8').split('\n');
	for (const { block, want } of list.sort((a, b) => b.block.line - a.block.line)) {
		const start = block.line - 1;
		const count = trimEnd(block.body).length;
		lines.splice(start, count, ...want.map((l) => (l === '' ? l : block.indent + l)));
	}
	fs.writeFileSync(file, lines.join('\n'));
	console.log(`laid out ${list.length} block(s) in ${path.relative(docsDir, file)}`);
}
fs.rmSync(tmp, { recursive: true, force: true });

for (const file of [...new Set([...sources, ...markdownFiles(contentDir, { withGenerated: true })])].sort()) {
	const rel = path.relative(docsDir, file);
	for (const block of blocks(file)) {
		for (const { offset, keyword } of secondClauses(block.body)) {
			counts.clauses++;
			failures.push(`${rel}:${block.line + offset}: a second ${keyword}: a scenario is one behavior, so use And, split the scenario, or give each form of a step a block of its own`);
		}
	}
}

if (failures.length) {
	console.log('Failures:');
	for (const f of failures) console.log(`  ${f}`);
	console.log('');
}
console.log(
	`check-gherkin: ${counts.blocks} blocks checked, ${counts.skipped} skipped (nocheck); ` +
		`${counts.syntax} syntax errors, ${counts.undefined} undefined, ${counts.ambiguous} ambiguous, ${counts.argument} argument problems, ` +
		`${counts.clauses} second clauses, ${counts.layout} not laid out${fix ? ' (laid out now)' : ''}`,
);
process.exit(failures.length ? 1 : 0);

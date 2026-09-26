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
// data table, say).
//
// Uses ../bin/axx-all, axx with every pack (built by `npm run gen`), unless
// AXX_BIN is set.

import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { blocks, markdownFiles, toFeature } from './gherkin-blocks.mjs';

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

// ------------------------------------------------------------- run

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'axx-docs-gherkin-'));
// The snippets may use any pack: the project lists them all.
const packs = JSON.parse(execFileSync(axxBin, ['pack', 'list', '--json'], { cwd: tmp, encoding: 'utf8' })).data.packs.map((p) => p.pack);
fs.writeFileSync(path.join(tmp, 'axx-packs.yaml'), `packs:\n${packs.map((p) => `  - ${p}\n`).join('')}`);
const counts = { blocks: 0, skipped: 0, syntax: 0, undefined: 0, argument: 0, ambiguous: 0 };
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
fs.rmSync(tmp, { recursive: true, force: true });

if (failures.length) {
	console.log('Failures:');
	for (const f of failures) console.log(`  ${f}`);
	console.log('');
}
console.log(
	`check-gherkin: ${counts.blocks} blocks checked, ${counts.skipped} skipped (nocheck); ` +
		`${counts.syntax} syntax errors, ${counts.undefined} undefined, ${counts.ambiguous} ambiguous, ${counts.argument} argument problems`,
);
process.exit(failures.length ? 1 : 0);

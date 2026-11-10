// The Gherkin code blocks of the docs, as check-gherkin.mjs checks them and
// step-tokens.mjs colors them.

import fs from 'node:fs';
import path from 'node:path';

/** The Markdown files under dir; generated pages (_gen) too when withGenerated. */
export function markdownFiles(dir, { withGenerated = false } = {}) {
	return fs.readdirSync(dir, { withFileTypes: true }).flatMap((d) => {
		const p = path.join(dir, d.name);
		if (d.isDirectory()) return d.name === '_gen' && !withGenerated ? [] : markdownFiles(p, { withGenerated });
		return /\.mdx?$/.test(d.name) ? [p] : [];
	});
}

/**
 * The gherkin blocks of a Markdown file: the line their code starts on, their
 * lines, nocheck, unformatted (a block that shows a layout `axx fmt` changes,
 * on purpose), and the fence's indent.
 */
export function blocks(file) {
	const lines = fs.readFileSync(file, 'utf8').split('\n');
	const found = [];
	for (let i = 0; i < lines.length; i++) {
		const open = lines[i].match(/^(\s*)(`{3,}|~{3,})gherkin\b(.*)$/);
		if (!open) continue;
		const [, indent, fence, info] = open;
		const body = [];
		let j = i + 1;
		for (; j < lines.length; j++) {
			const close = lines[j].match(/^(\s*)(`{3,}|~{3,})\s*$/);
			if (close && close[2][0] === fence[0] && close[2].length >= fence.length) break;
			body.push(lines[j].startsWith(indent) ? lines[j].slice(indent.length) : lines[j].trimStart());
		}
		found.push({ line: i + 2, nocheck: /\bnocheck\b/.test(info), unformatted: /\bunformatted\b/.test(info), body, indent });
		i = j;
	}
	return found;
}

/**
 * Wraps a snippet so it is a complete feature: the text, the lines added
 * before the snippet's, and the columns added before each of its lines.
 */
export function toFeature(body) {
	const text = body.join('\n');
	if (/^\s*Feature:/m.test(text)) return { text, offset: 0, indent: 0 };
	if (/^\s*(Background|Scenario|Scenario Outline|Scenario Template|Example|Rule):/m.test(text)) {
		return { text: `Feature: docs snippet\n${text}`, offset: 1, indent: 0 };
	}
	return { text: `Feature: docs snippet\n  Scenario: docs snippet\n${body.map((l) => `    ${l}`).join('\n')}`, offset: 2, indent: 4 };
}

/**
 * Wraps a snippet so that it sits where it would in a feature, as layout
 * checks it: the text, and how to take the snippet back out of the text
 * `axx fmt` gives (the lines before it, and the columns it is in by).
 */
export function toLaidOut(body) {
	const text = body.join('\n');
	const shift = (n) => body.map((l) => (l === '' ? l : ' '.repeat(n) + l)).join('\n');
	if (/^\s*Feature:/m.test(text)) return { text, header: 0, indent: 0 };
	if (/^\s*(Background|Scenario|Scenario Outline|Scenario Template|Example|Rule):/m.test(text)) {
		return { text: `Feature: docs snippet\n${shift(2)}`, header: 1, indent: 2 };
	}
	return { text: `Feature: docs snippet\n  Scenario: docs snippet\n${shift(4)}`, header: 2, indent: 4 };
}

/**
 * The snippet in out, a toLaidOut text as `axx fmt` laid it out: its lines,
 * base columns in (a snippet keeps the indent it starts at: an excerpt shows
 * its lines where they sit in their file).
 */
export function fromLaidOut(out, { header, indent }, base = 0) {
	let lines = out.replace(/\n$/, '').split('\n');
	// The wrapper's lines, and the blank lines axx fmt puts after them.
	let dropped = 0;
	while (dropped < header && lines.length) {
		if (lines[0].trim() !== '') dropped++;
		lines.shift();
	}
	while (lines.length && lines[0].trim() === '') lines.shift();
	const pad = ' '.repeat(indent);
	const lead = ' '.repeat(base);
	return lines.map((l) => (l.startsWith(pad) ? l.slice(indent) : l.trimStart())).map((l) => (l === '' ? l : lead + l));
}

/** The columns a snippet starts at: the least indent of its lines that have text. */
export function baseIndent(body) {
	const ins = body.filter((l) => l.trim() !== '').map((l) => l.length - l.trimStart().length);
	return ins.length ? Math.min(...ins) : 0;
}

/** How a block's code is known again when the site renders it: its lines, without trailing space. */
export function blockKey(code) {
	return code
		.split('\n')
		.map((l) => l.trimEnd())
		.join('\n')
		.trimEnd();
}

/** The inline code of a Markdown file, outside its code blocks: what may be a step written in a sentence. */
export function inlineCode(file) {
	const text = fs.readFileSync(file, 'utf8').replace(/^(\s*)(`{3,}|~{3,})[\s\S]*?^\1\2\s*$/gm, '');
	return [...text.matchAll(/(?<!`)`([^`\n]+)`(?!`)/g)].map((m) => m[1]);
}

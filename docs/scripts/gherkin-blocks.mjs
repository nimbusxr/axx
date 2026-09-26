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

/** The gherkin blocks of a Markdown file: the line their code starts on, their lines, and nocheck. */
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
		found.push({ line: i + 2, nocheck: /\bnocheck\b/.test(info), body });
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

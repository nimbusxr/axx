import fs from 'node:fs';
import { InlineStyleAnnotation } from '@astrojs/starlight/expressive-code';
import { blockKey } from '../../scripts/gherkin-blocks.mjs';

/**
 * Colors the data of the steps in Gherkin code blocks as editors do, apart
 * from the words a step always has:
 *
 * - its parameter values (`5s`, `503`, `2nd`, `"Pickup"`, `seeds/parcels.yaml`)
 *   and the <placeholders> of Scenario Outlines, where axx's language server
 *   says they are, found when the docs are generated (scripts/step-tokens.mjs);
 * - its data table: the names in its first column (a property, a column)
 *   and the header of an Examples table, then the values, with the pipes
 *   between them dimmed.
 *
 * colors are each theme's colors, in the themes' order: { value, name,
 * punctuation }.
 */
export function stepTokensPlugin(tokensFile, colors) {
	const tokens = fs.existsSync(tokensFile) ? (JSON.parse(fs.readFileSync(tokensFile, 'utf8')).blocks ?? {}) : {};
	return {
		name: 'axx-step-tokens',
		hooks: {
			postprocessAnalyzedCode: ({ codeBlock }) => {
				if (codeBlock.language !== 'gherkin') return;
				const paint = (codeLine, from, to, kind) => {
					if (to <= from || to > codeLine.text.length) return;
					colors.forEach((c, styleVariantIndex) => {
						codeLine.addAnnotation(
							new InlineStyleAnnotation({ inlineRange: { columnStart: from, columnEnd: to }, color: c[kind], styleVariantIndex, renderPhase: 'later' }),
						);
					});
				};
				for (const [line, column, length] of tokens[blockKey(codeBlock.code)] ?? []) {
					const codeLine = codeBlock.getLine(line);
					if (codeLine) paint(codeLine, column, column + length, 'value');
				}
				// Data tables: the header of an Examples table, and the first
				// column of a step's table of two columns or more, are names.
				let header = false;
				for (const codeLine of codeBlock.getLines()) {
					const text = codeLine.text;
					if (/^\s*(Examples|Scenarios):/.test(text)) {
						header = true;
						continue;
					}
					if (!/^\s*\|/.test(text)) {
						if (text.trim()) header = false;
						continue;
					}
					const cells = cellsOf(text);
					cells.forEach(([from, to], i) => {
						const kind = header || (i === 0 && cells.length > 1) ? 'name' : 'value';
						paint(codeLine, from, to, kind);
					});
					for (const at of pipesOf(text)) paint(codeLine, at, at + 1, 'punctuation');
					header = false;
				}
			},
		},
	};
}

/** The pipes of a table row that separate its cells (not those escaped, `\|`). */
function pipesOf(row) {
	const out = [];
	for (let i = 0; i < row.length; i++) {
		if (row[i] === '\\') i++;
		else if (row[i] === '|') out.push(i);
	}
	return out;
}

/** The cells of a table row: [from, to] each, spaces around them left out. */
function cellsOf(row) {
	const pipes = pipesOf(row);
	const out = [];
	for (let i = 0; i + 1 < pipes.length; i++) {
		let from = pipes[i] + 1;
		let to = pipes[i + 1];
		while (from < to && row[from] === ' ') from++;
		while (to > from && row[to - 1] === ' ') to--;
		if (to > from) out.push([from, to]);
	}
	return out;
}

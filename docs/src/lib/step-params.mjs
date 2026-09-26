import fs from 'node:fs';
import { stepParam } from './axx-gherkin.mjs';

/**
 * Colors steps written in inline code as code blocks color them: the
 * parameter values of a step (`the "Express" option is chosen`), where
 * axx's language server says they are (scripts/step-tokens.mjs), and the
 * parameter types of a step expression (`the {string} page is opened`).
 * Code blocks are left to Expressive Code (step-tokens-plugin.mjs).
 */
export function stepParams(tokensFile) {
	const inline = fs.existsSync(tokensFile) ? (JSON.parse(fs.readFileSync(tokensFile, 'utf8')).inline ?? {}) : {};
	return {
		name: 'axx-step-params',
		inlineCode(node) {
			const ranges = [...(inline[node.value] ?? []).map(([at, length]) => [at, at + length])];
			for (const m of node.value.matchAll(stepParam)) ranges.push([m.index, m.index + m[0].length]);
			if (!ranges.length) return undefined;
			ranges.sort((a, b) => a[0] - b[0]);
			let html = '';
			let last = 0;
			for (const [from, to] of ranges) {
				if (from < last) continue;
				html += literal(node.value.slice(last, from)) + `<span class="step-param">${literal(node.value.slice(from, to))}</span>`;
				last = to;
			}
			html += literal(node.value.slice(last));
			return { type: 'html', value: `<code>${html}</code>` };
		},
	};
}

// The code goes into the page as inline HTML: every character but letters,
// digits and spaces is written as an entity, so none is HTML.
function literal(text) {
	return text.replace(/[^A-Za-z0-9 ]/g, (c) => `&#${c.codePointAt(0)};`);
}

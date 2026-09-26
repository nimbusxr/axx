// Which words of the docs' Gherkin blocks are a step's parameter values
// (`5s`, `503`, `2nd`, `"Pickup"`, `seeds/parcels.yaml`) and a Scenario
// Outline's <placeholders>, as axx itself reads the steps: its language
// server's semantic tokens, the ones editors color. The site colors them
// the same (src/lib/step-tokens-plugin.mjs).

import { spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { blockKey, blocks, inlineCode, toFeature } from './gherkin-blocks.mjs';

/**
 * The tokens of the gherkin blocks of files, by block (blockKey of its
 * code): [line, column, length] a token, lines and columns of the block's
 * own code; and of the steps written in their inline code, by the code:
 * [column, length] a token. packs are the packs a project of every block
 * has.
 */
export async function stepTokens(axxBin, files, packs) {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'axx-docs-tokens-'));
	fs.writeFileSync(path.join(dir, 'axx-packs.yaml'), `packs:\n${packs.map((p) => `  - ${p}\n`).join('')}`);
	const lsp = languageServer(axxBin, dir);
	const out = { blocks: {}, inline: {} };
	try {
		await lsp.request('initialize', { processId: process.pid, rootUri: pathToFileURL(dir).href, capabilities: {} });
		lsp.notify('initialized', {});
		let n = 0;
		for (const file of files) {
			for (const block of blocks(file)) {
				const key = blockKey(block.body.join('\n'));
				if (key in out.blocks) continue;
				const { text, offset, indent } = toFeature(block.body);
				const uri = pathToFileURL(path.join(dir, `block-${++n}.feature`)).href;
				lsp.notify('textDocument/didOpen', { textDocument: { uri, languageId: 'gherkin', version: 1, text } });
				const tokens = [];
				for (const [line, col, length] of await semanticTokens(lsp, uri)) {
					if (line >= offset && col >= indent) tokens.push([line - offset, col - indent, length]);
				}
				if (tokens.length) out.blocks[key] = tokens;
			}
		}
		// Inline code that is a step, written in a sentence: the spans, a
		// step a line of one scenario, each after a keyword of its own.
		const spans = [...new Set(files.flatMap(inlineCode))].filter((s) => /\s\S+\s/.test(s) && !/^\s*(Feature|Scenario|Background|Rule|Examples)\b/.test(s));
		const keyword = /^(Given|When|Then|And|But|\*) /;
		const lines = spans.map((s) => (keyword.test(s) ? s : `* ${s}`));
		const uri = pathToFileURL(path.join(dir, 'inline.feature')).href;
		const text = `Feature: docs inline\n  Scenario: docs inline\n${lines.map((l) => `    ${l}`).join('\n')}\n`;
		lsp.notify('textDocument/didOpen', { textDocument: { uri, languageId: 'gherkin', version: 1, text } });
		for (const [line, col, length] of await semanticTokens(lsp, uri)) {
			const i = line - 2;
			if (i < 0 || i >= spans.length) continue;
			const shift = 4 + (keyword.test(spans[i]) ? 0 : 2);
			(out.inline[spans[i]] ??= []).push([col - shift, length]);
		}
		await lsp.request('shutdown', null);
		lsp.notify('exit', null);
	} finally {
		lsp.close();
		fs.rmSync(dir, { recursive: true, force: true });
	}
	return out;
}

/** The semantic tokens of an open document: [line, column, length] a token. */
async function semanticTokens(lsp, uri) {
	const res = await lsp.request('textDocument/semanticTokens/full', { textDocument: { uri } });
	lsp.notify('textDocument/didClose', { textDocument: { uri } });
	const out = [];
	let line = 0;
	let col = 0;
	const data = res?.data ?? [];
	for (let i = 0; i + 5 <= data.length; i += 5) {
		const [dLine, dCol, length] = data.slice(i, i + 3);
		col = dLine === 0 ? col + dCol : dCol;
		line += dLine;
		out.push([line, col, length]);
	}
	return out;
}

/** A JSON-RPC client of `axx lsp` on its stdin and stdout. */
function languageServer(axxBin, cwd) {
	const child = spawn(axxBin, ['lsp'], { cwd, stdio: ['pipe', 'pipe', 'inherit'] });
	const pending = new Map();
	let id = 0;
	let buf = Buffer.alloc(0);
	child.stdout.on('data', (chunk) => {
		buf = Buffer.concat([buf, chunk]);
		for (;;) {
			const end = buf.indexOf('\r\n\r\n');
			if (end < 0) return;
			const length = Number(/Content-Length: (\d+)/i.exec(buf.subarray(0, end).toString())?.[1]);
			if (buf.length < end + 4 + length) return;
			const msg = JSON.parse(buf.subarray(end + 4, end + 4 + length).toString());
			buf = buf.subarray(end + 4 + length);
			const waiting = msg.id !== undefined && pending.get(msg.id);
			if (!waiting) continue;
			pending.delete(msg.id);
			if (msg.error) waiting.reject(new Error(`axx lsp: ${msg.error.message}`));
			else waiting.resolve(msg.result);
		}
	});
	const send = (msg) => {
		const body = Buffer.from(JSON.stringify({ jsonrpc: '2.0', ...msg }));
		child.stdin.write(`Content-Length: ${body.length}\r\n\r\n`);
		child.stdin.write(body);
	};
	return {
		request: (method, params) =>
			new Promise((resolve, reject) => {
				pending.set(++id, { resolve, reject });
				send({ id, method, params });
			}),
		notify: (method, params) => send({ method, params }),
		close: () => child.kill(),
	};
}

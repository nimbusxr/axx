#!/usr/bin/env node
// Every page is in the sidebar: the guides, tutorials, explanations and
// references written by hand, and each pack's reference page
// (references/_gen/packs, which `npm run gen` writes). A page the sidebar
// leaves out is a page readers do not find.
//
//   node scripts/check-sidebar.mjs
//
// It reads the sidebar's pages from astro.config.mjs: its quoted slugs, and
// the packs of its packsOf([...]) lists. Run it after `npm run gen`.

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const docsDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const contentDir = path.join(docsDir, 'src/content/docs');

const config = fs.readFileSync(path.join(docsDir, 'astro.config.mjs'), 'utf8');
const listed = new Set();
for (const m of config.matchAll(/'((?:guides|tutorials|explanations|references)\/[a-z0-9/-]+)'/g)) {
	listed.add(m[1]);
}
for (const m of config.matchAll(/packsOf\(\[([^\]]*)\]\)/g)) {
	for (const pack of m[1].matchAll(/'([a-z0-9-]+)'/g)) {
		listed.add(`references/packs/${pack[1]}`);
	}
}

const pages = [];
for (const dir of ['guides', 'tutorials', 'explanations', 'references']) {
	for (const file of fs.readdirSync(path.join(contentDir, dir))) {
		if (/\.mdx?$/.test(file)) {
			pages.push(`${dir}/${file.replace(/\.mdx?$/, '')}`);
		}
	}
}
const packsDir = path.join(contentDir, 'references/_gen/packs');
if (!fs.existsSync(packsDir)) {
	console.error('check-sidebar: no pack pages in references/_gen/packs: run `npm run gen` first');
	process.exit(1);
}
for (const file of fs.readdirSync(packsDir)) {
	if (file.endsWith('.md') && file !== 'index.md') {
		pages.push(`references/packs/${file.replace(/\.md$/, '')}`);
	}
}

const missing = pages.filter((page) => !listed.has(page)).sort();
if (missing.length > 0) {
	console.error(`check-sidebar: ${missing.length} page(s) not in the sidebar (astro.config.mjs):`);
	for (const page of missing) {
		console.error(`  ${page}`);
	}
	process.exit(1);
}
console.log(`check-sidebar: ${pages.length} pages, all in the sidebar`);

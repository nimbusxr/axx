#!/usr/bin/env node
// Lists the pages of a built site that moved, as the KeyValueStore the
// CloudFront router reads (ADR 0005) keeps them: every page Astro wrote as a
// redirect (a meta refresh) is a key, its path, with the page it moved to as
// its value.
//   node scripts/redirects.mjs <site> > redirects.json
import fs from 'node:fs';
import path from 'node:path';

const site = process.argv[2];
if (!site) {
	console.error('usage: redirects.mjs <site>');
	process.exit(2);
}
const refresh = /<meta http-equiv="refresh" content="0;url=([^"]+)"/;
const moved = {};
function walk(dir) {
	for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
		const p = path.join(dir, e.name);
		if (e.isDirectory()) {
			walk(p);
		} else if (e.name === 'index.html') {
			const m = refresh.exec(fs.readFileSync(p, 'utf8').slice(0, 2048));
			if (m) {
				const rel = path.relative(site, dir).split(path.sep).join('/');
				moved[rel ? `/${rel}/` : '/'] = m[1];
			}
		}
	}
}
walk(site);
process.stdout.write(JSON.stringify(moved, Object.keys(moved).sort()) + '\n');

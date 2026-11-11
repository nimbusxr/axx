#!/usr/bin/env node
// Checks that a built site loads nothing from another host, so that a copy of
// it (a release's docs archive, served by `axx docs serve` or an
// organization's own server) works where axx.nimbusxr.us and every CDN are
// out of reach: no script, stylesheet, font, image, frame or module from an
// absolute or protocol-relative address. Links (<a href>) and metadata
// (canonical, Open Graph) may name other hosts: a page works without them.
//   node scripts/check-offline.mjs <site>
import fs from 'node:fs';
import path from 'node:path';

const site = process.argv[2];
if (!site) {
	console.error('usage: check-offline.mjs <site>');
	process.exit(2);
}

// An address of another host.
const external = (url) => /^(https?:)?\/\//i.test(url.trim());

// The attributes that load what they name, by element.
const loads = {
	script: ['src'],
	img: ['src', 'srcset'],
	source: ['src', 'srcset'],
	video: ['src', 'poster'],
	audio: ['src'],
	track: ['src'],
	iframe: ['src'],
	embed: ['src'],
	object: ['data'],
	input: ['src'],
	link: ['href', 'imagesrcset'],
};
// The <link> relations that load what they name.
const loadingRels = new Set(['stylesheet', 'preload', 'modulepreload', 'prefetch', 'preconnect', 'dns-prefetch', 'icon', 'shortcut', 'apple-touch-icon', 'mask-icon', 'manifest']);

const tag = /<(script|img|source|video|audio|track|iframe|embed|object|input|link)\b([^>]*)>/gi;
const attr = /([a-z-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))/gi;
const cssURL = /url\(\s*(['"]?)([^'")]+)\1\s*\)|@import\s+(['"])([^'"]+)\3/gi;
const jsImport = /\bimport\s*\(\s*(['"`])((?:https?:)?\/\/[^'"`]+)\1|\bfrom\s*(['"])((?:https?:)?\/\/[^'"]+)\3/g;

const problems = [];
const report = (file, what, url) => problems.push(`${path.relative(site, file)}: ${what} loads ${url}`);

function urlsOf(name, value) {
	return name.endsWith('srcset') ? value.split(',').map((c) => c.trim().split(/\s+/)[0]).filter(Boolean) : [value];
}

function checkCSS(file, css, where) {
	for (const m of css.matchAll(cssURL)) {
		const url = m[2] ?? m[4];
		if (external(url)) report(file, where, url);
	}
}

function checkHTML(file, html) {
	for (const m of html.matchAll(tag)) {
		const el = m[1].toLowerCase();
		const attrs = {};
		for (const a of m[2].matchAll(attr)) attrs[a[1].toLowerCase()] = a[3] ?? a[4] ?? a[5] ?? '';
		if (el === 'link' && !(attrs.rel ?? '').toLowerCase().split(/\s+/).some((r) => loadingRels.has(r))) continue;
		for (const name of loads[el]) {
			if (!(name in attrs)) continue;
			for (const url of urlsOf(name, attrs[name])) {
				if (external(url)) report(file, `<${el} ${name}>`, url);
			}
		}
	}
	for (const m of html.matchAll(/<style\b[^>]*>([\s\S]*?)<\/style>/gi)) checkCSS(file, m[1], '<style>');
	for (const m of html.matchAll(/\bstyle\s*=\s*"([^"]*)"/gi)) checkCSS(file, m[1], 'a style attribute');
}

let files = 0;
function walk(dir) {
	for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
		const p = path.join(dir, e.name);
		if (e.isDirectory()) {
			walk(p);
			continue;
		}
		const ext = path.extname(e.name);
		if (ext === '.html') {
			files++;
			checkHTML(p, fs.readFileSync(p, 'utf8'));
		} else if (ext === '.css') {
			files++;
			checkCSS(p, fs.readFileSync(p, 'utf8'), 'the stylesheet');
		} else if (ext === '.js' || ext === '.mjs') {
			files++;
			for (const m of fs.readFileSync(p, 'utf8').matchAll(jsImport)) report(p, 'the script', m[2] ?? m[4]);
		}
	}
}
walk(site);

if (files === 0) {
	console.error(`check-offline: ${site} has no pages`);
	process.exit(1);
}
if (problems.length) {
	console.error(`check-offline: what the site loads from other hosts, which a copy of it cannot reach:\n  ${problems.join('\n  ')}`);
	process.exit(1);
}
console.log(`check-offline: ${files} files load nothing from other hosts`);

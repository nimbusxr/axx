// Runs router.js in Node, with a stand-in for its KeyValueStore:
//   node docs/cloudfront/router_test.mjs
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const moved = { '/references/configuration/': '/references/config/', '/next/docs/': '/next/' };
const src = fs.readFileSync(new URL('./router.js', import.meta.url), 'utf8')
	.replace("import cf from 'cloudfront';", `const moved = ${JSON.stringify(moved)};
const cf = { kvs: () => ({ get: async (k) => { if (k in moved) return moved[k]; throw new Error('not found'); } }) };`)
	+ '\nexport { handler };';
const file = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'router-')), 'router.mjs');
fs.writeFileSync(file, src);
const { handler } = await import(file);

const browser = 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8';
const opencode = 'text/markdown;q=1.0, text/x-markdown;q=0.9, text/plain;q=0.8, text/html;q=0.7, */*;q=0.1';
const claude = 'text/markdown, text/html, */*';
const cases = [
	['/', browser, '/index.html'],
	['/', opencode, '/index.md'],
	['/next/', claude, '/next/index.md'],
	['/next/', browser, '/next/index.html'],
	['/guides/install/', claude, '/guides/install.md'],
	['/guides/install/', 'text/html, text/markdown', '/guides/install/index.html'],
	['/next/guides/install/', 'text/markdown', '/next/guides/install.md'],
	['/guides/install/', '', '/guides/install/index.html'],
	['/_astro/x.css', 'text/markdown', '/_astro/x.css'],
	['/guides/install.md', '', '/guides/install.md'],
	['/guides/install', '', '301 /guides/install/'],
	['/references/configuration/', claude, '301 /references/config/'],
	['/references/configuration', '', '301 /references/config/'],
	['/next/docs/', browser, '301 /next/'],
];
let failed = 0;
for (const [uri, accept, want] of cases) {
	const r = await handler({ request: { uri, headers: accept ? { accept: { value: accept } } : {} } });
	const got = r.statusCode ? `${r.statusCode} ${r.headers.location.value}` : r.uri;
	if (got !== want) {
		failed++;
		console.error(`${uri} [${accept}]: ${got}, want ${want}`);
	}
}
if (failed) process.exit(1);
console.log(`router: ${cases.length} cases ok`);

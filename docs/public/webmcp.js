// The docs' tools for agents that browse them, through WebMCP
// (document.modelContext, https://developer.chrome.com/docs/ai/webmcp/imperative-api;
// navigator.modelContext before Chrome 150). Chrome offers it in an origin
// trial: a page needs the trial's token (a meta tag) for its visitors to have
// it. A browser without it is left alone. Agents that fetch pages instead
// have the same in Markdown: each page's .md twin, the step index and
// llms.txt; and coding agents have it all from axx itself, offline
// (`axx pack list`, `axx steps`, `axx steps search`).
//
// The tools read the site's own Markdown and search index, below the base
// this script is served from (/ for the release's docs, /next/ for main's).

const base = new URL('./', import.meta.url);

// A tool's result is the text the agent reads.
const text = (t) => t;

/** A page of the site, in Markdown. */
async function markdown(path) {
	const clean = String(path || '')
		.replace(/^https?:\/\/[^/]+/, '')
		.replace(/^\/+/, '')
		.replace(/[?#].*$/, '')
		.replace(/\/+$/, '')
		.replace(/\.md$/, '');
	if (clean.split('/').includes('..')) throw new Error(`not a page of these docs: ${path}`);
	// A path as search gives it, below the base: /next/guides/... is guides/... of /next/.
	const below = base.pathname.replace(/^\/+|\/+$/g, '');
	const page = below && (clean === below || clean.startsWith(`${below}/`)) ? clean.slice(below.length + 1) : clean;
	const res = await fetch(new URL(`${page || 'index'}.md`, base));
	if (!res.ok) throw new Error(`no page ${path} (${res.status}); axx_search_docs finds pages`);
	return res.text();
}

/** An excerpt of the search index as text: no tags, entities decoded. */
const plain = (html) =>
	html
		.replace(/<[^>]+>/g, '')
		.replace(/&lt;/g, '<')
		.replace(/&gt;/g, '>')
		.replace(/&quot;/g, '"')
		.replace(/&#39;/g, "'")
		.replace(/&amp;/g, '&');

const readOnly = { readOnlyHint: true };

const tools = [
	{
		name: 'axx_packs',
		description: "axx's packs, one line each: what each one tests and reaches. A project lists the packs it uses in axx-packs.yaml (`axx pack add <name>`).",
		inputSchema: { type: 'object', properties: {} },
		annotations: readOnly,
		execute: async () => text(await markdown('references/packs')),
	},
	{
		name: 'axx_pack',
		description: "One pack's reference: what it reaches, its settings in axx.yaml, its steps with their documentation and examples, and its agent tools.",
		inputSchema: {
			type: 'object',
			properties: { name: { type: 'string', description: 'the pack, like sql, kafka or web-core' } },
			required: ['name'],
		},
		annotations: readOnly,
		execute: async ({ name }) => text(await markdown(`references/packs/${String(name).toLowerCase().replace(/[^a-z0-9-]/g, '')}`)),
	},
	{
		name: 'axx_find_steps',
		description: 'Steps whose pack, id or expression has every word of the query, a line each (pack | id | expression). Never invent step text: find it here.',
		inputSchema: {
			type: 'object',
			properties: {
				query: { type: 'string', description: 'words of what the step does, like "response status" or "kafka event key"' },
				pack: { type: 'string', description: 'one pack only, like rest' },
			},
			required: ['query'],
		},
		annotations: readOnly,
		execute: async ({ query, pack }) => {
			const index = await markdown('references/step-index');
			const words = String(query).toLowerCase().split(/\W+/).filter((w) => w.length > 1);
			const lines = index
				.split('\n')
				.filter((l) => /^[a-z0-9-]+ \| /.test(l))
				.filter((l) => !pack || l.startsWith(`${pack} | `))
				.filter((l) => words.every((w) => l.toLowerCase().includes(w)));
			if (lines.length === 0) return text(`no step has every word of "${query}"; try fewer words, or axx_packs for the packs`);
			const shown = lines.slice(0, 40);
			const more = lines.length > shown.length ? `\n(${lines.length - shown.length} more: add words to narrow it)` : '';
			return text(`${shown.join('\n')}${more}\n\naxx_pack {name} gives a pack's steps with their documentation and examples.`);
		},
	},
	{
		name: 'axx_search_docs',
		description: "Search axx's docs: the best pages for the query, with an excerpt and the path to read with axx_read_page.",
		inputSchema: {
			type: 'object',
			properties: { query: { type: 'string', description: 'what to find, like "start services before a run"' } },
			required: ['query'],
		},
		annotations: readOnly,
		execute: async ({ query }) => {
			const pagefind = await import(new URL('pagefind/pagefind.js', base).href);
			const search = await pagefind.search(String(query));
			const pages = await Promise.all(search.results.slice(0, 6).map((r) => r.data()));
			if (pages.length === 0) return text(`no page matches "${query}"`);
			return text(
				pages
					.map((p) => `${p.meta.title}: ${new URL(p.url, base).pathname}\n  ${plain(p.excerpt)}`)
					.join('\n\n'),
			);
		},
	},
	{
		name: 'axx_read_page',
		description: 'A page of the docs, in Markdown, by its path (like /guides/seed-and-query-sql/).',
		inputSchema: {
			type: 'object',
			properties: { path: { type: 'string', description: 'the page, like /guides/manage-services/' } },
			required: ['path'],
		},
		annotations: readOnly,
		execute: async ({ path }) => text(await markdown(path)),
	},
];

const context = document.modelContext ?? navigator.modelContext;
if (context && typeof context.registerTool === 'function') {
	for (const tool of tools) {
		Promise.resolve(context.registerTool(tool)).catch((err) => console.warn(`WebMCP: ${tool.name}: ${err}`));
	}
}

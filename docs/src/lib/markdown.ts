import type { CollectionEntry } from 'astro:content';
import { docs, withBase } from './docs-build';

export const SITE = 'https://axx.nimbusxr.us';

/** Where this build's pages are: the root, or /next/ for main's docs. */
const ROOT = SITE + docs.base.replace(/\/$/, '');

/** Path of the Markdown twin of a docs page: `/tutorials/quickstart.md`, `/index.md`. */
export function markdownPath(id: string): string {
	return withBase(`/${id || 'index'}.md`);
}

/**
 * Turns MDX component markup into plain Markdown. Only the handful of
 * Starlight components the site uses are handled; everything else passes through.
 */
function mdxToMarkdown(body: string): string {
	return body
		.replace(/^import\s.+?from\s+['"].+?['"];?[ \t]*$/gm, '')
		.replace(/^\{\/\*[\s\S]*?\*\/\}[ \t]*$/gm, '')
		.replace(/^<p class="[^"]*">([\s\S]*?)<\/p>[ \t]*$/gm, '> $1')
		.replace(/^[ \t]*<\/?(Tabs|CardGrid)(\s[^>]*)?>[ \t]*$/gm, '')
		.replace(/^[ \t]*<\/(TabItem|Card)>[ \t]*$/gm, '')
		.replace(/^[ \t]*<TabItem\s[^>]*label="([^"]+)"[^>]*>[ \t]*$/gm, '**$1**')
		.replace(/^[ \t]*<Card\s[^>]*title="([^"]+)"[^>]*>[ \t]*$/gm, '### $1')
		.replace(/^[ \t]*<LinkCard\s[^>]*title="([^"]+)"[^>]*href="([^"]+)"[^>]*\/>[ \t]*$/gm, '- [$1]($2)')
		// The home page's components (src/components/home).
		.replace(/^[ \t]*<\/?(FeatureGrid|Flow)(\s[^>]*)?>[ \t]*$/gm, '')
		.replace(/^[ \t]*<\/(Feature|FlowStep)>[ \t]*$/gm, '')
		.replace(/^[ \t]*<Feature\s[^>]*title="([^"]+)"[^>]*href="([^"]+)"[^>]*>[ \t]*$/gm, '### [$1]($2)')
		.replace(/^[ \t]*<(Feature|FlowStep)\s[^>]*title="([^"]+)"[^>]*>[ \t]*$/gm, '### $2')
		.replace(/^[ \t]*<HomeCta\s[^>]*primary="([^"]+)"[^>]*primaryHref="([^"]+)"[^>]*secondary="([^"]+)"[^>]*secondaryHref="([^"]+)"[^>]*\/>[ \t]*$/gm, '- [$1]($2)\n- [$3]($4)');
}

/** Makes root-relative links absolute so the Markdown works outside the site. */
function absolutize(body: string): string {
	return body.replace(/\]\(\/(?!\/)/g, `](${ROOT}/`);
}

/** The home page's twin starts with where an agent starts. */
const AGENTS_START =
	'> **Coding agents:** with axx installed, `axx --help` gives the workflow and the rules, and axx answers the rest itself, offline and for its version: ' +
	'`axx steps` (every step of the project\'s packs, read once) and `axx steps show <id>...`, `axx steps search <words>` (a step the project\'s packs lack, with the pack to add), ' +
	'`axx pack list` (what each pack reaches), `axx explain <code>` (an error). ' +
	`In a project that has run \`axx init\`, its AGENTS.md section has the same. [llms.txt](${ROOT}/llms.txt) indexes this site's Markdown.`;

/** Whether a page is a pack's reference page (references/packs/<pack>). */
export function isPackPage(id: string): boolean {
	return /^references\/packs\/[^/]+$/.test(id);
}

/**
 * The short form of a pack page, for the agents that fetch it: the page with
 * each step cut to its expression and the first paragraph of its
 * documentation. A pack page has every step's documentation, tables and
 * examples, some 50,000 characters, which an agent then carries in its
 * context to the end; axx gives each step's documentation offline, for the
 * version installed (`axx steps show <id>`), and the whole page is the full
 * twin (<page>.full.md).
 */
export function compactPackPage(body: string, id: string): string {
	const [head, ...rest] = body.split(/^## Steps$/m);
	if (rest.length === 0) return body;
	const steps = rest.join('## Steps');
	const after = steps.search(/^## (?!Steps)/m);
	const stepPart = after < 0 ? steps : steps.slice(0, after);
	const tail = after < 0 ? '' : steps.slice(after);
	const out: string[] = [];
	const sections = stepPart.split(/^### /m).slice(1);
	const names = new Set(sections.map((sec) => sec.split('\n', 1)[0].trim()));
	for (const section of sections) {
		const name = section.split('\n', 1)[0].trim();
		// A step's form for a named service (`rest.request.header.on`) folds into the step.
		if (/\.on`$/.test(name) && names.has(name.replace(/\.on`$/, '`'))) continue;
		const twin = names.has(name.replace(/`$/, '.on`')) ? ' (+ `.on`, for a named service)' : '';
		const code = /```gherkin\n([\s\S]*?)```/.exec(section);
		const text = section
			.slice(code ? code.index + code[0].length : name.length)
			.split(/\n\s*\n/)
			.map((p) => p.trim())
			.find((p) => p && !p.startsWith('The table may start') && !p.startsWith('|') && !p.startsWith('**'));
		const lines = code ? code[1].trim().split('\n').map((l) => l.trim()).join(' ') : '';
		out.push(`- ${name}: \`${lines}\`${twin}${text ? ` ${text.split('\n')[0]}` : ''}`);
	}
	const pack = id.split('/').pop();
	return [
		head.trim(),
		`## Steps\n\nEach step's expression and what it does. Its documentation, parameters and examples: \`axx steps show <id>\` (offline, for the axx installed), or this page in full: ${ROOT}/${id}.full.md. The pack's steps, whether the project uses it or not: \`axx pack show ${pack}\`.\n\n${out.join('\n')}`,
		tail.trim(),
	]
		.filter(Boolean)
		.join('\n\n');
}

/**
 * The Markdown twin: title, description and the page source; a pack page's
 * in its short form (compactPackPage) unless full.
 */
export function toMarkdown(entry: CollectionEntry<'docs'>, full = false): string {
	const { title, description, hero } = entry.data as {
		title: string;
		description?: string;
		hero?: { tagline?: string; actions?: { text: string; link: string }[] };
	};
	let body = entry.body ?? '';
	if (entry.filePath?.endsWith('.mdx')) body = mdxToMarkdown(body);
	// Maintainer notes are not content.
	body = body.replace(/^<!-- (TODO\(verify\)|Code generated)[\s\S]*?-->[ \t]*$/gm, '').replace(/\n{3,}/g, '\n\n');
	const parts = [`# ${title}`];
	if (description) parts.push(`> ${description}`);
	if (hero?.tagline) parts.push(hero.tagline);
	// The home page's links are relative to it: from the site's root here.
	const rooted = (link: string) => (/^[a-z]+:|^\//i.test(link) ? link : `/${link}`);
	// The home page's twin leaves its hero's links to people: agents followed them.
	if (hero?.actions?.length && entry.id !== 'index') parts.push(hero.actions.map((a) => `- [${a.text}](${rooted(a.link)})`).join('\n'));
	parts.push(`Source: ${ROOT}${entry.id === 'index' ? '/' : `/${entry.id}/`}`);
	// An agent that lands here from a project's README: what axx tells it
	// itself, instead of the page for people. No list of this site's pages:
	// agents followed it instead of asking axx (llms.txt has it).
	if (entry.id === 'index') {
		parts.push(AGENTS_START);
		return absolutize(parts.join('\n\n')) + '\n';
	}
	parts.push((isPackPage(entry.id) && !full ? compactPackPage(body, entry.id) : body).trim());
	return absolutize(parts.join('\n\n')) + '\n';
}

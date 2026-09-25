import type { CollectionEntry } from 'astro:content';

export const SITE = 'https://axx.nimbusxr.us';

/** Path of the Markdown twin of a docs page: `/tutorials/quickstart.md`, `/index.md`. */
export function markdownPath(id: string): string {
	return `/${id || 'index'}.md`;
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
		.replace(/^[ \t]*<LinkCard\s[^>]*title="([^"]+)"[^>]*href="([^"]+)"[^>]*\/>[ \t]*$/gm, '- [$1]($2)');
}

/** Makes root-relative links absolute so the Markdown works outside the site. */
function absolutize(body: string): string {
	return body.replace(/\]\(\/(?!\/)/g, `](${SITE}/`);
}

/** The Markdown twin: title, description and the page source. */
export function toMarkdown(entry: CollectionEntry<'docs'>): string {
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
	if (hero?.actions?.length) parts.push(hero.actions.map((a) => `- [${a.text}](${a.link})`).join('\n'));
	parts.push(`Source: ${SITE}${entry.id === 'index' ? '/' : `/${entry.id}/`}`);
	parts.push(body.trim());
	return absolutize(parts.join('\n\n')) + '\n';
}

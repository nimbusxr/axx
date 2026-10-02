import type { APIRoute, GetStaticPaths } from 'astro';
import { getCollection, type CollectionEntry } from 'astro:content';
import { aliases } from '../lib/aliases.mjs';
import { toMarkdown } from '../lib/markdown';

// The Markdown twin of every docs page: /tutorials/quickstart/ → /tutorials/quickstart.md,
// the landing page → /index.md. Agents fetch these instead of scraping HTML. The pages'
// aliases (src/lib/aliases.mjs) have them too: /packs/rest.md is /references/packs/rest.md.
export const getStaticPaths = (async () => {
	const entries = await getCollection('docs');
	const byId = new Map(entries.map((entry) => [entry.id, entry]));
	const twins = entries.map((entry) => ({ params: { slug: entry.id }, props: { entry } }));
	for (const [alias, slug] of aliases(entries.map((entry) => entry.id))) {
		const entry = byId.get(slug);
		if (entry) twins.push({ params: { slug: alias }, props: { entry } });
	}
	return twins;
}) satisfies GetStaticPaths;

export const GET: APIRoute<{ entry: CollectionEntry<'docs'> }> = ({ props }) =>
	new Response(toMarkdown(props.entry), {
		headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
	});

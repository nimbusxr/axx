import type { APIRoute, GetStaticPaths } from 'astro';
import { getCollection, type CollectionEntry } from 'astro:content';
import { toMarkdown } from '../lib/markdown';

// The Markdown twin of every docs page: /tutorials/quickstart/ → /tutorials/quickstart.md,
// the landing page → /index.md. Agents fetch these instead of scraping HTML.
export const getStaticPaths = (async () => {
	const entries = await getCollection('docs');
	return entries.map((entry) => ({ params: { slug: entry.id }, props: { entry } }));
}) satisfies GetStaticPaths;

export const GET: APIRoute<{ entry: CollectionEntry<'docs'> }> = ({ props }) =>
	new Response(toMarkdown(props.entry), {
		headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
	});

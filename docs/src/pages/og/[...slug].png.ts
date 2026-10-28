import type { APIRoute, GetStaticPaths } from 'astro';
import { getCollection, type CollectionEntry } from 'astro:content';
import { card } from '../../lib/og';

// The Open Graph card of every docs page, /og/<page>.png (/og/index.png for the home page),
// which src/route-data.ts names in each page's head.
export const getStaticPaths = (async () =>
	(await getCollection('docs')).map((entry) => ({ params: { slug: entry.id || 'index' }, props: { entry } }))) satisfies GetStaticPaths;

export const GET: APIRoute<{ entry: CollectionEntry<'docs'> }> = async ({ props }) =>
	new Response(await card(props.entry), { headers: { 'Content-Type': 'image/png' } });

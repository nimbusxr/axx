import type { APIRoute } from 'astro';
import { card } from '../lib/og';

// The site's Open Graph card, /og.png, which src/route-data.ts names in every page's head.
export const GET: APIRoute = async () => new Response(await card(), { headers: { 'Content-Type': 'image/png' } });

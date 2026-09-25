import { defineRouteMiddleware } from '@astrojs/starlight/route-data';
import { markdownPath } from './lib/markdown';

// Every page advertises its Markdown twin (served by src/pages/[...slug].md.ts)
// so agents can fetch the source instead of scraping HTML.
export const onRequest = defineRouteMiddleware((context) => {
	const route = context.locals.starlightRoute;
	route.head.push({
		tag: 'link',
		attrs: { rel: 'alternate', type: 'text/markdown', href: markdownPath(route.id) },
	});
});

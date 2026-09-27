import { defineRouteMiddleware } from '@astrojs/starlight/route-data';
import { docs, withoutBase } from './lib/docs-build';
import { markdownPath } from './lib/markdown';

// Every page advertises its Markdown twin (served by src/pages/[...slug].md.ts)
// so agents can fetch the source instead of scraping HTML.
export const onRequest = defineRouteMiddleware((context) => {
	const route = context.locals.starlightRoute;
	route.head.push({
		tag: 'link',
		attrs: { rel: 'alternate', type: 'text/markdown', href: markdownPath(route.id) },
	});
	// Main's docs say so, and lead to the same page in the release's.
	if (docs.channel === 'next' && !route.entry.data.banner) {
		const release = docs.release || 'the latest release';
		route.entry.data.banner = {
			content: `These are the docs of main, not released yet. See <a href="${withoutBase(context.url.pathname)}">the docs of ${release}</a>, the latest release.`,
		};
	}
});

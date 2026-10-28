import { defineRouteMiddleware } from '@astrojs/starlight/route-data';
import { docs, withBase, withoutBase } from './lib/docs-build';
import { markdownPath, SITE } from './lib/markdown';
import { CARD } from './lib/og';

// Every page advertises its Markdown twin (served by src/pages/[...slug].md.ts)
// so agents can fetch the source instead of scraping HTML.
export const onRequest = defineRouteMiddleware((context) => {
	const route = context.locals.starlightRoute;
	route.head.push({
		tag: 'link',
		attrs: { rel: 'alternate', type: 'text/markdown', href: markdownPath(route.id) },
	});
	// What a link to the page shows where it is shared: the site's card (src/pages/og.png.ts).
	if (route.id !== '404') {
		const image = new URL(withBase('/og.png'), SITE).href;
		const alt = 'axx: the verification layer for agentic software factories, beside its ASCII axe';
		route.head.push(
			{ tag: 'meta', attrs: { property: 'og:image', content: image } },
			{ tag: 'meta', attrs: { property: 'og:image:type', content: 'image/png' } },
			{ tag: 'meta', attrs: { property: 'og:image:width', content: String(CARD.width) } },
			{ tag: 'meta', attrs: { property: 'og:image:height', content: String(CARD.height) } },
			{ tag: 'meta', attrs: { property: 'og:image:alt', content: alt } },
			{ tag: 'meta', attrs: { name: 'twitter:image', content: image } },
			{ tag: 'meta', attrs: { name: 'twitter:image:alt', content: alt } },
		);
	}
	// Main's docs say so, and lead to the same page in the release's.
	if (docs.channel === 'next' && !route.entry.data.banner) {
		const release = docs.release || 'the latest release';
		route.entry.data.banner = {
			content: `These are the docs of main, not released yet. See <a href="${withoutBase(context.url.pathname)}">the docs of ${release}</a>, the latest release.`,
		};
	}
});

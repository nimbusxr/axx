/**
 * Which docs a build is (.github/workflows/docs.yml): the latest release's,
 * at the site's root, or main's, at /next/; or a release's archive
 * (release.yml), which `axx docs serve` and organizations' copies serve at
 * the root of their host, without main's beside it. A build without
 * AXX_DOCS_CHANNEL (a local one) is none of them, at the root.
 */
export function docsBuild(env = process.env) {
	const channel = ['latest', 'next', 'archive'].includes(env.AXX_DOCS_CHANNEL) ? env.AXX_DOCS_CHANNEL : '';
	return {
		channel,
		// The latest release, like v0.1.1: the one the root's docs are.
		release: env.AXX_DOCS_RELEASE ?? '',
		base: channel === 'next' ? '/next/' : '/',
	};
}

/**
 * Puts the site's base path in front of the docs' site-absolute links
 * (/guides/install/), whose pages are below it in main's docs.
 */
export function siteBase(base) {
	const prefix = base.replace(/\/$/, '');
	const rebase = (url) => (prefix && typeof url === 'string' && url.startsWith('/') && !url.startsWith('//') && url !== prefix && !url.startsWith(`${prefix}/`) ? prefix + url : undefined);
	const node = (n) => {
		const url = rebase(n.url);
		return url ? { ...n, url } : undefined;
	};
	return { name: 'axx-site-base', link: node, definition: node, image: node };
}

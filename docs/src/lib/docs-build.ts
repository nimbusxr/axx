/** Which docs this build is: see src/lib/site-base.mjs. */
declare const __AXX_DOCS__: { channel: '' | 'latest' | 'next'; release: string; base: string };

export const docs = __AXX_DOCS__;

/** A site-absolute path ("/guides/install/") below the site's base. */
export function withBase(path: string): string {
	return docs.base.replace(/\/$/, '') + path;
}

/** A page's path without the site's base: the same page in the other docs. */
export function withoutBase(pathname: string): string {
	const prefix = docs.base.replace(/\/$/, '');
	return prefix && pathname.startsWith(prefix + '/') ? pathname.slice(prefix.length) : pathname;
}

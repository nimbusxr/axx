import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import type { CollectionEntry } from 'astro:content';
import { initWasm, Resvg } from '@resvg/resvg-wasm';
import satori from 'satori';
import { withBase } from './docs-build';

// The Open Graph card of every page (src/pages/og/[...slug].png.ts): what a link to it shows
// where it is shared. 1200 by 630, on the site's own black, in its Inter.

export const CARD = { width: 1200, height: 630 };

/** The axe of AsciiAxe.astro's fallback, the site's mark. */
const AXE = String.raw`
                 __..------.
         .--===###%%%%%%%%%%)
         |#######%%%%%%%%%%%|
         '---.##%%%%%%%%%%%/
             |##%%%%%%%%%/
             |#| '%%%%%:'
             |#|   '::'
             |#|
             |#|
            /#/
           /#/
          /#/
          |#|
          '::'`.split('\n').slice(1);

const require = createRequire(join(process.cwd(), 'package.json'));

let ready: Promise<{ name: string; data: Buffer; weight: 400 | 600; style: 'normal' }[]> | undefined;
/** Loads resvg's WebAssembly and the fonts, once a build. */
function setup() {
	ready ??= (async () => {
		// Once a process: a dev server's reload of this module finds it done already.
		const memo = globalThis as { axxResvg?: Promise<void> };
		await (memo.axxResvg ??= readFile(require.resolve('@resvg/resvg-wasm/index_bg.wasm')).then((wasm) => initWasm(wasm)));
		const font = (weight: 400 | 600) => readFile(require.resolve(`@fontsource/inter/files/inter-latin-${weight}-normal.woff`));
		return [
			{ name: 'Inter', data: await font(400), weight: 400, style: 'normal' },
			{ name: 'Inter', data: await font(600), weight: 600, style: 'normal' },
		];
	})();
	return ready;
}

/** A text cut at a word to at most max characters. */
function clip(text: string, max: number): string {
	if (text.length <= max) return text;
	return text.slice(0, text.lastIndexOf(' ', max - 1)).replace(/[\s,;:.-]+$/, '') + '…';
}

/** Markdown's marks out of a description: `code`, **bold**, [links](...). */
const plain = (text: string) => text.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/[`*_]/g, '');

const KINDS: Record<string, string> = { tutorials: 'Tutorial', guides: 'Guide', references: 'Reference', explanations: 'Explanation' };

type Node = { type: string; props: { style?: Record<string, unknown>; children?: Node | string | (Node | string)[] } };
const el = (type: string, style: Record<string, unknown>, children?: Node['props']['children']): Node => ({ type, props: { style, children } });

/** The card of a docs page, as a PNG. */
export async function card(entry: CollectionEntry<'docs'>): Promise<Uint8Array> {
	const fonts = await setup();
	const data = entry.data as { title: string; description?: string; eyebrow?: string; hero?: { title?: string } };
	const home = entry.id === 'index';
	// The home page's title is its hero's: two sentences, the second quieter.
	// A pack's page is titled as its sidebar names it ("Core", under Models): its card names the pack.
	const pack = entry.id.match(/^references\/packs\/([^/]+)$/)?.[1];
	const [first, second] = home && data.hero?.title ? data.hero.title.replace(/<\/?span>/g, '\n').split('\n').map((s) => s.trim()).filter(Boolean) : [pack ?? data.title];
	const label = home ? data.eyebrow ?? '' : pack ? 'Pack reference' : KINDS[entry.id.split('/')[0]] ?? 'Docs';
	const description = home
		? 'Open-source acceptance testing that verifies running software across APIs, events, data, native apps and AI.'
		: clip(plain(data.description ?? ''), 125);
	const titleSize = home ? 68 : first.length > 48 ? 50 : 58;
	const address = 'axx.nimbusxr.us' + withBase(home ? '/' : `/${entry.id}/`);

	// The axe in the site's colors (AsciiAxe.astro): a steel head with a red strip along its top, a
	// wooden haft with its grip wrapped white.
	const color = (ch: string, row: number, col: number) =>
		row === 0 ? '#8f3530' : row >= 9 && ch === '#' ? '#d8d5ce' : row >= 4 && col <= 16 ? '#7d5434' : '#5a5a62';
	const axe = AXE.flatMap((line, row) =>
		[...line].flatMap((ch, col) =>
			ch === ' ' ? [] : [el('div', { position: 'absolute', left: col * 17, top: row * 31, fontSize: 28, color: color(ch, row, col) }, ch)],
		),
	);
	const root = el(
		'div',
		{ width: CARD.width, height: CARD.height, display: 'flex', position: 'relative', background: '#080808', fontFamily: 'Inter', color: '#b8b8bd' },
		[
			el('div', { position: 'absolute', top: 24, left: 24, right: 24, bottom: 24, border: '1px solid #222224', borderRadius: 20 }),
			el('div', { position: 'absolute', top: 126, right: 70, width: 520, height: 440, display: 'flex' }, axe),
			el('div', { position: 'absolute', top: 78, right: 72, fontSize: 17, color: '#606065', letterSpacing: 0.2 }, address),
			// Satori adds padding to a box's height: the column is the card's height less its padding.
			el('div', { position: 'relative', display: 'flex', flexDirection: 'column', width: 760, height: CARD.height - 62 - 66, padding: '62px 0 66px 72px' }, [
				el('div', { display: 'flex', alignItems: 'center' }, [
					el('div', { fontSize: 38, fontWeight: 600, letterSpacing: -2.8, color: '#b5b5bb' }, 'axx'),
					el('div', { fontSize: 38, fontWeight: 600, letterSpacing: -2.8, color: '#494951', marginRight: 14 }, 'eptance'),
					el('div', { fontSize: 12, fontWeight: 600, letterSpacing: 1, color: '#000', background: '#a2a2a8', borderRadius: 999, padding: '3px 9px', marginTop: -10 }, 'BETA'),
				]),
				el('div', { display: 'flex', flexDirection: 'column', marginTop: 'auto' }, [
					el('div', { display: 'flex', alignItems: 'center', fontSize: 17, fontWeight: 600, letterSpacing: 1.8, color: '#b89470', textTransform: 'uppercase' }, [
						el('div', { width: 30, height: 2, background: '#a8443a', marginRight: 14 }),
						label,
					]),
					el('div', { display: 'flex', flexDirection: 'column', marginTop: 22, fontSize: titleSize, fontWeight: 600, lineHeight: 1.05, letterSpacing: -titleSize * 0.05, color: '#d0d0d4' }, [
						el('div', {}, clip(first, 90)),
						...(second ? [el('div', { color: '#939397' }, second)] : []),
					]),
					el('div', { maxWidth: 660, marginTop: 22, fontSize: 24, lineHeight: 1.45, color: '#8c8c90' }, description),
				]),
			]),
		],
	);
	const svg = await satori(root as never, { ...CARD, fonts });
	return new Resvg(svg, { fitTo: { mode: 'width', value: CARD.width } }).render().asPng();
}

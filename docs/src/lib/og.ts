import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { initWasm, Resvg } from '@resvg/resvg-wasm';
import satori from 'satori';
import { asciiCell } from './axe-ascii';
import { createAxeScene } from './axe-scene';

// The site's Open Graph card (src/pages/og.png.ts): what a link to any of its pages shows where it
// is shared. The home page's axe, drawn as the home page draws it, and the logo.

export const CARD = { width: 1200, height: 630 };

const require = createRequire(join(process.cwd(), 'package.json'));

/** A canvas's 2D context, as much of one as the axe's scene draws with, writing SVG. */
class SvgInk {
	readonly canvas: { width: number; height: number };
	readonly paths: string[] = [];
	fillStyle = '#000';
	strokeStyle = '#000';
	lineWidth = 1;
	lineJoin = 'miter';
	lineCap = 'butt';
	globalAlpha = 1;
	private d = '';
	private at = { x: 0, y: 0, k: 1 };
	private saved: { x: number; y: number; k: number; alpha: number }[] = [];
	constructor(width: number, height: number) { this.canvas = { width, height }; }
	save() { this.saved.push({ ...this.at, alpha: this.globalAlpha }); }
	restore() { const { alpha, ...at } = this.saved.pop()!; this.at = at; this.globalAlpha = alpha; }
	// The scene only moves and scales its drawing evenly.
	translate(x: number, y: number) { this.at.x += x * this.at.k; this.at.y += y * this.at.k; }
	scale(k: number) { this.at.k *= k; }
	clearRect() { this.paths.length = 0; }
	beginPath() { this.d = ''; }
	moveTo(x: number, y: number) { this.d += `M${this.point(x, y)}`; }
	lineTo(x: number, y: number) { this.d += `L${this.point(x, y)}`; }
	closePath() { this.d += 'Z'; }
	fill() { this.paths.push(`<path d="${this.d}" fill="${svgColor(this.fillStyle)}" fill-opacity="${this.globalAlpha}"/>`); }
	stroke() {
		this.paths.push(`<path d="${this.d}" fill="none" stroke="${svgColor(this.strokeStyle)}" stroke-opacity="${this.globalAlpha}" stroke-width="${this.lineWidth * this.at.k}" stroke-linejoin="${this.lineJoin}" stroke-linecap="${this.lineCap}"/>`);
	}
	private point(x: number, y: number) { return `${x * this.at.k + this.at.x} ${y * this.at.k + this.at.y}`; }
}
/** CSS's rgb(r g b), which the scene writes, as SVG's rgb(r,g,b). */
const svgColor = (color: string) => color.replace(/rgb\((\S+) (\S+) (\S+)\)/, 'rgb($1,$2,$3)');

type Node = { type: string; props: { style?: Record<string, unknown>; children?: Node | string | (Node | string)[] } };
const el = (type: string, style: Record<string, unknown>, children?: Node['props']['children']): Node => ({ type, props: { style, children } });

/**
 * The axe as the home page holds it (with its motion reduced), over width by height: the scene
 * sampled a cell at a time and each cell's character drawn in its color (axe-ascii.ts).
 */
function axe(width: number, height: number, cell: number): Node[] {
	const ink = new SvgInk(width, height);
	createAxeScene(ink as unknown as CanvasRenderingContext2D)(0, true);
	const columns = Math.round(width / cell), rows = Math.round(height / (cell * 1.5));
	const cellWidth = width / columns, cellHeight = height / rows;
	const scene = new Resvg(`<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}">${ink.paths.join('')}</svg>`).render().pixels;
	// The sample a canvas's drawImage makes of the scene (AsciiAxe.astro): bilinear, at each cell's
	// center, of its premultiplied pixels (resvg's are), then not premultiplied, as getImageData's.
	const pixels = new Uint8ClampedArray(columns * rows * 4);
	const at = (x: number, y: number, c: number) => scene[(Math.min(height - 1, Math.max(0, y)) * width + Math.min(width - 1, Math.max(0, x))) * 4 + c];
	for (let y = 0; y < rows; y++) {
		for (let x = 0; x < columns; x++) {
			const sx = (x + .5) * cellWidth - .5, sy = (y + .5) * cellHeight - .5;
			const x0 = Math.floor(sx), y0 = Math.floor(sy), fx = sx - x0, fy = sy - y0;
			const sample = [0, 1, 2, 3].map((c) =>
				(at(x0, y0, c) * (1 - fx) + at(x0 + 1, y0, c) * fx) * (1 - fy) + (at(x0, y0 + 1, c) * (1 - fx) + at(x0 + 1, y0 + 1, c) * fx) * fy);
			const index = (y * columns + x) * 4;
			for (let c = 0; c < 3; c++) pixels[index + c] = sample[3] ? sample[c] * 255 / sample[3] : 0;
			pixels[index + 3] = sample[3];
		}
	}
	const glyphs: Node[] = [];
	for (let y = 0; y < rows; y++) {
		for (let x = 0; x < columns; x++) {
			const at = asciiCell(pixels, (y * columns + x) * 4, 'dark');
			if (!at) continue;
			glyphs.push(el('div', {
				position: 'absolute', left: x * cellWidth, top: y * cellHeight, width: cellWidth, height: cellHeight,
				display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: cellHeight * .92, color: at.color,
			}, at.glyph));
		}
	}
	return glyphs;
}

/** The card, as a PNG. */
export async function card(): Promise<Uint8Array> {
	// Once a process: a dev server's reload of this module finds it done already.
	const memo = globalThis as { axxResvg?: Promise<void> };
	await (memo.axxResvg ??= readFile(require.resolve('@resvg/resvg-wasm/index_bg.wasm')).then((wasm) => initWasm(wasm)));
	const font = (weight: 400 | 600) => readFile(require.resolve(`@fontsource/inter/files/inter-latin-${weight}-normal.woff`));
	const fonts = [
		{ name: 'Inter', data: await font(400), weight: 400, style: 'normal' },
		{ name: 'Inter', data: await font(600), weight: 600, style: 'normal' },
	] as const;
	// The logo as the site's header sets it (custom.css's .site-title), large.
	const size = 112;
	const word = { fontSize: size, fontWeight: 600, letterSpacing: -size * .075 };
	const root = el('div', { width: CARD.width, height: CARD.height, display: 'flex', position: 'relative', background: '#080808', fontFamily: 'Inter' }, [
		el('div', { position: 'absolute', top: -150, right: -150, width: 1040, height: 900, display: 'flex' }, axe(1040, 900, 8)),
		el('div', { position: 'absolute', left: 72, bottom: 64, display: 'flex', alignItems: 'center' }, [
			el('div', { ...word, color: '#b5b5bb' }, 'axx'),
			el('div', { ...word, color: '#494951', marginRight: 22 }, 'eptance'),
			el('div', { fontSize: 20, fontWeight: 600, letterSpacing: 1.2, color: '#000', background: '#b5b5bb', borderRadius: 999, padding: '4px 12px', marginTop: -50 }, 'BETA'),
		]),
	]);
	const svg = await satori(root as never, { ...CARD, fonts: [...fonts] });
	return new Resvg(svg, { fitTo: { mode: 'width', value: CARD.width } }).render().asPng();
}

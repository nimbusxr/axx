/** How the axe's scene (axe-scene.ts) becomes characters: one a cell of a sample of it. */
import { MATERIAL_OFFSET } from './axe-scene';

export type Theme = 'dark' | 'light';
type Ramp = [[number, number, number], [number, number, number]];
// The colors of the axe's materials, from a sparse character's to a dense one's: on the dark theme
// they brighten out of the black, on the light one they darken out of the gray.
const MATERIALS: Record<Theme, { wood: Ramp; wrap: Ramp; paint: Ramp }> = {
	dark: { wood: [[36, 24, 15], [146, 94, 52]], wrap: [[70, 69, 66], [228, 225, 218]], paint: [[58, 17, 15], [168, 48, 38]] },
	light: { wood: [[112, 84, 62], [66, 36, 12]], wrap: [[92, 92, 96], [18, 18, 20]], paint: [[132, 64, 58], [112, 20, 16]] },
};
const GLYPHS = '.,:;=+*#%@';

/** The character and color of a sample's pixel at index (RGBA, not premultiplied), or none. */
export function asciiCell(pixels: ArrayLike<number>, index: number, theme: Theme): { glyph: string; color: string } | undefined {
	const value = pixels[index] * pixels[index + 3] / 255;
	if (value < 9) return;
	const isLine = pixels[index] - pixels[index + 1] > 12;
	// Blue against red is the material: wood above, the grip's wrap twice as far above,
	// paint below, steel and streaks even.
	const tint = isLine ? 0 : pixels[index + 2] - pixels[index];
	const material = tint > MATERIAL_OFFSET * 1.5 ? 'wrap' : tint > MATERIAL_OFFSET / 2 ? 'wood' : tint < -MATERIAL_OFFSET / 2 ? 'paint' : undefined;
	let color: string;
	if (material) {
		const [from, to] = MATERIALS[theme][material], f = Math.min(1, value / 230);
		color = `rgb(${from.map((c, i) => Math.round(c + (to[i] - c) * f)).join(', ')})`;
	} else if (theme === 'light') {
		// Graphite on the light gray: sparse characters mid gray, dense ones near black.
		const ink = Math.round(142 - value * .42);
		color = `rgb(${ink}, ${ink}, ${ink + 4})`;
	} else {
		const tone = Math.round(22 + value * .35);
		color = `rgb(${tone}, ${tone}, ${tone + 3})`;
	}
	const direction = (pixels[index + 2] - pixels[index + 1]) / Math.max(1, pixels[index] - pixels[index + 1]);
	const lineGlyph = direction < .15 ? '-' : direction < .45 ? '/' : direction < .75 ? '\\' : '|';
	return { glyph: isLine ? lineGlyph : GLYPHS[Math.min(GLYPHS.length - 1, Math.floor(value / 256 * GLYPHS.length))], color };
}

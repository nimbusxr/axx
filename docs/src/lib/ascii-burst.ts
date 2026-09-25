export const TITLE_BURST_DURATION = 2.6;

const ease = (n: number) => { const t = Math.max(0, Math.min(1, n)); return t * t * (3 - 2 * t); };

const random = (seed: number) => {
	const value = Math.sin(seed * 127.1 + 311.7) * 43758.5453;
	return value - Math.floor(value);
};

type BurstLayout = {
	art: string;
	font: string;
	fontSize: number;
	width: number;
	height: number;
	ratio: number;
};

/** Release each character from its exact place in the final title. */
export function createTitleBurst(ink: CanvasRenderingContext2D, layout: BurstLayout) {
	const { art, font, fontSize, width, height, ratio } = layout;
	ink.font = font;
	const metrics = ink.measureText('0');
	const cellWidth = metrics.width;
	const rows = art.split('\n');
	const left = (width - rows[0].length * cellWidth) / 2;
	const top = (height - rows.length * fontSize) / 2;
	const ascent = metrics.fontBoundingBoxAscent;
	const descent = metrics.fontBoundingBoxDescent;
	const baseline = (fontSize - ascent - descent) / 2 + ascent;
	const reach = Math.min(width, height);
	const impactX = width * .52;
	const impactY = height * .46;

	// Cache glyphs at device resolution so thousands of fragments stay inexpensive.
	const glyphs = [...new Set(art.replace(/\s/g, ''))];
	const tile = Math.ceil(fontSize * 2 * ratio);
	const atlas = document.createElement('canvas');
	atlas.width = tile * glyphs.length;
	atlas.height = tile;
	const paint = atlas.getContext('2d')!;
	paint.scale(ratio, ratio);
	paint.font = font;
	paint.textAlign = 'center';
	paint.textBaseline = 'alphabetic';
	paint.fillStyle = '#77777e';
	const tileSize = tile / ratio;
	glyphs.forEach((glyph, i) => paint.fillText(glyph, (i + .5) * tileSize, tileSize / 2 - fontSize / 2 + baseline));

	const particles = rows.flatMap((row, y) => [...row].flatMap((glyph, x) => {
		if (glyph === ' ') return [];
		const seed = y * rows[0].length + x;
		const px = left + (x + .5) * cellWidth;
		const py = top + (y + .5) * fontSize;
		const dx = px - impactX;
		const dy = py - impactY;
		const distance = Math.hypot(dx, dy);
		const angle = Math.atan2(dy, dx) + (random(seed + 1) - .5) * .8;
		const speed = reach * (.8 + random(seed + 2) * 1.4);
		return [{
			x: px, y: py, tile: glyphs.indexOf(glyph) * tile,
			vx: Math.cos(angle) * speed,
			vy: Math.sin(angle) * speed,
			delay: Math.min(.055, distance / reach * .07),
			spin: (random(seed + 3) - .5) * 10,
			depth: .85 + random(seed + 4) * .85,
			life: 1.8 + random(seed + 5) * .55,
			streak: seed % 4 === 0,
		}];
	}));

	return (time: number, shakeX = 0, shakeY = 0, foreground?: CanvasImageSource, vanishingPoint = { x: .28, y: .46 }) => {
		ink.setTransform(ratio, 0, 0, ratio, 0, 0);
		ink.clearRect(0, 0, width, height);
		if (time < 0 || time >= TITLE_BURST_DURATION) return;
		ink.save();
		ink.translate(shakeX, shakeY);
		// Recede along the actual flight axis, which shifts left as the camera
		// orbits. A short outward impulse is followed by sustained depth travel.
		const vanishX = vanishingPoint.x * width;
		const vanishY = vanishingPoint.y * height;
		for (const particle of particles) {
			const age = Math.max(0, time - particle.delay);
			// Stay solid while travelling. Fade only once the pieces are far behind us.
			const opacity = ease((particle.life - age) / .45);
			if (opacity < .015) continue;
			const travel = (1 - Math.exp(-age * 5)) / 5;
			// Sideways impact velocity, with depth increasing as we leave the debris behind.
			const perspective = 1 / (1 + age * particle.depth + age * age * .65);
			const x = vanishX + (particle.x - vanishX + particle.vx * travel) * perspective;
			const y = vanishY + (particle.y - vanishY + particle.vy * travel) * perspective;
			// Keep the farthest characters legible down to a small pixel-sized fleck.
			const size = tileSize * Math.max(perspective, 1.5 / fontSize);
			if (particle.streak && age > .04 && age < particle.life - .25) {
				// Ghosts follow the projected path, pointing back toward the foreground.
				// This makes the backward flight readable even between small ASCII cells.
				const previousAge = Math.max(0, age - .065);
				const previousTravel = (1 - Math.exp(-previousAge * 5)) / 5;
				const previousPerspective = 1 / (1 + previousAge * particle.depth + previousAge * previousAge * .65);
				const previousX = vanishX + (particle.x - vanishX + particle.vx * previousTravel) * previousPerspective;
				const previousY = vanishY + (particle.y - vanishY + particle.vy * previousTravel) * previousPerspective;
				ink.globalAlpha = opacity * .25;
				ink.drawImage(atlas, particle.tile, 0, tile, tile, previousX - size / 2, previousY - size / 2, size, size);
			}
			ink.save();
			ink.translate(x, y);
			ink.rotate(particle.spin * age);
			ink.globalAlpha = opacity;
			ink.drawImage(atlas, particle.tile, 0, tile, tile, -size / 2, -size / 2, size, size);
			ink.restore();
		}
		if (foreground) {
			// The solid source silhouette keeps the receding fragments behind the axx.
			ink.globalAlpha = 1;
			ink.globalCompositeOperation = 'destination-out';
			ink.drawImage(foreground, 0, 0, width, height);
		}
		ink.restore();
	};
}

const WORDS = ['BLACK', 'BOX', 'MEET', 'AXX'] as const;

/** Sample real display typography into small characters on a shared artboard. */
export function createAsciiTitles(columns: number, rows: number) {
	const mask = document.createElement('canvas');
	const ink = mask.getContext('2d', { willReadFrequently: true })!;
	// Match the roughly 3:5 proportions of a monospace character cell.
	mask.width = columns * 6;
	mask.height = rows * 10;
	ink.font = '900 100px Inter, sans-serif';
	ink.letterSpacing = '-3px';
	const widest = ink.measureText('BLACK');
	const capHeight = widest.actualBoundingBoxAscent;
	const squeeze = .86;
	const scale = Math.min(
		mask.width * .92 / ((widest.actualBoundingBoxLeft + widest.actualBoundingBoxRight) * squeeze),
		mask.height * .84 / capHeight,
	);
	const samples = document.createElement('canvas');
	samples.width = columns;
	samples.height = rows;
	const sample = samples.getContext('2d', { willReadFrequently: true })!;
	const glyphs = ' .:-=+*#%@';
	const titles: Record<string, string> = {};
	for (const word of WORDS) {
		const metrics = ink.measureText(word);
		ink.clearRect(0, 0, mask.width, mask.height);
		ink.save();
		ink.translate(mask.width / 2, mask.height / 2);
		ink.scale(scale * squeeze, scale);
		ink.fillStyle = '#fff';
		ink.fillText(word, (metrics.actualBoundingBoxLeft - metrics.actualBoundingBoxRight) / 2, capHeight / 2);
		ink.restore();
		sample.clearRect(0, 0, columns, rows);
		sample.drawImage(mask, 0, 0, columns, rows);
		const pixels = sample.getImageData(0, 0, columns, rows).data;
		titles[word] = Array.from({ length: rows }, (_, y) => Array.from({ length: columns }, (_, x) => {
			const coverage = pixels[(y * columns + x) * 4 + 3] / 255;
			// Fine tonal grain inside the strokes; lighter punctuation along the curves.
			const grain = .82 + ((x * 17 + y * 31) % 13) / 72;
			return glyphs[Math.round(coverage * grain * (glyphs.length - 1))];
		}).join('')).join('\n');
	}
	return titles;
}

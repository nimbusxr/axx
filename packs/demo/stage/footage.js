// The screens: a browser, a phone, a desktop, as the run's cameras saw them.
// A take has their pictures, each with the moment of the run it was taken
// at; a screen shows the picture of the moment the stage plays. The pictures
// are loaded before the first is asked for, and drawn on a canvas, so that
// the picture of a moment is there the moment it is asked for.
(() => {
	const pictures = {};
	axxStage.preload = (take) => Promise.all((take.footage || []).filter((shot) => !pictures[shot.file]).map((shot) => new Promise((done) => {
		const img = new Image();
		img.onload = img.onerror = done;
		img.src = 'footage/' + shot.file;
		pictures[shot.file] = img;
	})));

	const screen = (surface) => ({
		mount(el) {
			el.innerHTML = '<div class="screen screen-' + surface + '"><canvas></canvas></div>';
		},
		render(el, state) {
			// The last picture taken by this moment of the run, or the screen's
			// first, where the moment is before it.
			let shot = null, first = null;
			for (const s of state.take.footage || []) {
				if (s.surface !== surface) continue;
				first = first || s;
				if (s.at <= state.runTime + 1e-6) shot = s;
			}
			shot = shot || first;
			const canvas = el.querySelector('canvas');
			if (!shot) { el.querySelector('.screen').hidden = true; return; }
			el.querySelector('.screen').hidden = false;
			if (canvas.dataset.file === shot.file) return;
			const img = pictures[shot.file];
			if (!img || !img.naturalWidth) return;
			canvas.dataset.file = shot.file;
			canvas.width = img.naturalWidth;
			canvas.height = img.naturalHeight;
			canvas.getContext('2d').drawImage(img, 0, 0);
		},
	});
	for (const surface of ['browser', 'phone', 'desktop']) axxStage.scene(surface, screen(surface));
})();

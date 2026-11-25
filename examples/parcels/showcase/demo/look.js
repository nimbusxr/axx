// The look of axx's own demo: behind the picture, a field of dots and two
// soft lights in the site's colors, which drift slowly.
//
// It is a project's own script (packs.demo.scenes in axx.yaml), on the stage's
// frame event: where the lights are is worked out from the moment it is
// given, never from a clock of its own, so the same picture comes out for a
// video and before an audience.
(() => {
	axxStage.on('frame', (state) => {
		const stage = document.getElementById('stage');
		let back = stage.querySelector('.backdrop');
		if (!back) {
			back = document.createElement('div');
			back.className = 'backdrop';
			back.innerHTML = '<div class="glow glow-a"></div><div class="glow glow-b"></div><div class="dots"></div>';
			stage.prepend(back);
		}
		// The lights move eight times a second: slowly enough to be felt, not watched.
		const t = Math.round(state.t * 8) / 8;
		if (back.dataset.t === String(t)) return;
		back.dataset.t = String(t);
		const at = (x, y) => 'translate(' + x.toFixed(2) + 'vw, ' + y.toFixed(2) + 'vh)';
		back.querySelector('.glow-a').style.transform = at(Math.sin(t / 9) * 7, Math.cos(t / 11) * 6);
		back.querySelector('.glow-b').style.transform = at(Math.cos(t / 10) * -8, Math.sin(t / 13) * 7);
	});
})();

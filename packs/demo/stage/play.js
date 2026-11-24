// What starts the stage: it gets the demo's take and timeline from where the
// page came from, and says when the first picture is drawn. With ?play in
// the address the stage plays by itself, in its own time, for a look at a
// demo in any browser: space stops and starts it, the arrows go from beat to
// beat.
(async () => {
	const data = await (await fetch('demo.json')).json();
	await axxStage.load(data);
	if (new URLSearchParams(location.search).has('play')) {
		let t = 0, last = performance.now(), playing = true;
		const beats = data.timeline.beats;
		const tick = (now) => {
			if (playing) t = Math.min(data.timeline.duration, t + (now - last) / 1000);
			last = now;
			axxStage.seek(t);
			requestAnimationFrame(tick);
		};
		addEventListener('keydown', (e) => {
			if (e.key === ' ') playing = !playing;
			if (e.key === 'ArrowRight' || e.key === 'PageDown') t = beats.find((b) => b > t + 0.01) ?? data.timeline.duration;
			if (e.key === 'ArrowLeft' || e.key === 'PageUp') t = [...beats].reverse().find((b) => b < t - 0.5) ?? 0;
		});
		requestAnimationFrame(tick);
	}
	window.axxReady = true;
})();

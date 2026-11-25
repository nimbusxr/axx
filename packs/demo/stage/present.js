// A show before an audience. The stage plays the take as far as the run has
// come; where the audience is in it (the cursor, a beat) is the show's, kept
// by axx, so that the presenter's page and this one agree. The keys of a
// clicker work: right, page down, space and enter go on; left and page up go
// back. On a step that failed: r tries it once more, t plays on from the last
// take, 0 starts again from the top.
(() => {
	axxStage.present = (first) => {
		let data = first, cursor = -1, row = -1, t = 0, last = performance.now();
		const say = (word) => fetch('control/' + word, { method: 'POST' });
		const apply = async (d) => {
			await axxStage.update(d);
			data = d;
			// Another beat, or the same one of another row.
			if (d.live.cursor !== cursor || d.live.row !== row) {
				cursor = d.live.cursor;
				row = d.live.row;
				const beats = d.timeline.beats;
				t = beats.length ? beats[Math.min(cursor, beats.length - 1)] : 0;
			}
			// What plays again is marked, as what plays from a take is.
			const again = d.live.mode === 'live' && cursor < d.live.entered - 1;
			axxStage.mark(d.mark || (again ? 'again' : ''));
		};
		const tick = (now) => {
			const dt = (now - last) / 1000;
			last = now;
			const tl = data.timeline, beats = tl.beats;
			// The beat plays to where the next begins, or as far as the run has come.
			const until = cursor + 1 < beats.length ? beats[cursor + 1] - 0.001 : tl.duration;
			t = Math.min(until, t + dt);
			axxStage.seek(t);
			requestAnimationFrame(tick);
		};
		addEventListener('keydown', (e) => {
			if (['ArrowRight', 'PageDown', ' ', 'Enter'].includes(e.key)) { e.preventDefault(); say('forward'); }
			if (['ArrowLeft', 'PageUp'].includes(e.key)) { e.preventDefault(); say('back'); }
			if (e.key === 'r') say('retry');
			if (e.key === 't') say('take');
			if (e.key === '0') say('top');
		});
		const events = new EventSource('events');
		events.onmessage = async () => apply(await (await fetch('demo.json')).json());
		apply(first).then(() => requestAnimationFrame(tick));
	};
})();

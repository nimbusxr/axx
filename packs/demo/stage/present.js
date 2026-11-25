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
		// A line at the top of the picture while the audience waits for the
		// run: what the demo sets up, a step that takes its time, a run that
		// starts again. It shows when the stage has nothing left to play.
		const loading = document.createElement('div');
		loading.className = 'loading';
		loading.hidden = true;
		document.body.appendChild(loading);
		// The presenter's page, in a small window of its own. A browser lets a
		// page open a window only when the person asked for it, so where it
		// refuses, the page says which key does: p.
		const hint = document.createElement('div');
		hint.className = 'presenter-hint';
		hint.textContent = 'Press P for the presenter\u2019s page';
		hint.hidden = true;
		document.body.appendChild(hint);
		let presenter = null;
		const openPresenter = () => {
			if (presenter && !presenter.closed) { presenter.focus(); return true; }
			try { presenter = window.open('presenter', 'axx-presenter', 'popup=yes,width=520,height=760'); } catch (e) { presenter = null; }
			return !!presenter;
		};
		let asked = openPresenter();
		let ended = false;
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
			// The run works and the stage has played what there is of it.
			const live = data.live;
			const working = live.mode === 'restarting' || live.mode === 'live' && (!live.waiting || !!live.failure);
			loading.hidden = ended || !(working && t >= tl.duration - 0.01);
			// Until the show begins, how to get the presenter's page.
			hint.hidden = ended || asked || !(live.row === 0 && live.cursor === 0 && (live.mode === 'recorded' || live.mode === 'live' && live.entered <= 1));
			requestAnimationFrame(tick);
		};
		addEventListener('keydown', (e) => {
			if (['ArrowRight', 'PageDown', ' ', 'Enter'].includes(e.key)) { e.preventDefault(); say('forward'); }
			if (['ArrowLeft', 'PageUp'].includes(e.key)) { e.preventDefault(); say('back'); }
			if (e.key === 'p' || e.key === 'P') asked = openPresenter() || asked;
			// As a slide show ends: the show is over, for every page of it.
			if (e.key === 'q' || e.key === 'Q' || e.key === 'Escape') say('quit');
			if (e.key === 'r') say('retry');
			if (e.key === 't') say('take');
			if (e.key === '0') say('top');
		});
		// When the show is over (the presenter ended it, or Ctrl+C in the
		// terminal), its pages close: the presenter's window, and this one
		// where the browser lets a page close itself. Where it does not, the
		// page says that the show is over.
		const over = () => {
			if (ended) return;
			ended = true;
			events.close();
			if (presenter && !presenter.closed) presenter.close();
			loading.hidden = hint.hidden = true;
			window.close();
			setTimeout(() => {
				const card = document.createElement('div');
				card.className = 'over';
				card.textContent = 'The show is over. You can close this page.';
				document.body.appendChild(card);
			}, 400);
		};
		const events = new EventSource('events');
		events.onmessage = async () => { if (!ended) apply(await (await fetch('demo.json')).json()); };
		events.addEventListener('over', over);
		// A show that went without a word (its process was stopped) is over too.
		events.onerror = () => fetch('demo.json').catch(over);
		apply(first).then(() => requestAnimationFrame(tick));
	};
})();

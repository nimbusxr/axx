// The stage: the one page that plays a demo. It is given a take (what a run
// did) and a timeline (what the film shows when), and draws the picture of
// any moment: seek(t). A film is the stage, moment after moment; a live show
// is the stage in a window, its moment moved by a presenter.
//
// What it draws is made of scenes. The pack's own are registered as a
// project's are: axxStage.scene("diagram", {...}) replaces the diagram.
(() => {
	const scenes = {};
	const handlers = {};
	let data = null;
	let mounted = {};
	let lastSegment = -1;

	const emit = (name, detail) => (handlers[name] || []).forEach((fn) => fn(detail));

	// The state of a moment: everything a scene draws from.
	function stateAt(t) {
		const tl = data.timeline;
		t = Math.max(0, Math.min(t, tl.duration));
		let index = -1;
		for (let i = 0; i < tl.segments.length; i++) {
			if (tl.segments[i].start <= t) index = i;
		}
		const seg = index >= 0 ? tl.segments[index] : { kind: 'hold', step: -1, start: 0, end: 0 };
		const playing = seg.kind === 'step';
		const span = seg.end - seg.start;
		const progress = playing && span > 0 ? Math.min(1, (t - seg.start) / span) : 1;
		const subtitle = tl.subtitles.find((s) => s.start <= t && (t < s.end || s.end >= tl.duration));
		return {
			t, duration: tl.duration, segment: seg, segmentIndex: index,
			// step is the take's step that plays or was last played; done is how
			// many of the take's steps are over.
			step: seg.step >= 0 ? data.take.steps[seg.step] : null,
			stepIndex: seg.step,
			done: playing ? seg.step : seg.step + 1,
			playing, progress,
			// runTime is the moment of the run the picture is of, in the take's
			// seconds: a screen shows its picture of that moment.
			runTime: (seg.from || 0) + progress * ((seg.to || 0) - (seg.from || 0)),
			card: seg.kind === 'card' ? seg.text : null,
			subtitle: subtitle ? subtitle.text : null,
			show: seg.show && seg.show.length ? seg.show : ['diagram'],
			take: data.take, timeline: tl, theme: data.theme || {},
		};
	}

	function mount() {
		const root = document.getElementById('stage');
		root.innerHTML = '<div class="surfaces"></div><div class="ticker"></div><div class="subtitle"></div><div class="card"></div><div class="mark"></div>';
		mounted = {};
		lastSegment = -1;
		const theme = data.theme || {};
		for (const [k, v] of Object.entries(theme.colors || {})) root.style.setProperty('--' + k, v);
		if (theme.font) root.style.setProperty('--font', theme.font);
	}

	function surface(name, state) {
		const host = document.querySelector('#stage .surfaces');
		if (!mounted[name]) {
			const el = document.createElement('section');
			el.className = 'surface surface-' + name;
			host.appendChild(el);
			const scene = scenes[name] || scenes.missing;
			mounted[name] = { el, scene };
			if (scene.mount) scene.mount(el, state);
		}
		return mounted[name];
	}

	// What has to be there before a picture is drawn: the take's pictures of
	// its screens, and what each scene makes ready (the diagrams, read).
	async function prepare(d) {
		if (S.preload) await S.preload(d.take);
		for (const scene of Object.values(scenes)) if (scene.prepare) await scene.prepare(d);
	}

	const S = {
		// problems are what the stage could not make sense of in what it was
		// given: a film is not made of a stage that has any.
		problems: [],
		// scene registers what draws a surface ("diagram", "browser"), the
		// card or the subtitles: { mount(el, state), render(el, state) }.
		scene(name, def) { scenes[name] = def; },
		// on listens to what the playhead passes: "segment", "step",
		// "direction" (a project's own directions), "frame".
		on(name, fn) { (handlers[name] = handlers[name] || []).push(fn); },
		// load takes what the stage plays, and is done when its pictures are in.
		async load(d) {
			data = d;
			S.problems = [];
			mount();
			await prepare(d);
			S.seek(0);
		},
		// update takes what the stage plays as it grew: a show's take, as its
		// run goes on. The surfaces are made again when what the scenario
		// talks to changed.
		async update(d) {
			const key = (x) => JSON.stringify([x.take.peers, (x.live || {}).mode]);
			const again = !data || key(d) !== key(data);
			data = d;
			if (again) mount();
			await prepare(d);
		},
		// mark puts a word in the stage's corner, or takes it away.
		mark(word) { if (data) data.mark = word; },
		seek(t) {
			if (!data) return;
			const state = stateAt(t);
			S.state = state;
			const root = document.getElementById('stage');
			// The surfaces the moment shows, side by side; the others are hidden.
			for (const m of Object.values(mounted)) m.el.hidden = true;
			if (!state.card) {
				for (const name of state.show) {
					const m = surface(name, state);
					m.el.hidden = false;
					m.scene.render(m.el, state);
				}
			}
			root.dataset.surfaces = state.card ? '0' : String(state.show.length);
			(scenes.card || {}).render?.(root.querySelector('.card'), state);
			(scenes.subtitle || {}).render?.(root.querySelector('.subtitle'), state);
			(scenes.ticker || {}).render?.(root.querySelector('.ticker'), state);
			const mark = root.querySelector('.mark');
			mark.textContent = data.mark || '';
			mark.hidden = !data.mark;
			if (state.segmentIndex !== lastSegment) {
				const forward = state.segmentIndex > lastSegment;
				lastSegment = state.segmentIndex;
				emit('segment', state);
				if (forward && state.playing) {
					emit('step', state);
					for (const d of state.segment.directions || []) emit('direction', { ...d, state });
				}
			}
			emit('frame', state);
		},
		state: null,
	};
	window.axxStage = S;

	const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
	S.escape = esc;

	S.scene('card', {
		render(el, state) {
			el.hidden = !state.card;
			if (state.card && el.dataset.text !== state.card) {
				el.dataset.text = state.card;
				el.innerHTML = '<p>' + esc(state.card) + '</p>';
			}
		},
	});

	S.scene('subtitle', {
		render(el, state) {
			const text = state.card ? null : state.subtitle;
			el.hidden = !text;
			if (text && el.dataset.text !== text) {
				el.dataset.text = text;
				el.innerHTML = '<p>' + esc(text) + '</p>';
			}
		},
	});

	// The ticker says which step the picture is of, in the feature's words:
	// what the audience sees is a step that ran.
	S.scene('ticker', {
		render(el, state) {
			const st = state.step;
			el.hidden = !st || !!state.card || state.theme.steps === false;
			if (!st) return;
			const over = !state.playing || state.progress >= 1;
			const sign = !over ? '›' : st.status === 'passed' ? '✓' : '✕';
			const cls = !over ? 'running' : st.status === 'passed' ? 'passed' : 'failed';
			el.innerHTML = '<span class="' + cls + '">' + sign + '</span> <b>' + esc(st.keyword) + '</b> ' + esc(st.text);
		},
	});

	// A surface the stage has no scene for yet says so, rather than show nothing.
	S.scene('missing', {
		render(el, state) {
			el.innerHTML = '<p class="missing">No picture of this surface in the take.</p>';
		},
	});
})();

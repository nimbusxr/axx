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

	// What a step did: each thing it sent or saw.
	const acts = (step) => (step.exchanges || []).filter((x) => x.sent || x.seen);
	// What the scenario looks at, rather than calls: what it finds there got
	// there by a line of the system's own.
	const looked = (kind) => ['database', 'topic', 'broker', 'mock'].includes(kind);

	// What is happening at a moment: the exchange that plays, how far along it
	// is, and what of it shows. Every scene draws from this, so that the film's
	// rules are the same on a diagram, in a terminal and in a project's own
	// scene: an exchange goes out, comes back, and stays to be read; a step
	// whose response is held ([hold response]) shows what it sent, and what
	// came back shows with the step after it.
	function happening(state) {
		const take = state.take, segs = state.timeline.segments;
		let hold = null;
		for (let i = state.segmentIndex; i >= 0; i--) {
			if (segs[i].kind !== 'step') continue;
			const here = i === state.segmentIndex;
			const after = segs.findIndex((s, j) => j > i && s.kind === 'step');
			// Before an audience the step after it has not run yet.
			const open = !!data.live && (data.live.mode === 'live' || data.live.mode === 'restarting');
			if (segs[i].holdResponse && (after >= 0 || open)) {
				hold = { step: segs[i].step, stage: here ? 'out' : 'waiting' };
			} else if (here) {
				// The step after a held one, when it sends nothing of its own.
				let before = -1;
				for (let j = i - 1; j >= 0 && before < 0; j--) if (segs[j].kind === 'step') before = j;
				if (before >= 0 && segs[before].holdResponse && !acts(state.step).length) hold = { step: segs[before].step, stage: 'reveal' };
			}
			break;
		}
		const held = hold ? acts(take.steps[hold.step]) : [];
		// The exchange whose answer is held: the step's last that was sent and answered.
		const kept = [...held].reverse().find((x) => x.sent && x.seen) || null;
		let now = null;
		if (state.playing && state.step) {
			const list = acts(state.step);
			if (list.length) {
				const at = Math.min(list.length - 1e-9, state.progress * list.length);
				const k = Math.floor(at);
				now = { exchange: list[k], phase: at - k, failed: state.step.status !== 'passed', list, step: state.stepIndex };
				// Out, and no further for now.
				if (hold && hold.stage === 'out' && list[k] === kept) now.phase *= 0.34;
			}
		}
		if (kept && hold.stage !== 'out') {
			// Waiting for the step after it, or, in that step, coming back.
			const phase = hold.stage === 'waiting' ? 0.34 : 0.36 + 0.64 * state.progress;
			now = { exchange: kept, phase, failed: take.steps[hold.step].status !== 'passed', list: held, step: hold.step, held: hold.step };
		}
		if (!now) {
			// A step that sent nothing (a check of what came back) leaves the
			// last thing that was sent and seen on the stage: it is what the
			// step is about.
			// Of the steps the film showed: what a step did with the camera off
			// ([cut]) the audience never saw.
			const upTo = state.playing ? state.stepIndex : state.done - 1;
			for (let i = Math.min(upTo, take.steps.length - 1); i >= 0 && !now; i--) {
				if (!state.filmed.has(i)) continue;
				const list = acts(take.steps[i]);
				if (list.length) now = { exchange: list[list.length - 1], phase: 1, failed: take.steps[i].status !== 'passed', over: true, list, step: i };
			}
		}
		now = now || { exchange: null, phase: 0, list: [] };
		const x = now.exchange;
		now.kept = kept;
		now.answered = !kept || (hold.stage === 'reveal' && now.phase >= 0.62);
		// What the scenario found there, rather than asked for.
		now.found = !!x && (!!x.from || looked(x.kind));
		// What of the exchange shows by now.
		now.sent = x && x.sent && (now.over || now.phase > 0.05) ? x.sent : null;
		now.seen = x && x.seen && (now.over || now.phase >= (now.found ? 0.38 : x.sent ? 0.62 : 0.36)) ? x.seen : null;
		return now;
	}

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
		const state = {
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
		// The steps the film has shown by now: those with time of their own in it.
		state.filmed = new Set(tl.segments.filter((s) => s.kind === 'step' && s.start <= t).map((s) => s.step));
		state.now = happening(state);
		return state;
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
		// What a cue shows is a surface some scene draws.
		const said = new Set();
		for (const seg of d.timeline.segments) {
			for (const name of seg.show || []) {
				if (scenes[name] || said.has(name)) continue;
				said.add(name);
				S.problems.push('[show ' + name + ']: no scene draws ' + name + ' (a script registers one with axxStage.scene("' + name + '", ...))');
			}
		}
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
			// What shows, for a stylesheet: a phone is narrow, and what stands
			// beside it has the room it leaves.
			root.dataset.show = state.card ? '' : state.show.join(' ');
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
	S.acts = acts;
	S.looked = looked;
	// A message's body as a few lines to read: JSON laid out, long lines and
	// long bodies cut.
	S.pretty = (m, lines = 9, width = 56) => {
		if (!m || !m.body) return '';
		let body = m.body;
		if ((m.mediaType || '').includes('json') || /^\s*[[{]/.test(body)) {
			try { body = JSON.stringify(JSON.parse(body), null, 2); } catch (e) { /* as it is */ }
		}
		const all = body.split('\n');
		const shown = all.slice(0, lines).map((l) => (l.length > width ? l.slice(0, width - 1) + '…' : l));
		if (all.length > lines) shown.push('…');
		return shown.join('\n');
	};

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
			// Only a step the film showed: while the picture holds on what the
			// camera was off for (an introduction, say), there is no step to name.
			const st = state.step;
			const shown = st && state.timeline.segments.some((s) => s.kind === 'step' && s.step === state.stepIndex);
			el.hidden = !shown || !!state.card || state.theme.steps === false;
			if (!shown) return;
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

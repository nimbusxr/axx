// The terminal: a demo's steps as someone would type them. A request is the
// curl command that makes it, typed out, and what came back is what curl
// would print; what the scenario found in a database or a topic is a line
// that says where, and what was there.
//
// It is a module of the stage like any other, and the example of one: a scene
// registered under a name of its own, [show terminal], that draws what the
// stage says is happening (state.now) and what happened before. Nothing here
// keeps a clock: the picture of a moment is made from the state of that
// moment, so the film can ask for any moment in any order.
(() => {
	const { acts, escape, pretty } = axxStage;

	// An exchange as a command, in lines.
	function command(x) {
		const request = x.sent && /^([A-Z]+) (\S+)$/.exec(x.sent.title);
		if (x.kind === 'service' && request && !x.from) {
			const [, method, path] = request;
			const lines = ['curl ' + (method === 'GET' ? '' : '-X ' + method + ' ') + quote((x.address || 'http://' + x.peer) + path)];
			if (x.sent.body) {
				lines.push("-H 'Content-Type: " + (x.sent.mediaType || 'application/json').split(';')[0] + "'");
				lines.push('-d ' + quote(compact(x.sent.body)));
			}
			return lines.map((l, i) => (i ? '    ' : '') + l + (i < lines.length - 1 ? ' \\' : ''));
		}
		// What the scenario looked at, or a call a mock received: a remark.
		const where = x.from ? (x.from === 'system' ? '' : x.from + ' → ') + x.peer : x.peer;
		return ['# ' + where + (x.sent ? ': ' + x.sent.title : '')];
	}
	const quote = (s) => "'" + String(s).replace(/'/g, "'\\''") + "'";
	function compact(body) {
		let text = body;
		try { text = JSON.stringify(JSON.parse(body)); } catch (e) { text = body.replace(/\s+/g, ' '); }
		return text.length > 160 ? text.slice(0, 159) + '…' : text;
	}

	// What came back, in lines.
	function answer(x) {
		if (!x.seen) return [];
		const first = x.kind === 'service' && /^\d{3}\b/.test(x.seen.title) ? 'HTTP/1.1 ' + x.seen.title : x.seen.title;
		const body = pretty(x.seen, 8, 72);
		return [first].concat(body ? body.split('\n') : []);
	}

	axxStage.scene('terminal', {
		mount(el, state) {
			el.innerHTML = '<div class="term"><div class="term-bar"><i></i><i></i><i></i><span></span></div><div class="term-body"><pre class="term-lines"></pre></div></div>';
			el.querySelector('.term-bar span').textContent = state.theme.caller || 'demo';
		},
		render(el, state) {
			const take = state.take, now = state.now;
			const out = [];
			const prompt = '<span class="t-prompt">$</span> ';
			const typed = (lines, chars) => {
				// The command as far as it is typed, with the cursor after it.
				let left = chars;
				lines.forEach((line, i) => {
					if (left <= 0 && i > 0) return;
					const shown = line.slice(0, Math.max(0, left));
					left -= line.length;
					const cursor = left <= 0 ? '<span class="t-cursor"> </span>' : '';
					const cls = line.startsWith('#') ? 't-remark' : 't-command';
					out.push((i ? '' : prompt) + '<span class="' + cls + '">' + escape(shown) + '</span>' + cursor);
				});
			};
			const whole = (lines) => lines.forEach((line, i) => out.push((i ? '' : prompt) + '<span class="' + (line.startsWith('#') ? 't-remark' : 't-command') + '">' + escape(line) + '</span>'));
			const printed = (x, failed) => answer(x).forEach((line, i) => out.push('<span class="' + (i ? 't-body' : failed ? 't-failed' : 't-status') + '">' + escape(line) + '</span>'));
			// Every step the film has shown, in order: what was typed, and what
			// came back; then the exchange that plays, as far as it has got.
			for (const seg of state.timeline.segments) {
				if (seg.kind !== 'step' || seg.start > state.t) continue;
				const step = take.steps[seg.step];
				for (const x of acts(step)) {
					if (x === now.exchange && !now.over) {
						const lines = command(x), length = lines.reduce((n, l) => n + l.length, 0);
						// Typed while the exchange goes out; what came back when it shows.
						typed(lines, x.sent ? Math.ceil(length * Math.min(1, now.phase / 0.3)) : length);
						if (now.seen) printed(x, now.failed);
						break;
					}
					// One whose time has not come within the step that plays.
					if (seg.step === now.step && !now.over && now.list.indexOf(x) > now.list.indexOf(now.exchange)) break;
					whole(command(x));
					// What came back, unless it is still held for the step after.
					if (x !== now.kept || now.answered) printed(x, step.status !== 'passed');
					out.push('');
				}
			}
			if (!out.length || out[out.length - 1] === '') out.push(prompt + '<span class="t-cursor"> </span>');
			// The last lines: a terminal scrolls.
			const rows = 22;
			el.querySelector('.term-lines').innerHTML = out.slice(-rows).join('\n');
		},
	});
})();

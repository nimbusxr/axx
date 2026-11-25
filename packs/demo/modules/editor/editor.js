// The editor: a file a step read, as an editor shows it, with the colors of
// what it is. A feature file is colored as axx's editor plugins color it, and
// a demo's cues stand out in it: the words in square brackets as directions,
// the rest as what the audience reads.
//
// It is a module of the stage like the terminal, built on the same things: a
// scene of a name of its own, [show editor], that draws from the state of a
// moment. A step shows a file by reading it (the files pack's check that a
// file contains a text, and the cloud packs' of an object).
(() => {
	const { acts, escape } = axxStage;
	// What holds files: a folder, a bucket, a container.
	const holdsFiles = (kind) => /folder|bucket|container/.test(kind || '');
	const span = (is, text) => '<span class="e-' + is + '">' + escape(text) + '</span>';

	// A step's text: its quoted values, the <names> of an outline's columns.
	function stepText(text) {
		let out = '', rest = text;
		const next = /("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|<[^<>\s][^<>]*>)/;
		for (let m = next.exec(rest); m; m = next.exec(rest)) {
			out += escape(rest.slice(0, m.index)) + span(m[0][0] === '<' ? 'name' : 'string', m[0]);
			rest = rest.slice(m.index + m[0].length);
		}
		return out + escape(rest);
	}

	// A line of a feature file, colored.
	function gherkin(line, doc) {
		const indent = /^\s*/.exec(line)[0], text = line.slice(indent.length);
		let m;
		if (text.startsWith('"""') || text.startsWith('```')) { doc.open = !doc.open; return escape(indent) + span('string', text); }
		if (doc.open) return escape(indent) + span('string', text);
		if ((m = /^(#\s*demo:)(.*)$/.exec(text))) {
			// A cue: its directions, and what the audience reads.
			const parts = m[2].split(/(\[[^\]]*\])/).map((p) => (p.startsWith('[') ? span('direction', p) : span('cue', p))).join('');
			return escape(indent) + span('cue-mark', m[1]) + parts;
		}
		if (doc.cue && /^#\s{6,}\S/.test(text)) return escape(indent) + span('cue-mark', '#') + span('cue', text.slice(1));
		if (text.startsWith('#')) return escape(indent) + span('comment', text);
		if (text.startsWith('@')) return escape(indent) + span('tag', text);
		if ((m = /^(Feature|Rule|Background|Scenario Outline|Scenario Template|Scenario|Example|Examples|Scenarios)(:)(.*)$/.exec(text))) {
			return escape(indent) + span('keyword', m[1]) + escape(m[2]) + span('title', m[3]);
		}
		if ((m = /^(Given|When|Then|And|But|\*)( )(.*)$/.exec(text))) return escape(indent) + span('step', m[1]) + m[2] + stepText(m[3]);
		if (text.startsWith('|')) return escape(indent) + text.split(/(\|)/).map((p) => (p === '|' ? span('pipe', p) : stepText(p))).join('');
		return escape(indent) + span('prose', text);
	}

	// A file's lines, colored by its name.
	function colored(name, body) {
		const lines = body.split('\n');
		if (!/\.feature$/.test(name)) return lines.map((l) => escape(l));
		const doc = { open: false, cue: false };
		return lines.map((l) => {
			const html = gherkin(l, doc);
			// A comment right under a cue, indented to its text (past "demo:"), goes on with it.
			doc.cue = /^\s*#\s*demo:/.test(l) || doc.cue && /^\s*#\s{6,}\S/.test(l);
			return html;
		});
	}

	axxStage.scene('editor', {
		mount(el) {
			el.innerHTML = '<div class="ed"><div class="ed-bar"><i></i><i></i><i></i><span class="ed-tab"></span><span class="ed-path"></span></div><div class="ed-body"><div class="ed-lines"></div></div></div>';
		},
		render(el, state) {
			// The last file a step the video has shown read, and how far that
			// step has played.
			let file = null, progress = 1;
			for (const seg of state.timeline.segments) {
				if (seg.kind !== 'step' || seg.start > state.t) continue;
				for (const x of acts(state.take.steps[seg.step])) {
					if (!holdsFiles(x.kind) || !x.seen || !x.seen.body) continue;
					file = x;
					progress = seg.end > seg.start ? Math.min(1, (state.t - seg.start) / (seg.end - seg.start)) : 1;
				}
			}
			const ed = el.querySelector('.ed');
			ed.hidden = !file;
			if (!file) return;
			const name = file.seen.title, lines = colored(name, file.seen.body);
			el.querySelector('.ed-tab').textContent = name.split('/').pop();
			el.querySelector('.ed-path').textContent = name.includes('/') ? name.slice(0, name.lastIndexOf('/')) : '';
			// Its letters are as large as lets the file show whole, down to a
			// size that still reads; a longer file scrolls as its step plays.
			const fits = 34, shown = Math.min(lines.length, fits);
			ed.style.setProperty('--lines', String(Math.max(18, shown)));
			const width = String(lines.length).length;
			const first = lines.length > fits ? Math.round((lines.length - fits) * Math.min(1, Math.max(0, (progress - 0.15) / 0.7))) : 0;
			el.querySelector('.ed-lines').innerHTML = lines.slice(first, first + fits).map((html, i) =>
				'<div class="ed-line"><span class="ed-no">' + String(first + i + 1).padStart(width, ' ') + '</span><span class="ed-code">' + (html || ' ') + '</span></div>').join('');
		},
	});
})();

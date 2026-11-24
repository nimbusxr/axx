// The diagram: what a scenario with no screen looks like. The scenario's
// caller on the left, what it talks to as boxes, and for each step the
// message that went out along a line and what came back. It shows only what
// the run sent and saw: axx looks at the system from outside.
//
// It is drawn in characters on a grid, with the engine of diagrams drawn the
// same way elsewhere: boxes, lines that turn at right angles and join where
// they meet, and a brighter band that runs along a line that is in use.
(() => {
	const RULES = {
		light: { tl: '┌', tr: '┐', bl: '└', br: '┘', h: '─', v: '│' },
		heavy: { tl: '╔', tr: '╗', bl: '╚', br: '╝', h: '═', v: '║' },
		zone: { tl: '┌', tr: '┐', bl: '└', br: '┘', h: '╌', v: '╎' },
	};
	const N = 1, E = 2, S = 4, W = 8;
	const side = (dx, dy) => (dx > 0 ? E : dx < 0 ? W : dy > 0 ? S : N);
	const JOIN = {
		[N]: '│', [S]: '│', [N | S]: '│', [E]: '─', [W]: '─', [E | W]: '─',
		[E | S]: '┌', [S | W]: '┐', [N | E]: '└', [N | W]: '┘',
		[N | E | S]: '├', [N | S | W]: '┤', [E | S | W]: '┬', [N | E | W]: '┴', [N | E | S | W]: '┼',
	};
	const TEE = { '─s': '┬', '─n': '┴', '│e': '├', '│w': '┤', '═s': '╤', '═n': '╧', '║e': '╟', '║w': '╢', '╌s': '┬', '╌n': '┴', '╎e': '├', '╎w': '┤' };

	const newGrid = (w, h) => Array.from({ length: h }, () => Array.from({ length: w }, () => ({ ch: ' ', k: '' })));
	function put(grid, x, y, ch, k) {
		if (y < 0 || y >= grid.length || x < 0 || x >= grid[0].length) return;
		grid[y][x] = { ch, k };
	}
	function write(grid, x, y, text, k) {
		[...text].forEach((ch, i) => put(grid, x + i, y, ch, k));
	}
	function drawBox(grid, b, kind) {
		const r = RULES[b.style || 'light'];
		const right = b.x + b.w - 1, bottom = b.y + b.h - 1;
		for (let x = b.x; x <= right; x++) { put(grid, x, b.y, r.h, kind); put(grid, x, bottom, r.h, kind); }
		for (let y = b.y; y <= bottom; y++) { put(grid, b.x, y, r.v, kind); put(grid, right, y, r.v, kind); }
		put(grid, b.x, b.y, r.tl, kind); put(grid, right, b.y, r.tr, kind);
		put(grid, b.x, bottom, r.bl, kind); put(grid, right, bottom, r.br, kind);
		if (b.title) write(grid, b.x + 2, b.y, ' ' + b.title + ' ', kind === 'dim' ? 'dim' : 'title');
		(b.lines || []).forEach((line, i) => write(grid, b.x + 2, b.y + 1 + i, line.text, kind === 'dim' ? 'dim' : line.kind || 'sub'));
	}
	// Every cell of a path of corners, each with the way it is entered and left.
	function cells(path) {
		const out = [];
		let inDx = 0, inDy = 0;
		for (let i = 0; i < path.length - 1; i++) {
			const [x0, y0] = path[i], [x1, y1] = path[i + 1];
			const dx = Math.sign(x1 - x0), dy = Math.sign(y1 - y0);
			let x = x0, y = y0;
			if (!out.length) { inDx = dx; inDy = dy; }
			while (x !== x1 || y !== y1) {
				out.push({ x, y, dx, dy, inDx, inDy });
				inDx = dx; inDy = dy;
				x += dx; y += dy;
			}
		}
		const [lx, ly] = path[path.length - 1];
		const last = out[out.length - 1] || { dx: 1, dy: 0 };
		out.push({ x: lx, y: ly, dx: last.dx, dy: last.dy, inDx: last.dx, inDy: last.dy });
		return out;
	}
	// An edge, and on a lit one how far each cell is behind the band's head.
	function drawEdge(grid, e, kind, head) {
		const path = e.cells;
		path.forEach((c, i) => {
			const last = i === path.length - 1;
			if (last) {
				put(grid, c.x, c.y, c.dx > 0 ? '>' : c.dx < 0 ? '<' : c.dy > 0 ? 'v' : '^', kind);
			} else {
				let conn = side(-c.inDx, -c.inDy) | side(c.dx, c.dy);
				const under = grid[c.y] && grid[c.y][c.x];
				if (under && under.conn) conn |= under.conn;
				put(grid, c.x, c.y, JOIN[conn] || '┼', kind);
				grid[c.y][c.x].conn = conn;
			}
			if (head !== undefined && grid[c.y] && grid[c.y][c.x]) {
				const behind = Math.abs(head - i);
				if (behind < 5) grid[c.y][c.x].hot = Math.floor(behind);
			}
		});
		const first = path[0];
		const bx = first.x - first.dx, by = first.y - first.dy;
		const rule = grid[by] && grid[by][bx];
		const dir = first.dx > 0 ? 'e' : first.dx < 0 ? 'w' : first.dy > 0 ? 's' : 'n';
		if (rule && TEE[rule.ch + dir]) put(grid, bx, by, TEE[rule.ch + dir], rule.k);
	}
	const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
	function html(grid) {
		return grid.map((row) => {
			let out = '', run = '', k = '';
			const flush = () => { if (run) out += k ? '<span class="k-' + k + '">' + esc(run) + '</span>' : esc(run); run = ''; };
			for (const c of row) {
				if (c.hot !== undefined && c.k) {
					flush(); k = '';
					out += '<span class="k-' + c.k + ' hot hot' + c.hot + '">' + esc(c.ch) + '</span>';
					continue;
				}
				if (c.k !== k) { flush(); k = c.k; }
				run += c.ch;
			}
			flush();
			return out.replace(/\s+$/, '');
		}).join('\n');
	}

	// The layout: the caller, then what the scenario talks to in a column,
	// then the mocks, which the system calls, in a column of their own.
	const BW = 28, BH = 4, GAP = 1, CALLER_W = 16;
	function layout(take, theme) {
		// Only what the scenario sent something to or saw something of: a
		// broker whose topics do the talking would be a box nothing happens at.
		const spoke = new Set();
		for (const st of take.steps) for (const x of acts(st)) spoke.add(x.peer);
		const peers = take.peers.filter((p) => spoke.has(p.name));
		const mid = peers.filter((p) => p.kind !== 'mock');
		const mocks = peers.filter((p) => p.kind === 'mock');
		const rows = Math.max(mid.length, mocks.length, 1);
		const height = rows * (BH + GAP) - GAP + 2;
		const midX = CALLER_W + 18, mockX = midX + BW + 16;
		const width = (mocks.length ? mockX + BW : midX + BW) + 1;
		const boxes = {}, edges = {};
		const column = (list, x) => {
			const y0 = Math.floor((height - (list.length * (BH + GAP) - GAP)) / 2);
			list.forEach((p, i) => {
				boxes[p.name] = { id: p.name, x, y: y0 + i * (BH + GAP), w: BW, h: BH, title: clip(p.name, BW - 6), kind: p.kind, peer: p };
			});
		};
		column(mid, midX);
		column(mocks, mockX);
		const cy = Math.floor(height / 2);
		const caller = { id: '@caller', x: 0, y: cy - 2, w: CALLER_W, h: 4, title: clip(theme.caller || 'axx', CALLER_W - 6), style: 'heavy' };
		const trunk = CALLER_W + 8;
		for (const p of mid) {
			const b = boxes[p.name], py = b.y + 1;
			edges['c>' + p.name] = { cells: cells(dedupe([[CALLER_W, cy - 1], [trunk, cy - 1], [trunk, py], [b.x - 1, py]])) };
		}
		const service = mid.find((p) => p.kind === 'service');
		for (const p of mocks) {
			const b = boxes[p.name], py = b.y + 1;
			if (service) {
				const s = boxes[service.name], sy = s.y + 2, tx = midX + BW + 7;
				edges['s>' + p.name] = { cells: cells(dedupe([[s.x + BW, sy], [tx, sy], [tx, py], [b.x - 1, py]])) };
			}
		}
		return { width, height, boxes, edges, caller, service: service ? service.name : null };
	}
	const dedupe = (path) => path.filter((p, i) => i === 0 || p[0] !== path[i - 1][0] || p[1] !== path[i - 1][1]);
	const clip = (s, n) => (s.length > n ? s.slice(0, n - 1) + '…' : s);

	// What a step did, as the stretches of its time: each thing it sent or
	// saw goes out along its line and comes back.
	function acts(step) {
		return (step.exchanges || []).filter((x) => x.sent || x.seen);
	}
	const edgeOf = (x) => (x.from ? 's>' : 'c>') + x.peer;

	function pretty(m) {
		if (!m || !m.body) return '';
		let body = m.body;
		if ((m.mediaType || '').includes('json') || /^\s*[[{]/.test(body)) {
			try { body = JSON.stringify(JSON.parse(body), null, 2); } catch (e) { /* as it is */ }
		}
		const lines = body.split('\n');
		const shown = lines.slice(0, 9).map((l) => clip(l, 56));
		if (lines.length > 9) shown.push('…');
		return shown.join('\n');
	}

	axxStage.scene('diagram', {
		mount(el, state) {
			el.innerHTML = '<pre class="grid"></pre><div class="messages"><div class="msg sent"></div><div class="msg seen"></div></div>';
			el._layout = layout(state.take, state.theme);
			// The grid is as large as fits: a diagram of many boxes is drawn smaller.
			el.style.setProperty('--rows', el._layout.height);
			el.style.setProperty('--cols', el._layout.width);
		},
		render(el, state) {
			const L = el._layout, take = state.take;
			const grid = newGrid(L.width, L.height);
			// Which peers the scenario has met by now, and what each last said.
			const met = {}, lastSaid = {};
			const upTo = state.playing ? state.stepIndex : state.done - 1;
			for (let i = 0; i <= upTo && i < take.steps.length; i++) {
				const current = state.playing && i === state.stepIndex;
				for (const x of take.steps[i].exchanges || []) {
					met[x.peer] = true;
					if (!current && x.seen) lastSaid[x.peer] = { text: x.seen.title, kind: 'ok' };
					else if (!current && x.sent && !x.from) lastSaid[x.peer] = { text: x.sent.title, kind: 'coord' };
				}
			}
			// The step that plays: which of its exchanges, and how far along.
			let active = null;
			if (state.playing && state.step) {
				const list = acts(state.step);
				if (list.length) {
					const at = Math.min(list.length - 1e-9, state.progress * list.length);
					const k = Math.floor(at);
					active = { x: list[k], phase: at - k, failed: state.step.status !== 'passed' };
				}
			} else if (state.step) {
				const list = acts(state.step);
				if (list.length) active = { x: list[list.length - 1], phase: 1, failed: state.step.status !== 'passed', over: true };
			}
			drawBox(grid, L.caller, 'key');
			for (const b of Object.values(L.boxes)) {
				if (!met[b.id]) continue;
				const said = lastSaid[b.id];
				const lines = [{ text: b.kind, kind: 'sub' }];
				if (said) lines.push({ text: clip(said.text, BW - 4), kind: said.kind });
				const on = active && active.x.peer === b.id && !active.over;
				drawBox(grid, { ...b, lines }, on ? (active.failed ? 'fail' : 'key') : 'line');
			}
			for (const [id, e] of Object.entries(L.edges)) {
				const peer = id.slice(2);
				if (!met[peer]) continue;
				if (active && !active.over && edgeOf(active.x) === id) continue;
				drawEdge(grid, e, 'line');
			}
			let sent = null, seen = null;
			if (active) {
				const e = L.edges[edgeOf(active.x)];
				const x = active.x, p = active.phase;
				// Out in the first part of its time, back in the next, then it rests.
				const out = Math.min(1, p / 0.4), back = Math.min(1, Math.max(0, (p - 0.45) / 0.4));
				const going = x.sent && p < 0.45, coming = x.seen && (x.sent ? p >= 0.45 && p < 0.9 : p < 0.5);
				if (e && !active.over) {
					const n = e.cells.length - 1;
					let head, kind = 'coord';
					if (going) head = out * n;
					else if (coming) { head = (1 - (x.sent ? back : Math.min(1, p / 0.45))) * n; kind = active.failed ? 'fail' : 'ok'; }
					drawEdge(grid, e, kind, head);
				}
				if (x.sent && (active.over || p > 0.05)) sent = x.sent;
				if (x.seen && (active.over || (x.sent ? p >= 0.85 : p >= 0.45))) seen = x.seen;
				el._peer = x.peer;
			}
			el.querySelector('.grid').innerHTML = html(grid);
			// A mock knows the calls it got, not who made them: where the scenario
			// talks to one service, it was that one.
			const from = active && active.x.from ? (active.x.from === 'system' && L.service ? L.service : active.x.from) : '';
			message(el.querySelector('.msg.sent'), sent, from ? from + ' → ' + active.x.peer : '→ ' + (active ? active.x.peer : ''));
			message(el.querySelector('.msg.seen'), seen, '← ' + (active ? active.x.peer : ''), active && active.failed);
		},
	});

	function message(el, m, where, failed) {
		el.classList.toggle('on', !!m);
		el.classList.toggle('failed', !!failed);
		if (!m) return;
		const key = where + '|' + m.title + '|' + (m.body || '').length;
		if (el.dataset.key === key) return;
		el.dataset.key = key;
		const body = pretty(m);
		el.innerHTML = '<div class="where">' + esc(where) + '</div><div class="title">' + esc(m.title) + '</div>' + (body ? '<pre>' + esc(body) + '</pre>' : '');
	}
})();

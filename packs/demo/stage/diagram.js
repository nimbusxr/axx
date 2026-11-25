// The diagram: what a scenario with no screen looks like. The system as
// boxes and the lines between them, and for each step what went along a
// line and what came back. It shows what the run sent and saw, on a map of
// the system: one of the project's diagrams, written in Mermaid, or, without
// any, what the scenario talked to.
//
// The map is drawn in characters on a grid, by the engine below: boxes, lines
// that turn at right angles and join where they meet, and a brighter band
// that runs along a line that is in use. Mermaid's library only reads a
// diagram's text (its boxes, lines and groups): nothing of its drawing is
// used. Every map is laid out from left to right, whatever way its text says.
(() => {
	// --- The engine: characters on a grid.
	const RULES = {
		light: { tl: '┌', tr: '┐', bl: '└', br: '┘', h: '─', v: '│' },
		round: { tl: '╭', tr: '╮', bl: '╰', br: '╯', h: '─', v: '│' },
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
	// How a line that is dotted or thick in the map is drawn, where it runs alone.
	const STROKE = { dotted: { '─': '╌', '│': '╎' }, thick: { '─': '━', '│': '┃', '┌': '┏', '┐': '┓', '└': '┗', '┘': '┛' } };
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
		if (b.title) write(grid, b.x + 2, b.y, ' ' + b.title + ' ', kind === 'dim' ? 'dim' : b.style === 'zone' ? 'sub' : 'title');
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
		const path = e.cells, stroke = STROKE[e.stroke] || {};
		path.forEach((c, i) => {
			const last = i === path.length - 1;
			if (last) {
				put(grid, c.x, c.y, c.dx > 0 ? '>' : c.dx < 0 ? '<' : c.dy > 0 ? 'v' : '^', kind);
			} else {
				let conn = side(-c.inDx, -c.inDy) | side(c.dx, c.dy);
				const under = grid[c.y] && grid[c.y][c.x];
				const shared = under && under.conn;
				if (shared) conn |= under.conn;
				const ch = JOIN[conn] || '┼';
				put(grid, c.x, c.y, shared ? ch : stroke[ch] || ch, kind);
				grid[c.y][c.x].conn = conn;
			}
			if (head !== undefined && grid[c.y] && grid[c.y][c.x]) {
				const behind = Math.abs(head - i);
				if (behind < 5) grid[c.y][c.x].hot = Math.floor(behind);
			}
		});
		if (e.label && path.length > e.label.length + 4) {
			// What the map says of the line, on its first stretch.
			const c = path[1];
			if (c.dy === 0) write(grid, Math.min(c.x, c.x + c.dx * (e.label.length + 1)) + 1, c.y - 1, e.label, kind === 'line' ? 'sub' : kind);
		}
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
	const dedupe = (path) => path.filter((p, i) => i === 0 || p[0] !== path[i - 1][0] || p[1] !== path[i - 1][1]);
	const clip = (s, n) => (s.length > n ? s.slice(0, n - 1) + '…' : s);

	// --- The map: boxes, the lines between them, and groups of boxes.
	// OUTSIDE is where what the scenario sends comes from when the map names
	// nobody who uses the system: the edge of the picture.
	const OUTSIDE = '@outside';
	const SHAPES = { cylinder: 'database', hexagon: 'topic', stadium: '', round: '', circle: '', subroutine: '' };
	const STYLES = { stadium: 'round', round: 'round', circle: 'round', cylinder: 'round', subroutine: 'heavy' };

	// What a step did, as the stretches of its time: each thing it sent or saw.
	const acts = (step) => (step.exchanges || []).filter((x) => x.sent || x.seen);
	// What the scenario looks at, rather than calls: what it finds there got
	// there by a line of the system's own.
	const looked = (kind) => ['database', 'topic', 'broker', 'mock'].includes(kind);

	// The map of a take with no diagram: what the scenario talked to, with
	// what it sent coming from who uses the system, or from outside.
	function mapOf(take, theme) {
		const caller = theme.caller || OUTSIDE;
		const map = { name: '', nodes: [], edges: [], groups: [], caller, made: true };
		if (theme.caller) map.nodes.push({ id: caller, label: caller, shape: 'stadium' });
		const spoke = [];
		for (const st of take.steps) for (const x of acts(st)) if (!spoke.includes(x.peer)) spoke.push(x.peer);
		const peers = take.peers.filter((p) => spoke.includes(p.name));
		const service = peers.find((p) => p.kind === 'service');
		for (const p of peers) {
			map.nodes.push({ id: p.name, label: p.name, shape: p.kind === 'mock' ? 'round' : null });
			if (p.kind === 'mock' && service) map.edges.push({ from: service.name, to: p.name });
			// A topic's line is dotted, as an event's is: sent, and read later.
			else if (p.kind !== 'mock') map.edges.push({ from: caller, to: p.name, stroke: p.kind === 'topic' ? 'dotted' : '' });
		}
		return map;
	}

	// A project's diagram, read by Mermaid's parser.
	async function read(diagram) {
		const parsed = await mermaid.mermaidAPI.getDiagramFromText(diagram.source);
		const db = parsed.db;
		if (!db || !db.getVertices || !db.getEdges) throw new Error('it is a ' + parsed.type + ', and a demo\'s diagram is a flowchart');
		const vertices = db.getVertices();
		const map = { name: diagram.name, nodes: [], edges: [], groups: [], caller: null };
		for (const v of vertices.values ? vertices.values() : Object.values(vertices)) {
			map.nodes.push({ id: v.id, label: String(v.text || v.id).replace(/<[^>]*>/g, ' ').trim(), shape: v.type || null });
		}
		for (const e of db.getEdges()) map.edges.push({ from: e.start, to: e.end, stroke: e.stroke === 'normal' ? '' : e.stroke, label: String(e.text || '').trim() });
		for (const g of db.getSubGraphs ? db.getSubGraphs() : []) map.groups.push({ id: g.id, label: g.title || g.id, nodes: g.nodes || [] });
		return map;
	}

	// The layout: the boxes in columns from left to right, each a line's
	// length after those that lead to it, and the lines between them.
	const BH = 4, GAP = 2, CHANNEL = 12;
	function layout(map) {
		const ids = map.nodes.map((n) => n.id);
		const outside = map.edges.some((e) => e.from === OUTSIDE);
		const real = map.edges.filter((e) => e.from !== e.to && (e.from === OUTSIDE || ids.includes(e.from)) && ids.includes(e.to));
		// Each box's column: one after the furthest of those that lead to it.
		// A line that closes a circle does not count.
		const rank = {}, incoming = {};
		for (const id of ids) incoming[id] = real.filter((e) => e.to === id && e.from !== OUTSIDE).length;
		const queue = ids.filter((id) => !incoming[id]);
		for (const id of queue) rank[id] = 0;
		for (let i = 0; i < queue.length; i++) {
			for (const e of real) {
				if (e.from !== queue[i]) continue;
				rank[e.to] = Math.max(rank[e.to] || 0, rank[e.from] + 1);
				if (--incoming[e.to] === 0) queue.push(e.to);
			}
		}
		for (const id of ids) if (rank[id] === undefined) rank[id] = 0;
		const columns = [];
		for (const id of ids) (columns[rank[id]] = columns[rank[id]] || []).push(id);
		for (let c = 0; c < columns.length; c++) columns[c] = columns[c] || [];
		// Within a column, a box sits near those it is joined to in the columns
		// beside it: fewer lines cross.
		const place = {};
		const index = () => columns.forEach((col) => col.forEach((id, i) => { place[id] = i; }));
		index();
		const mean = (id, towards) => {
			const near = real.filter((e) => (towards < 0 ? e.to === id : e.from === id)).map((e) => (towards < 0 ? e.from : e.to)).filter((o) => o !== OUTSIDE && rank[o] !== rank[id]);
			return near.length ? near.reduce((s, o) => s + place[o], 0) / near.length : place[id];
		};
		for (let sweep = 0; sweep < 4; sweep++) {
			const towards = sweep % 2 ? 1 : -1;
			const order = towards < 0 ? columns.keys() : [...columns.keys()].reverse();
			for (const c of order) {
				const key = Object.fromEntries(columns[c].map((id) => [id, mean(id, towards)]));
				columns[c].sort((a, b) => key[a] - key[b] || place[a] - place[b]);
				index();
			}
		}
		const node = Object.fromEntries(map.nodes.map((n) => [n.id, n]));
		const bw = Math.max(20, Math.min(30, Math.max(...map.nodes.map((n) => n.label.length), 8) + 8));
		const tallest = Math.max(...columns.map((col) => col.length), 1);
		const height = tallest * (BH + GAP) - GAP + 4;
		const left = outside ? 10 : 0;
		const colX = (c) => left + c * (bw + CHANNEL);
		const width = colX(columns.length - 1) + bw + 1;
		const boxes = {};
		columns.forEach((col, c) => {
			const y0 = Math.floor((height - (col.length * (BH + GAP) - GAP)) / 2);
			col.forEach((id, i) => {
				boxes[id] = { id, x: colX(c), y: y0 + i * (BH + GAP), w: bw, h: BH, title: clip(node[id].label, bw - 6), style: STYLES[node[id].shape] || 'light', shape: node[id].shape, column: c, slot: i };
			});
		});
		// The lines. One to the next column turns in the channel between the two;
		// one that goes further, or back, runs along the top or the bottom of
		// the picture.
		const mid = Math.floor(height / 2);
		const edges = real.map((e, i) => {
			const to = boxes[e.to], ty = to.y + 1;
			let path;
			if (e.from === OUTSIDE) {
				path = [[0, mid], [5, mid], [5, ty], [to.x - 1, ty]];
				if (to.column > 0) path = [[0, mid], [5, mid], [5, height - 1], [to.x - 4, height - 1], [to.x - 4, ty], [to.x - 1, ty]];
			} else {
				const from = boxes[e.from], fy = from.y + 2, fx = from.x + bw;
				const out = fx + 2 + 2 * (from.slot % 4), into = to.x - 3 - 2 * (to.slot % 3);
				if (to.column === from.column + 1) {
					path = [[fx, fy], [out, fy], [out, ty], [to.x - 1, ty]];
				} else {
					// Along the boxes it passes, just above them or just below,
					// whichever is nearer to its two ends.
					const lo = Math.min(from.column, to.column), hi = Math.max(from.column, to.column);
					const passed = Object.values(boxes).filter((b) => b.column >= lo && b.column <= hi);
					const above = Math.min(...passed.map((b) => b.y)) - 2, below = Math.max(...passed.map((b) => b.y + BH - 1)) + 2;
					const lane = Math.abs(fy - above) + Math.abs(ty - above) <= Math.abs(below - fy) + Math.abs(below - ty) ? above : below;
					path = [[fx, fy], [out, fy], [out, lane], [into, lane], [into, ty], [to.x - 1, ty]];
				}
			}
			return { id: i, from: e.from, to: e.to, stroke: e.stroke, label: e.label, cells: cells(dedupe(path)) };
		});
		// A group of boxes that stand together in one column gets a dashed frame.
		const zones = [];
		for (const g of map.groups) {
			const members = g.nodes.map((id) => boxes[id]).filter(Boolean).sort((a, b) => a.slot - b.slot);
			if (!members.length || members.some((b, i) => b.column !== members[0].column || b.slot !== members[0].slot + i)) continue;
			const first = members[0], last = members[members.length - 1];
			zones.push({ x: first.x - 1, y: first.y - 1, w: bw + 2, h: last.y + BH - first.y + 2, title: clip(g.label, bw - 6), style: 'zone' });
		}
		const sources = new Set(ids.filter((id) => !real.some((e) => e.to === id)));
		const services = Object.values(boxes).filter((b) => !b.shape || b.shape === 'square').map((b) => b.id);
		return { map, width, height, boxes, edges, zones, sources, services };
	}

	// The line an exchange went along, in a map, and whether the scenario
	// sent it (out and back) or found it there (the map's line leads to it).
	function lineOf(L, x, who) {
		const into = L.edges.filter((e) => e.to === x.peer);
		if (x.from) {
			// A call the system made: a mock knows the calls it got, not who made them.
			const e = into.find((e) => e.from === x.from) || (into.length === 1 ? into[0] : null);
			return e ? { edge: e, found: true } : null;
		}
		if (!looked(x.kind)) {
			const mine = into.find((e) => e.from === who) || into.find((e) => e.from === L.map.caller);
			if (mine) return { edge: mine };
			const from = into.filter((e) => L.sources.has(e.from));
			return from.length === 1 ? { edge: from[0] } : null;
		}
		const direct = into.find((e) => e.from === OUTSIDE || (who && e.from === who));
		if (direct) return { edge: direct };
		return into.length === 1 ? { edge: into[0], found: true } : null;
	}

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

	// The project's diagrams, read and laid out before the first picture.
	let layouts = {}, first = '';
	axxStage.scene('diagram', {
		async prepare(data) {
			const key = JSON.stringify(data.diagrams || []);
			if (key !== layouts.key) {
				layouts = { key };
				first = '';
				// Mermaid reads; it draws nothing, and starts nothing by itself.
				if ((data.diagrams || []).length && window.mermaid) mermaid.initialize({ startOnLoad: false, securityLevel: 'strict' });
				for (const d of data.diagrams || []) {
					try {
						layouts[d.name] = layout(await read(d));
						first = first || d.name;
					} catch (e) {
						axxStage.problems.push('the diagram ' + d.name + ' cannot be read: ' + String(e.message || e).split('\n').slice(0, 4).join(' '));
					}
				}
			}
			// What the cues name is in the diagram they are shown on.
			if (!first || layouts.checked === data.timeline) return;
			layouts.checked = data.timeline;
			const said = new Set();
			const problem = (text) => { if (!said.has(text)) { said.add(text); axxStage.problems.push(text); } };
			for (const seg of data.timeline.segments) {
				const L = layouts[seg.diagram || first];
				if (!L) continue;
				if (seg.as && !L.boxes[seg.as]) problem('[as ' + seg.as + ']: the diagram ' + L.map.name + ' has no box ' + seg.as);
				for (const [a, b] of seg.flows || []) {
					if (!L.edges.some((e) => e.from === a && e.to === b)) problem('[' + a + ' --> ' + b + ']: the diagram ' + L.map.name + ' has no line from ' + a + ' to ' + b);
				}
			}
		},
		mount(el) {
			el.innerHTML = '<pre class="grid"></pre><div class="messages"><div class="msg sent"></div><div class="msg seen"></div></div>';
		},
		render(el, state) {
			const take = state.take, seg = state.segment;
			let L = layouts[seg.diagram || first];
			if (!L) {
				// No diagram of the project's: the map is of what the scenario has
				// talked to. In a show, whose take grows, it is made again when
				// another joins.
				const spoke = [];
				for (const st of take.steps) for (const x of acts(st)) if (!spoke.includes(x.peer)) spoke.push(x.peer);
				const key = spoke.join('|') + '|' + (state.theme.caller || '');
				if (el._made !== key) { el._made = key; el._layout = layout(mapOf(take, state.theme)); }
				L = el._layout;
			}
			if (el._shown !== L) {
				el._shown = L;
				// The grid is as large as fits: a map of many boxes is drawn smaller.
				el.style.setProperty('--rows', L.height);
				el.style.setProperty('--cols', L.width);
			}
			const who = seg.as || L.map.caller || '';
			const grid = newGrid(L.width, L.height);
			// What the scenario has talked to by now, and what each last said.
			const met = {}, lastSaid = {}, kinds = {};
			for (const p of take.peers) kinds[p.name] = p.kind;
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
			}
			if (!active) {
				// A step that sent nothing (a check of what came back) leaves the
				// last thing that was sent and seen on the stage: it is what the
				// step is about.
				for (let i = Math.min(upTo, take.steps.length - 1); i >= 0 && !active; i--) {
					const list = acts(take.steps[i]);
					if (list.length) active = { x: list[list.length - 1], phase: 1, failed: take.steps[i].status !== 'passed', over: true };
				}
			}
			const line = active && !active.over ? lineOf(L, active.x, who) : null;
			// The lines a cue lights while the step plays: what the scenario
			// cannot see, told by whoever wrote the demo.
			const told = state.playing ? (seg.flows || []).map(([a, b]) => L.edges.find((e) => e.from === a && e.to === b)).filter(Boolean) : [];
			for (const z of L.zones) drawBox(grid, z, 'zone');
			for (const b of Object.values(L.boxes)) {
				// On a project's diagram every box is there from the start; on a
				// map of what the scenario talked to, a box is there once it did.
				if (L.map.made && b.id !== L.map.caller && !met[b.id]) continue;
				const kind = kinds[b.id] || SHAPES[b.shape] || '';
				const lines = [{ text: kind, kind: 'sub' }];
				const said = lastSaid[b.id];
				if (said) lines.push({ text: clip(said.text, b.w - 4), kind: said.kind });
				const on = (active && !active.over && active.x.peer === b.id) || told.some((e) => e.to === b.id);
				drawBox(grid, { ...b, lines }, on ? (active && active.failed ? 'fail' : 'key') : 'line');
			}
			const lit = new Set(told.map((e) => e.id));
			if (line) lit.add(line.edge.id);
			for (const e of L.edges) {
				if (lit.has(e.id) || (L.map.made && !met[e.to])) continue;
				drawEdge(grid, e, 'line');
			}
			told.forEach((e, i) => {
				// One after the other, over the step's time.
				const p = Math.min(1, Math.max(0, state.progress * told.length - i));
				drawEdge(grid, e, 'told', p > 0 && p < 1 ? p * (e.cells.length - 1) : undefined);
			});
			let sent = null, seen = null;
			if (active) {
				const x = active.x, p = active.phase;
				if (line) {
					const n = line.edge.cells.length - 1;
					let head, kind = 'coord';
					if (line.found) {
						// What the scenario found there came along the system's own line.
						kind = active.failed ? 'fail' : 'ok';
						if (p < 0.6) head = (p / 0.6) * n;
					} else if (x.sent && p < 0.45) {
						head = Math.min(1, p / 0.4) * n;
					} else if (x.seen && (x.sent ? p < 0.9 : p < 0.5)) {
						head = (1 - Math.min(1, Math.max(0, x.sent ? (p - 0.45) / 0.4 : p / 0.45))) * n;
						kind = active.failed ? 'fail' : 'ok';
					}
					drawEdge(grid, line.edge, kind, head);
				}
				const found = line && line.found;
				if (x.sent && (active.over || p > 0.05)) sent = x.sent;
				if (x.seen && (active.over || (found ? p >= 0.55 : x.sent ? p >= 0.85 : p >= 0.45))) seen = x.seen;
			}
			el.querySelector('.grid').innerHTML = html(grid);
			// A mock knows the calls it got, not who made them: the map's line says.
			let from = '';
			if (active && active.x.from) {
				const l = lineOf(L, active.x, who);
				from = active.x.from === 'system' ? (l ? l.edge.from : (L.services.length === 1 ? L.services[0] : 'system')) : active.x.from;
			}
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

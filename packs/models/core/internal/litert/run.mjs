// Runs TFLite models with LiteRT (LiteRT.js, its WebAssembly build, on the
// CPU) for axx's tests of stubs. It reads a JSON list of runs on stdin, each a
// model's path and its inputs ({type, dims, data}, by name), and writes their
// outputs, as a JSON list, on stdout.
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

// LiteRT.js loads its WebAssembly module with a script, as a page or a worker
// does: in Node, as a worker would, with what its module asks of Node.
const here = path.dirname(fileURLToPath(import.meta.url));
const wasm = path.join(here, 'node_modules', '@litertjs', 'core', 'wasm');
globalThis.self = globalThis;
globalThis.require = createRequire(import.meta.url);
globalThis.__dirname = wasm;
globalThis.importScripts = (url) => vm.runInThisContext(readFileSync(fileURLToPath(url), 'utf8'), { filename: String(url) });
const { loadLiteRt, loadAndCompile, Tensor } = await import('@litertjs/core');

const arrays = {
	float32: Float32Array, int8: Int8Array, uint8: Uint8Array, int16: Int16Array,
	int32: Int32Array, int64: BigInt64Array, bool: Uint8Array,
};

const log = console.log;
console.log = console.error; // LiteRT's own messages, out of the results
await loadLiteRt('file://' + wasm + path.sep);

let input = '';
for await (const chunk of process.stdin) input += chunk;
const results = [];
const models = new Map();
for (const run of JSON.parse(input)) {
	let model = models.get(run.model);
	if (!model) {
		model = await loadAndCompile(new Uint8Array(readFileSync(run.model)), { accelerator: 'wasm' });
		models.set(run.model, model);
	}
	const inputs = model.getInputDetails().map((d) => {
		const t = run.feeds[d.name];
		if (!t) throw new Error(`no value for input ${d.name}`);
		const Arr = arrays[t.type];
		const data = t.type === 'int64' ? Arr.from(t.data.map((v) => BigInt(Math.round(v)))) : Arr.from(t.data);
		return new Tensor(data, t.dims);
	});
	const outputs = await model.run(inputs);
	const result = {};
	const details = model.getOutputDetails();
	for (let i = 0; i < outputs.length; i++) {
		const data = await outputs[i].data();
		result[details[i].name] = { type: details[i].dtype, dims: Object.values(details[i].shape).map(Number), data: Array.from(data, Number) };
	}
	results.push(result);
}
log(JSON.stringify(results));

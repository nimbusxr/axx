// Runs ONNX models with ONNX Runtime (onnxruntime-node) for axx's tests of
// stubs. It reads a JSON list of runs on stdin, each a model's path and its
// inputs ({type, dims, data}, by name), and writes their outputs, as a JSON
// list, on stdout.
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const ort = require('onnxruntime-node');

const arrays = {
	float32: Float32Array, float64: Float64Array, int8: Int8Array, uint8: Uint8Array,
	int16: Int16Array, uint16: Uint16Array, int32: Int32Array, uint32: Uint32Array,
	int64: BigInt64Array, uint64: BigUint64Array, bool: Uint8Array,
};

let input = '';
for await (const chunk of process.stdin) input += chunk;
const runs = JSON.parse(input);
const sessions = new Map();
const results = [];
for (const run of runs) {
	let session = sessions.get(run.model);
	if (!session) {
		session = await ort.InferenceSession.create(run.model, { executionProviders: ['cpu'], logSeverityLevel: 3 });
		sessions.set(run.model, session);
	}
	const feeds = {};
	for (const [name, t] of Object.entries(run.feeds)) {
		const Arr = arrays[t.type];
		const data = t.type.endsWith('int64') ? Arr.from(t.data.map((v) => BigInt(Math.round(v)))) : Arr.from(t.data);
		feeds[name] = new ort.Tensor(t.type, data, t.dims);
	}
	const out = await session.run(feeds);
	const result = {};
	for (const [name, t] of Object.entries(out)) {
		result[name] = { type: t.type, dims: t.dims.map(Number), data: Array.from(t.data, (v) => (typeof v === 'bigint' ? Number(v) : Number(v))) };
	}
	results.push(result);
}
process.stdout.write(JSON.stringify(results));

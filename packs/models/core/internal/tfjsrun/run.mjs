// Runs TensorFlow.js graph models (model.json and its weights' shards) with
// TensorFlow.js (its converter's GraphModel, on its CPU backend: what pages
// and Node.js services run them with) for axx's tests of stubs. It reads a
// JSON list of runs on stdin, each a model's model.json and its inputs
// ({type, dims, data}, by its signature's keys), and writes their outputs,
// by their keys, as a JSON list on stdout.
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';

const require = createRequire(import.meta.url);
const tf = require('@tensorflow/tfjs-core');
require('@tensorflow/tfjs-backend-cpu');
const { loadGraphModel } = require('@tensorflow/tfjs-converter');

// files loads a model.json and its shards, as tf.io's handlers of pages
// fetch them.
function files(path) {
	return {
		load: async () => {
			const json = JSON.parse(readFileSync(path, 'utf8'));
			const specs = [];
			const buffers = [];
			for (const group of json.weightsManifest) {
				specs.push(...group.weights);
				for (const p of group.paths) buffers.push(readFileSync(join(dirname(path), p)));
			}
			const all = Buffer.concat(buffers);
			return {
				modelTopology: json.modelTopology, weightSpecs: specs,
				weightData: all.buffer.slice(all.byteOffset, all.byteOffset + all.byteLength),
				format: json.format, generatedBy: json.generatedBy, convertedBy: json.convertedBy,
				signature: json.signature, userDefinedMetadata: json.userDefinedMetadata,
			};
		},
	};
}

await tf.setBackend('cpu');
let input = '';
for await (const chunk of process.stdin) input += chunk;
const runs = JSON.parse(input);
const models = new Map();
const results = [];
for (const run of runs) {
	let model = models.get(run.model);
	if (!model) {
		model = await loadGraphModel(files(run.model));
		models.set(run.model, model);
	}
	const feeds = {};
	for (const [name, t] of Object.entries(run.feeds)) {
		const type = t.type === 'bool' ? 'bool' : t.type === 'float32' ? 'float32' : 'int32';
		feeds[name] = tf.tensor(t.data, t.dims, type);
	}
	const names = model.outputNodes;
	let outs = await model.executeAsync(feeds, names);
	if (!Array.isArray(outs)) outs = [outs];
	const result = {};
	for (let i = 0; i < names.length; i++) {
		result[names[i]] = { type: outs[i].dtype, dims: outs[i].shape, data: Array.from(await outs[i].data(), Number) };
	}
	results.push(result);
}
process.stdout.write(JSON.stringify(results));

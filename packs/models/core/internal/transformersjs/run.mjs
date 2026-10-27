// Runs language models with transformers.js (Hugging Face's, on ONNX
// Runtime's Node.js binding) for axx's tests of stubs. It reads a JSON list
// of jobs on stdin, each a model's folder (as Hugging Face's repositories
// have it: config.json, the tokenizer's files, onnx/model*.onnx) and either
// texts to tokenize (without special tokens), chats to answer (a system
// prompt, a user's prompt and how to sample, as an app runs its
// text-generation pipeline), sounds (their samples) to transcribe, as its
// automatic-speech-recognition pipeline does, sounds whose features its
// processor computes, texts to embed (with its feature-extraction pipeline,
// pooled and normalized, or as the model's output that names their
// embedding: EmbeddingGemma's sentence_embedding), or texts to classify
// (its text-classification pipeline), and writes their results, as a JSON
// list, on stdout.
import path from 'node:path';

const log = console.log;
console.log = console.error; // transformers.js's own messages, out of the results
const { env, AutoTokenizer, AutoProcessor, AutoModel, pipeline } = await import('@huggingface/transformers');
env.allowRemoteModels = false;

let input = '';
for await (const chunk of process.stdin) input += chunk;
const results = [];
for (const job of JSON.parse(input)) {
	env.localModelPath = path.dirname(job.model) + path.sep;
	const name = path.basename(job.model);
	if (job.tokenize) {
		const tokenizer = await AutoTokenizer.from_pretrained(name);
		results.push({ tokens: job.tokenize.map((t) => Array.from(tokenizer.encode(t, { add_special_tokens: false }))) });
		continue;
	}
	if (job.embed) {
		const embeddings = [];
		if (job.output) {
			const tokenizer = await AutoTokenizer.from_pretrained(name);
			const model = await AutoModel.from_pretrained(name, { dtype: job.dtype || 'fp32', device: 'cpu' });
			for (const text of job.embed) {
				const out = await model(await tokenizer(text));
				embeddings.push(Array.from(out[job.output].data));
			}
			await model.dispose();
		} else {
			const extractor = await pipeline('feature-extraction', name, { dtype: job.dtype || 'fp32', device: 'cpu' });
			for (const text of job.embed) {
				const out = await extractor(text, { pooling: 'mean', normalize: true });
				embeddings.push(Array.from(out.data));
			}
			await extractor.dispose();
		}
		results.push({ features: embeddings });
		continue;
	}
	if (job.classify) {
		const classifier = await pipeline('text-classification', name, { dtype: job.dtype || 'fp32', device: 'cpu' });
		const answers = [];
		for (const text of job.classify) {
			const [top] = await classifier(text);
			answers.push(JSON.stringify(top));
		}
		results.push({ answers });
		await classifier.dispose();
		continue;
	}
	if (job.features) {
		const processor = await AutoProcessor.from_pretrained(name);
		const features = [];
		for (const samples of job.features) {
			const { input_features } = await processor(Float32Array.from(samples));
			features.push(Array.from(input_features.data));
		}
		results.push({ features });
		continue;
	}
	if (job.transcribe) {
		const recognizer = await pipeline('automatic-speech-recognition', name, { dtype: job.dtype || 'fp32', device: 'cpu' });
		const answers = [];
		for (const samples of job.transcribe) {
			const out = await recognizer(Float32Array.from(samples), { return_timestamps: !!job.timestamps });
			answers.push(job.timestamps ? JSON.stringify(out.chunks) : out.text);
		}
		results.push({ answers });
		await recognizer.dispose();
		continue;
	}
	const generator = await pipeline('text-generation', name, { dtype: job.dtype || 'fp32', device: 'cpu' });
	const answers = [];
	for (const chat of job.chats) {
		const messages = [];
		if (chat.system) messages.push({ role: 'system', content: chat.system });
		messages.push({ role: 'user', content: chat.prompt });
		const options = { max_new_tokens: chat.maxTokens || 256, do_sample: chat.temperature > 0 };
		if (chat.temperature > 0) options.temperature = chat.temperature;
		const out = await generator(messages, options);
		answers.push(out[0].generated_text.at(-1).content);
	}
	results.push({ answers });
	await generator.dispose();
}
log(JSON.stringify(results));

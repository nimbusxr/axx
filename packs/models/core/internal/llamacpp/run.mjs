// Runs GGUF models with llama.cpp (node-llama-cpp) for axx's tests of stubs.
// It reads a JSON list of jobs on stdin, each a model's path and either texts
// to tokenize (as llama.cpp does without special tokens), texts to embed (an
// embedding context's, as an app gets a text's embedding), a query's and
// documents' pairs to rank (a ranking context's: its score's sigmoid) or
// chats to answer
// (a system prompt, a user's prompt and a temperature, each in a context of
// its own, as an app runs a chat session), and writes their results, as a
// JSON list, on stdout.
import { getLlama, LlamaChatSession } from 'node-llama-cpp';

let input = '';
for await (const chunk of process.stdin) input += chunk;
const jobs = JSON.parse(input);
const llama = await getLlama({ logLevel: 'error', gpu: 'auto' });
const results = [];
for (const job of jobs) {
	if (job.tokenize) {
		const model = await llama.loadModel({ modelPath: job.model, vocabOnly: true });
		results.push({ tokens: job.tokenize.map((t) => Array.from(model.tokenize(t, false))) });
		await model.dispose();
		continue;
	}
	const model = await llama.loadModel({ modelPath: job.model, gpuLayers: job.gpu ? 'max' : 0 });
	if (job.rank) {
		const context = await model.createRankingContext({ contextSize: 512 });
		const scores = [];
		for (const pair of job.rank) scores.push(await context.rank(pair.query, pair.document));
		await context.dispose();
		results.push({ scores });
		await model.dispose();
		continue;
	}
	if (job.embed) {
		const context = await model.createEmbeddingContext({ contextSize: 512 });
		const embeddings = [];
		for (const text of job.embed) embeddings.push((await context.getEmbeddingFor(text)).vector);
		await context.dispose();
		results.push({ embeddings });
		await model.dispose();
		continue;
	}
	const answers = [];
	for (const chat of job.chats) {
		const context = await model.createContext({ contextSize: 4096, flashAttention: !!job.flashAttention });
		const session = new LlamaChatSession({ contextSequence: context.getSequence(), systemPrompt: chat.system || undefined });
		answers.push(await session.prompt(chat.prompt, { temperature: chat.temperature, maxTokens: chat.maxTokens || 256 }));
		await context.dispose();
	}
	results.push({ answers });
	await model.dispose();
}
process.stdout.write(JSON.stringify(results));

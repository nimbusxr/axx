// Runs MLC LLM's models with WebLLM, in a page of Chromium on its GPU
// (WebGPU), for axx's tests of stubs. It reads a JSON list of jobs on stdin:
// each a model's folder, its model library, whether the library computes in
// halves (shader-f16), its config's overrides (the context window it runs
// at: WebLLM's prebuilt models'), and the conversations
// it answers (each the user's prompts, in turns that keep the answers before
// them) at a temperature. It
// writes each job's answers, as a JSON list, on stdout, or why the machine's
// GPU cannot run its library.
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const here = path.dirname(fileURLToPath(import.meta.url));
const webllm = path.join(here, 'node_modules', '@mlc-ai', 'web-llm', 'lib', 'index.js');

let input = '';
for await (const chunk of process.stdin) input += chunk;
const jobs = JSON.parse(input);

// The page, WebLLM, and each job's model (as Hugging Face serves a model,
// under resolve/main/, where WebLLM looks) and library.
const page = `<!doctype html><meta charset="utf-8"><script type="module">
import * as webllm from './web-llm.js';
window.run = async (job) => {
  const adapter = navigator.gpu && await navigator.gpu.requestAdapter();
  if (!adapter) return { unavailable: 'the browser has no WebGPU adapter' };
  const info = adapter.info || {};
  if (info.isFallbackAdapter || info.architecture === 'swiftshader') return { unavailable: "the machine has no GPU: WebGPU's adapter is SwiftShader's, on its processor" };
  if (job.f16 && !adapter.features.has('shader-f16')) return { unavailable: 'its GPU has no shader-f16 (halves in shaders)' };
  const id = 'stub-' + job.index;
  const record = { model: location.origin + '/models/' + job.index + '/', model_id: id, model_lib: location.origin + '/libs/' + job.index + '.wasm' };
  if (job.overrides) record.overrides = job.overrides;
  const appConfig = { useIndexedDBCache: false, model_list: [record] };
  const engine = await webllm.CreateMLCEngine(id, { appConfig });
  const answers = [];
  for (const prompts of job.conversations) {
    const messages = job.system ? [{ role: 'system', content: job.system }] : [];
    const turns = [];
    for (const content of prompts) {
      messages.push({ role: 'user', content });
      const r = await engine.chat.completions.create({ messages, temperature: job.temperature, max_tokens: job.maxTokens || 64 });
      const answer = r.choices[0].message.content;
      messages.push({ role: 'assistant', content: answer });
      turns.push(answer);
    }
    answers.push(turns);
  }
  await engine.unload();
  return { answers };
};
window.ready = true;
</script>`;
const types = { '.json': 'application/json', '.wasm': 'application/wasm', '.js': 'text/javascript' };
const server = http.createServer(async (req, res) => {
	const url = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
	let file, m;
	if (url === '/') {
		res.setHeader('content-type', 'text/html');
		return res.end(page);
	} else if (url === '/web-llm.js') {
		file = webllm;
	} else if ((m = url.match(/^\/models\/(\d+)\/resolve\/main\/([^/]+)$/))) {
		file = path.join(jobs[Number(m[1])].model, m[2]);
	} else if ((m = url.match(/^\/libs\/(\d+)\.wasm$/))) {
		file = jobs[Number(m[1])].lib;
	}
	try {
		const body = await readFile(file);
		res.setHeader('content-type', types[path.extname(file)] || 'application/octet-stream');
		res.end(body);
	} catch {
		res.statusCode = 404;
		res.end();
	}
});
await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));

// Chromium's new headless mode, which has the GPU (its headless shell has
// SwiftShader's alone), of a profile on disk: WebLLM caches a model's shards
// (a vocabulary's embeddings may be hundreds of MB), which a context of no
// profile keeps in memory, of a small quota.
const profile = await mkdtemp(path.join(os.tmpdir(), 'axx-webllm-'));
const browser = await chromium.launchPersistentContext(profile, { channel: 'chromium', headless: true, args: ['--enable-unsafe-webgpu', ...(process.platform === 'linux' ? ['--enable-features=Vulkan'] : [])] });
const results = [];
try {
	const tab = browser.pages()[0] || (await browser.newPage());
	tab.on('console', (m) => m.type() === 'error' && console.error('page:', m.text()));
	await tab.goto(`http://localhost:${server.address().port}/`);
	await tab.waitForFunction(() => window.ready);
	for (const [index, job] of jobs.entries()) {
		results.push(await tab.evaluate((j) => window.run(j), { ...job, index }));
	}
} finally {
	await browser.close();
	server.close();
	await rm(profile, { recursive: true, force: true });
}
process.stdout.write(JSON.stringify(results));

// Drafts a parcel's customs description from what the shop says it holds,
// with a small language model the page runs itself (transformers.js, on
// ONNX Runtime's WebAssembly), from the portal's models: what the shop
// writes stays in its browser.
import { env, pipeline } from '/portal/vendor/transformers.min.js';

env.allowRemoteModels = false;
env.allowLocalModels = true;
env.localModelPath = '/portal/models/';
env.backends.onnx.wasm.wasmPaths = '/portal/vendor/';

const button = document.getElementById('describe');
const status = document.getElementById('describing');
let drafter;

button.addEventListener('click', async () => {
  const contents = document.getElementById('contents').value.trim();
  if (!contents) {
    status.textContent = 'Say what the parcel holds first';
    return;
  }
  button.disabled = true;
  status.textContent = 'Describing…';
  try {
    drafter ??= await pipeline('text-generation', 'customs-draft', { dtype: 'q4f16', device: 'wasm' });
    const out = await drafter([
      { role: 'system', content: "You write the description of a parcel's contents for its customs declaration: what the goods are and what they are made of, in a few words." },
      { role: 'user', content: contents },
    ], { max_new_tokens: 48 });
    document.getElementById('customs').value = out[0].generated_text.at(-1).content.trim();
    status.textContent = '';
  } catch {
    status.textContent = 'The description could not be drafted: write it yourself';
  } finally {
    button.disabled = false;
  }
});

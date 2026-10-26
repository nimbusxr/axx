// A recipient sending a delivered parcel back says why from its tracking
// page, in their own words, and the page suggests the reason from them, with
// a language model it runs itself (WebLLM: SmolLM2-135M, on the device's GPU,
// through WebGPU), from the portal's models. The recipient can choose another
// reason before they send it.
import { CreateMLCEngine } from '/portal/vendor/web-llm.js';

const ref = document.body.dataset.reference;
const why = document.getElementById('return-why');
const reason = document.getElementById('return-reason');
const send = document.getElementById('send-back');
const status = document.getElementById('return-status');
// The reasons, by their names, which the model answers with.
const reasons = new Map([...reason.options].filter((o) => o.value).map((o) => [o.text, o.value]));
const instructions =
  'A customer is sending a parcel back. Answer with the reason of theirs among these, and nothing else: ' +
  [...reasons.keys()].join(', ') + '.';

// The model: its build of float32s, which every GPU runs (one of halves
// needs halves in shaders, which some have not). It is fetched and made ready
// the first time a suggestion is wanted, not for every visit to the page.
let engine;
const model = () => engine ??= CreateMLCEngine('return-reason', {
  appConfig: {
    model_list: [{
      model: location.origin + '/portal/models/return-reason/resolve/main/',
      model_id: 'return-reason',
      model_lib: location.origin + '/portal/vendor/SmolLM2-135M-Instruct-q4f32_1_cs1k-webgpu.wasm',
      overrides: { context_window_size: 2048 },
    }],
  },
});

// One question at a time, the last's answer the one that counts; and none
// once the recipient chose a reason themselves.
let asked = 0;
let queue = Promise.resolve();
let typing;
let chosen = false;
reason.addEventListener('change', () => { chosen = true; });
why.addEventListener('input', () => {
  clearTimeout(typing);
  typing = setTimeout(() => { queue = queue.then(suggest); }, 600);
});

async function suggest() {
  const words = why.value.trim();
  if (!words || chosen) return;
  const mine = ++asked;
  status.textContent = 'Suggesting a reason…';
  try {
    const answer = await (await model()).chat.completions.create({
      messages: [{ role: 'system', content: instructions }, { role: 'user', content: words }],
      temperature: 0, max_tokens: 8,
    });
    if (mine !== asked || chosen) return; // the recipient wrote more since, or chose
    const value = reasons.get(answer.choices[0].message.content.trim());
    if (value) {
      reason.value = value;
      status.textContent = 'Suggested from what you wrote: ' + reason.selectedOptions[0].text;
    } else {
      status.textContent = 'Choose the reason';
    }
  } catch {
    status.textContent = 'Choose the reason';
  }
}

send.addEventListener('click', async () => {
  if (!reason.value) {
    status.textContent = 'Choose the reason first';
    return;
  }
  const sent = await fetch('/portal/track/' + encodeURIComponent(ref) + '/returns', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ reason: reason.value, why: why.value.trim() }),
  });
  status.textContent = sent.ok
    ? "We'll collect your parcel: " + reason.selectedOptions[0].text
    : 'Your parcel could not be sent back: try again later';
});

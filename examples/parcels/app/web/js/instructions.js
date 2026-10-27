// A recipient tells the courier where to leave the parcel, from its tracking
// page, in any language: the browser's own language models (Chrome's built-in
// language detector and translator) put the instructions in English, the
// couriers' language, which the service keeps as the parcel's delivery note.
// The browser's proofreader checks their spelling, and its speech recognition
// takes them spoken. A browser without them keeps the instructions as
// written.
const ref = document.body.dataset.reference;
const $ = (id) => document.getElementById(id);
const field = $('instructions');
const status = $('instructions-status');
const spelling = $('spelling');
const use = $('use-correction');
// How sure of a language the detector must be to translate from it.
const certainty = 0.5;
let correction = '';

// The instructions in English: as written, or translated from the language
// the browser's detector finds them in.
async function inEnglish(text) {
  if (!('LanguageDetector' in self) || !('Translator' in self)) return text;
  const [likeliest] = await (await LanguageDetector.create()).detect(text);
  const language = likeliest?.detectedLanguage;
  if (!language || language === 'en' || language === 'und' || likeliest.confidence < certainty) return text;
  const translator = await Translator.create({ sourceLanguage: language, targetLanguage: 'en' });
  return translator.translate(text);
}

$('save-instructions').addEventListener('click', async () => {
  const text = field.value.trim();
  if (!text) {
    status.textContent = 'Write where to leave the parcel first';
    return;
  }
  status.textContent = 'Saving…';
  try {
    const note = await inEnglish(text);
    const saved = await fetch('/portal/track/' + encodeURIComponent(ref) + '/note', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ note }),
    });
    status.textContent = saved.ok ? 'Saved. Your courier reads: ' + note : 'The instructions could not be saved: try again later';
  } catch {
    status.textContent = 'The instructions could not be saved: try again later';
  }
});

$('check-spelling').addEventListener('click', async () => {
  use.hidden = true;
  if (!('Proofreader' in self)) {
    spelling.textContent = 'This browser cannot check spelling';
    return;
  }
  const result = await (await Proofreader.create({ expectedInputLanguages: ['en'] })).proofread(field.value);
  if (!result.corrections.length) {
    spelling.textContent = 'No spelling mistakes found';
    return;
  }
  correction = result.correctedInput;
  spelling.textContent = 'Did you mean: ' + correction;
  use.hidden = false;
});

use.addEventListener('click', () => {
  field.value = correction;
  use.hidden = true;
  spelling.textContent = '';
});

const Recognition = self.SpeechRecognition || self.webkitSpeechRecognition;
$('dictate').hidden = !Recognition;
$('dictate').addEventListener('click', () => {
  const recognition = new Recognition();
  recognition.lang = navigator.language;
  recognition.onresult = (e) => {
    const said = e.results[0][0].transcript;
    field.value = field.value.trim() ? field.value.trim() + ' ' + said : said;
    status.textContent = '';
  };
  recognition.onerror = () => {
    status.textContent = 'Nothing was heard: try again';
  };
  status.textContent = 'Listening…';
  recognition.start();
});

// A shop writes to a parcel's recipient from the parcel's page: the
// browser's own writing models (Chrome's built-in Writer and Rewriter) draft
// the message from what the shop says it should say, and make it shorter.
// Its Summarizer puts a recipient's long delivery note in a line. What the
// shop writes stays in its browser.
const $ = (id) => document.getElementById(id);
const { recipient, sender, reference } = $('writing').dataset;
const status = $('drafting');
const message = $('message');

$('draft').hidden = !('Writer' in self);
$('shorten').hidden = !('Rewriter' in self);

async function write(what) {
  status.textContent = 'Writing…';
  try {
    message.value = await what();
    status.textContent = '';
  } catch {
    status.textContent = 'The message could not be written: write it yourself';
  }
}

$('draft').addEventListener('click', () => write(async () => {
  const brief = $('brief').value.trim();
  if (!brief) throw new Error('nothing to say');
  const writer = await Writer.create({
    tone: 'formal', format: 'plain-text', length: 'medium',
    sharedContext: `You write for the shop ${sender} to ${recipient}, who is to receive its parcel ${reference}.`,
  });
  return writer.write(brief);
}));

$('shorten').addEventListener('click', () => write(async () => {
  const rewriter = await Rewriter.create({ length: 'shorter', format: 'plain-text' });
  return rewriter.rewrite(message.value);
}));

const summarize = $('summarize-note');
if (summarize) {
  summarize.hidden = !('Summarizer' in self);
  summarize.addEventListener('click', async () => {
    const summary = $('note-summary');
    try {
      const summarizer = await Summarizer.create({ type: 'tldr', length: 'short', format: 'plain-text' });
      summary.textContent = 'In short: ' + await summarizer.summarize($('note').textContent);
    } catch {
      summary.textContent = 'The note could not be summarized';
    }
  });
}

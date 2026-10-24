// A recipient whose parcel arrived damaged sends a photo of it from its
// tracking page. The page checks the photo first, with an image classifier
// it runs itself (TensorFlow.js: MobileNetV3, of ImageNet's classes), from
// the portal's models: a photo that shows no parcel is not sent.
import { browser, image, loadGraphModel, softmax, tidy } from '/portal/vendor/tf.fesm.min.js';

const ref = document.body.dataset.reference;
const photo = document.getElementById('damage-photo');
const status = document.getElementById('damage-status');
// The classes of a parcel, and how sure of one the classifier must be.
const parcels = new Set(['carton', 'packet', 'envelope', 'crate', 'mailbag']);
const certainty = 0.5;
let model, labels;

// The likeliest class of the picture, and how likely: its middle square, at
// the model's 224×224, from 0 to 1.
function classify(picture) {
  return tidy(() => {
    const pixels = browser.fromPixels(picture);
    const [h, w] = pixels.shape;
    const side = Math.min(h, w);
    const square = pixels.slice([Math.floor((h - side) / 2), Math.floor((w - side) / 2), 0], [side, side, 3]);
    const input = image.resizeBilinear(square, [224, 224]).toFloat().div(255).expandDims(0);
    const scores = softmax(model.predict(input)).dataSync();
    let best = 0;
    for (let i = 1; i < scores.length; i++) if (scores[i] > scores[best]) best = i;
    return [labels[best], scores[best]];
  });
}

photo.addEventListener('change', async () => {
  const file = photo.files[0];
  if (!file) return;
  status.textContent = 'Checking the photo…';
  try {
    model ??= await loadGraphModel('/portal/models/photo-check/model.json');
    labels ??= (await (await fetch('/portal/models/photo-check/labels.txt')).text()).trim().split('\n');
    const [label, score] = classify(await createImageBitmap(file));
    if (!parcels.has(label) || score < certainty) {
      status.textContent = 'This photo does not show a parcel: send a photo of the parcel itself';
      return;
    }
    const sent = await fetch('/portal/track/' + encodeURIComponent(ref) + '/photos', {
      method: 'POST', headers: { 'Content-Type': file.type }, body: file,
    });
    status.textContent = sent.ok ? 'Thank you: your photo is with your parcel' : 'The photo could not be sent: try again later';
  } catch {
    status.textContent = 'The photo could not be checked: try again later';
  }
});

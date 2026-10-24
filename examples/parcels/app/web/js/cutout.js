// Cuts the photo of a parcel's contents out of its background, for its
// customs declaration, with a background remover the page runs itself
// (transformers.js, on ONNX Runtime's WebAssembly), from the portal's models:
// the photo stays in the shop's browser, and its cutout goes with the parcel.
import { env, pipeline, RawImage } from '/portal/vendor/transformers.min.js';

env.allowRemoteModels = false;
env.allowLocalModels = true;
env.localModelPath = '/portal/models/';
env.backends.onnx.wasm.wasmPaths = '/portal/vendor/';

const photo = document.getElementById('contents-photo');
const status = document.getElementById('cutting');
const preview = document.getElementById('contents-cutout');
const upload = document.getElementById('contents-cutout-file');
let remover;

photo.addEventListener('change', async () => {
  const file = photo.files[0];
  preview.hidden = true;
  upload.value = '';
  if (!file) return;
  status.textContent = 'Removing the background…';
  photo.disabled = true;
  try {
    remover ??= await pipeline('background-removal', 'contents-cutout', { dtype: 'fp16', device: 'wasm' });
    const cutout = await (await remover(await RawImage.fromBlob(file))).toBlob('image/png');
    const files = new DataTransfer();
    files.items.add(new File([cutout], file.name.replace(/\.[^.]*$/, '') + '-cutout.png', { type: 'image/png' }));
    upload.files = files.files;
    preview.src = URL.createObjectURL(cutout);
    preview.hidden = false;
    status.textContent = 'Background removed';
  } catch {
    status.textContent = 'The background could not be removed: choose the photo again';
  } finally {
    photo.disabled = false;
  }
});

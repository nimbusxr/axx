// A shop pastes the address a customer wrote (in an email, a chat), and the
// page fills in the recipient's fields from it, with the browser's own
// language model (Chrome's built-in Prompt API), asked for JSON of the
// fields: what the customer wrote stays in the shop's browser.
const $ = (id) => document.getElementById(id);
const button = $('fill-address');
const status = $('filling');
const fields = ['name', 'street', 'postcode', 'city'];
const schema = {
  type: 'object',
  properties: {
    name: { type: 'string' }, street: { type: 'string' }, postcode: { type: 'string' }, city: { type: 'string' },
    country: { type: 'string', description: 'ISO 3166-1 alpha-2 code' },
  },
};
let model;

button.hidden = !('LanguageModel' in self);
button.addEventListener('click', async () => {
  const text = $('pasted').value.trim();
  if (!text) {
    status.textContent = 'Paste the address first';
    return;
  }
  status.textContent = 'Reading the address…';
  try {
    model ??= await LanguageModel.create({
      initialPrompts: [{ role: 'system', content: "You read a parcel recipient's postal address from what a customer wrote: their name, street and house number, postcode, city and country." }],
    });
    // Each address of its own: a clone of the model has none of the others.
    const address = JSON.parse(await (await model.clone()).prompt(text, { responseConstraint: schema }));
    const found = fields.filter((field) => address[field]);
    for (const field of found) $(field).value = address[field];
    if (address.country) $('country').value = address.country.toUpperCase();
    status.textContent = found.length ? 'Check the address before you register the parcel' : 'No address found in the text';
  } catch {
    status.textContent = 'The address could not be read: fill it in yourself';
  }
});

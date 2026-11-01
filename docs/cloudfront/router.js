// The viewer-request function of the docs site's CloudFront distribution
// (cloudfront-js-2.0, ADR 0005). It sends a page's Markdown twin to a
// request that prefers text/markdown (coding agents ask for it first),
// real 301s for the pages that moved (from the KeyValueStore that
// scripts/deploy.sh keeps), and index.html for a directory's address.
// router_test.mjs runs it in Node.
import cf from 'cloudfront';

const kvs = cf.kvs();

// prefersMarkdown says the Accept header ranks text/markdown above
// text/html, or lists it first at the same rank.
function prefersMarkdown(accept) {
  if (!accept) return false;
  let md = -1, html = -1, mdAt = -1, htmlAt = -1;
  const parts = accept.split(',');
  for (let i = 0; i < parts.length; i++) {
    const p = parts[i].trim().toLowerCase().split(';');
    const type = p[0].trim();
    let q = 1;
    for (let j = 1; j < p.length; j++) {
      const kv = p[j].trim().split('=');
      if (kv[0] === 'q') q = parseFloat(kv[1]);
    }
    if ((type === 'text/markdown' || type === 'text/x-markdown') && q > md) { md = q; mdAt = i; }
    if (type === 'text/html' && q > html) { html = q; htmlAt = i; }
  }
  if (md <= 0) return false;
  if (html < 0) return true;
  return md > html || (md === html && mdAt < htmlAt);
}

function redirect(location) {
  return { statusCode: 301, statusDescription: 'Moved Permanently', headers: { location: { value: location } } };
}

async function handler(event) {
  const req = event.request;
  let uri = req.uri;
  // A page that moved.
  try {
    const to = await kvs.get(uri.endsWith('/') ? uri : uri + '/');
    if (to) return redirect(to);
  } catch (e) {
    // not moved
  }
  const last = uri.substring(uri.lastIndexOf('/') + 1);
  // /guides/install is /guides/install/.
  if (last !== '' && last.indexOf('.') < 0) return redirect(uri + '/');
  if (uri.endsWith('/')) {
    const accept = req.headers.accept ? req.headers.accept.value : '';
    if (prefersMarkdown(accept)) {
      // A version's home (/, /next/) has its twin inside it; any other page beside it.
      req.uri = uri === '/' || /^\/next\/$/.test(uri) ? uri + 'index.md' : uri.slice(0, -1) + '.md';
    } else {
      req.uri = uri + 'index.html';
    }
  }
  return req;
}

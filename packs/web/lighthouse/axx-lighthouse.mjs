// axx-lighthouse.mjs: the web-lighthouse pack's Lighthouse run. It audits
// one page, in a tab of the browser axx drives, which it reaches through the
// browser's remote debugging port: the tab axx opened for it, so that the
// page has the web app's cookies and storage. It reads what to audit on
// stdin, {browserURL, targetId, url, device, categories}, and writes
// Lighthouse's result and its HTML report on stdout, {lhr, html}, or why it
// could not, {error}.
import fs from 'node:fs';
import lighthouse from 'lighthouse';
import desktopConfig from 'lighthouse/core/config/desktop-config.js';
import puppeteer from 'puppeteer-core';

async function main() {
  const req = JSON.parse(fs.readFileSync(0, 'utf8'));
  // Without a default viewport, Puppeteer leaves the pages' own alone. It
  // attaches to the audited tab only: the browser's other tabs are other
  // scenarios', which come and go as they run.
  const browser = await puppeteer.connect({
    browserURL: req.browserURL,
    defaultViewport: null,
    targetFilter: (target) => target.type() !== 'page' || target._targetId === req.targetId,
  });
  try {
    const page = await tab(browser, req.targetId);
    const flags = {output: 'html', logLevel: 'error', onlyCategories: req.categories};
    const result = await lighthouse(req.url, flags, req.device === 'desktop' ? desktopConfig : undefined, page);
    if (!result) throw new Error('Lighthouse gave no result');
    return {lhr: result.lhr, html: result.report};
  } finally {
    await browser.disconnect();
  }
}

// tab is the page of the browser's tab with that CDP target id (the pinned
// Puppeteer keeps it as _targetId).
async function tab(browser, targetId) {
  const target = browser.targets().find((t) => t._targetId === targetId);
  if (!target) throw new Error(`the browser has no tab ${targetId}`);
  return target.page();
}

// Lighthouse leaves handles open that keep Node.js running for seconds:
// exit once the output is written. An error is written as {error}.
main().then(
  out => process.stdout.write(JSON.stringify(out), () => process.exit(0)),
  err => {
    process.stderr.write(`${err && err.stack || err}\n`);
    process.stdout.write(JSON.stringify({error: String(err && err.message || err)}), () => process.exit(1));
  },
);

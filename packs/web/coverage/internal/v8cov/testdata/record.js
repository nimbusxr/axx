// Records what Node's coverage tools make of a case's takes: the outputs
// the Go port must match, byte for byte. Run once per case, never by hand:
//
//   npm install @bcoe/v8-coverage@1.0.2 v8-to-istanbul@9.3.0 \
//     istanbul-lib-coverage@3.2.2 istanbul-lib-report@3.0.1 istanbul-reports@3.2.0
//   node record.js <case folder>
//
// A case's takes.json lists V8's ScriptCoverage takes with their sources;
// scripts are files of the case's site folder, by their URL path (a source
// map beside its script). It writes merged.json (the takes merged, as c8
// merges them), and lcov.info, coverage-final.json and coverage-summary.json
// with paths relative to the case folder.
'use strict';
const fs = require('fs');
const path = require('path');
const { mergeProcessCovs } = require('@bcoe/v8-coverage');
const v8toIstanbul = require('v8-to-istanbul');
const libCoverage = require('istanbul-lib-coverage');
const libReport = require('istanbul-lib-report');
const reports = require('istanbul-reports');

async function main() {
  const dir = path.resolve(process.argv[2]);
  const site = path.join(dir, 'site');
  const takes = JSON.parse(fs.readFileSync(path.join(dir, 'takes.json'), 'utf8'));
  const sources = {};
  const processes = [];
  for (const s of takes) {
    const p = new URL(s.url).pathname;
    if (!/\.m?js$/.test(p)) continue; // inline scripts and event handlers
    sources[p] = s.source;
    processes.push({ result: [{ scriptId: s.scriptId, url: p, functions: s.functions }] });
  }
  const merged = mergeProcessCovs(processes);
  fs.writeFileSync(path.join(dir, 'merged.json'), JSON.stringify(merged, null, 1) + '\n');
  const map = libCoverage.createCoverageMap({});
  for (const sc of merged.result) {
    const conv = v8toIstanbul(path.join(site, sc.url), 0, { source: sources[sc.url] });
    await conv.load();
    conv.applyCoverage(sc.functions);
    map.merge(conv.toIstanbul());
  }
  const rel = libCoverage.createCoverageMap({});
  for (const f of map.files()) {
    rel.addFileCoverage({ ...map.fileCoverageFor(f).toJSON(), path: path.relative(dir, f) });
  }
  process.chdir(dir);
  const context = libReport.createContext({ dir, coverageMap: rel, defaultSummarizer: 'flat' });
  reports.create('lcovonly', { projectRoot: dir }).execute(context);
  reports.create('json', {}).execute(context);
  reports.create('json-summary', {}).execute(context);
}
main().catch((e) => { console.error(e); process.exit(1); });

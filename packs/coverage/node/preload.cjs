// axx's preload of a Node.js service a run with coverage measures
// (coverage-node). V8 writes the coverage NODE_V8_COVERAGE names when the
// process exits, and when this calls v8.takeCoverage(), which also starts
// its counts afresh: when the run drops a file named "take" in this folder,
// newer than the last one it saw.
'use strict';
const fs = require('fs');
const path = require('path');
const v8 = require('v8');

const dir = __dirname;
const trigger = path.join(dir, 'take');
let seen = 0;
const timer = setInterval(() => {
  let st;
  try {
    st = fs.statSync(trigger);
  } catch {
    return;
  }
  if (st.mtimeMs <= seen) return;
  seen = st.mtimeMs;
  v8.takeCoverage();
  try {
    fs.writeFileSync(path.join(dir, 'taken-' + process.pid), String(st.mtimeMs));
  } catch {
    // a folder it cannot write to: the run waits, and goes on without it
  }
}, 200);
timer.unref();

// On its stop signal, a process that does not handle it exits through
// process.exit, where V8 writes the coverage, with the code the signal
// would have given it. One that handles it ends as it does.
for (const [signal, code] of [['SIGTERM', 143], ['SIGINT', 130]]) {
  process.on(signal, function axxCoverageStop() {
    if (process.listenerCount(signal) === 1) process.exit(code);
  });
}

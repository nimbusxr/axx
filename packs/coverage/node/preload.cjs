// axx's preload of a Node.js service a run with coverage measures
// (coverage-node). V8 writes the coverage NODE_V8_COVERAGE names when the
// process exits, and when this calls v8.takeCoverage(), which also starts
// its counts afresh: when the run drops a file named "take" in this folder,
// newer than the last one it saw.
//
// Node writes those files readable by their owner only. A container's
// process often runs as a user of its own, and this machine's user, which
// reads them, is another: so the preload makes each file it has V8 write
// readable by everyone. As the process exits, it has V8 write what ran,
// makes it readable, and marks it ("exited-" and its pid): the file V8
// still writes after, of what ran since, is one axx may find unreadable,
// and then knows it is only that.
'use strict';
const fs = require('fs');
const path = require('path');
const v8 = require('v8');

const dir = __dirname;
const trigger = path.join(dir, 'take');
const written = process.env.NODE_V8_COVERAGE;

// readable makes the files V8 wrote for this process readable by everyone.
function readable() {
  let names;
  try {
    names = fs.readdirSync(written);
  } catch {
    return;
  }
  const own = 'coverage-' + process.pid + '-';
  for (const name of names) {
    if (!name.startsWith(own)) continue;
    try {
      fs.chmodSync(path.join(written, name), 0o644);
    } catch {
      // not its own: another process's of the same pid, before it
    }
  }
}

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
  readable();
  try {
    fs.writeFileSync(path.join(dir, 'taken-' + process.pid), String(st.mtimeMs));
  } catch {
    // a folder it cannot write to: the run waits, and goes on without it
  }
}, 200);
timer.unref();

process.on('exit', function axxCoverageExit() {
  v8.takeCoverage();
  readable();
  try {
    fs.writeFileSync(path.join(dir, 'exited-' + process.pid), '');
  } catch {
    // a folder it cannot write to: nothing of its coverage is read either
  }
});

// On its stop signal, a process that does not handle it exits through
// process.exit, where V8 writes the coverage, with the code the signal
// would have given it. One that handles it ends as it does.
for (const [signal, code] of [['SIGTERM', 143], ['SIGINT', 130]]) {
  process.on(signal, function axxCoverageStop() {
    if (process.listenerCount(signal) === 1) process.exit(code);
  });
}

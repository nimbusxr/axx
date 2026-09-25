// SPDX-License-Identifier: Apache-2.0
// Stops `axx run` and everything it started. axx stops its apps and runs their cleanups when it
// is interrupted, so it gets SIGINT first, like Ctrl+C in a terminal. If it is still running
// after the grace period, the whole process tree is killed.

import { execFile, type ChildProcess } from 'node:child_process';

const GRACE_MS = 20_000; // axx gives each app 10s to stop, then runs cleanups

// Stops a process started with `detached: true` (so it leads its own process group on POSIX).
export function stopProcessTree(child: ChildProcess, graceMs = GRACE_MS): void {
  const pid = child.pid;
  if (pid === undefined || child.exitCode !== null || child.signalCode !== null) return;
  if (process.platform === 'win32') {
    execFile('taskkill', ['/pid', String(pid), '/t', '/f'], () => undefined);
    return;
  }
  signalGroup(pid, 'SIGINT');
  const timer = setTimeout(() => void killTree(pid), graceMs);
  child.once('exit', () => clearTimeout(timer));
}

// Kills the process, its group and every descendant, including those in other process groups
// (axx starts each app in its own group).
async function killTree(pid: number): Promise<void> {
  const pids = [...(await descendants(pid)), pid];
  signalGroup(pid, 'SIGKILL');
  for (const p of pids) {
    try {
      process.kill(p, 'SIGKILL');
    } catch {
      // already gone
    }
  }
}

function signalGroup(pgid: number, signal: NodeJS.Signals): void {
  try {
    process.kill(-pgid, signal);
  } catch {
    try {
      process.kill(pgid, signal);
    } catch {
      // already gone
    }
  }
}

function descendants(pid: number): Promise<number[]> {
  return new Promise((resolve) => {
    execFile('ps', ['-A', '-o', 'pid=', '-o', 'ppid='], (err, stdout) => {
      if (err) {
        resolve([]);
        return;
      }
      const children = new Map<number, number[]>();
      for (const line of stdout.split('\n')) {
        const [child, parent] = line.trim().split(/\s+/).map(Number);
        if (child > 0 && parent > 0) children.set(parent, [...(children.get(parent) ?? []), child]);
      }
      const found: number[] = [];
      const stack = [pid];
      while (stack.length > 0) {
        for (const c of children.get(stack.pop()!) ?? []) {
          found.push(c);
          stack.push(c);
        }
      }
      resolve(found);
    });
  });
}

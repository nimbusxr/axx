// SPDX-License-Identifier: Apache-2.0
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { test } from "node:test";
import { Stamp, day, released, stamped } from "./stamp.mjs";

const require = createRequire(import.meta.url);
const { Version } = require("release-please/build/src/version");
const { buildStrategy } = require("release-please/build/src/factory");
const { TagName } = require("release-please/build/src/util/tag-name");

const v = (s) => Version.parse(s);
const quiet = { error() {}, warn() {}, info() {}, debug() {}, trace() {} };

test("a day is a number, in UTC", () => {
  assert.equal(day(new Date("2026-11-21T23:59:59Z")), 20261121);
  assert.equal(day(new Date("2026-01-05T00:00:00Z")), 20260105);
});

test("a version is axx's major and minor, the day and the release's number that day", () => {
  assert.equal(stamped(v("0.8.4"), v("0.7.2026112001"), 20261121), "0.8.2026112101");
  assert.equal(stamped(v("0.8.4"), v("0.8.2026112001"), 20261121), "0.8.2026112101");
  // A second release the same day.
  assert.equal(stamped(v("0.8.4"), v("0.8.2026112101"), 20261121), "0.8.2026112102");
  // The versions release-please counted, before the stamps.
  assert.equal(stamped(v("0.7.9"), v("0.3.12"), 20261121), "0.7.2026112101");
  assert.equal(stamped(v("1.0.0"), undefined, 20270104), "1.0.2027010401");
  assert.throws(() => stamped(v("0.8.4"), v("0.8.2026112199"), 20261121), /99 releases a day/);
});

test("versions sort as they were released", () => {
  const order = ["0.3.12", "0.7.2026112101", "0.7.2026112102", "0.7.2026112201", "0.8.2026112201", "1.0.2027010401"];
  assert.deepEqual([...order].reverse().sort((a, b) => v(a).compare(v(b))), order);
  // The third number fits the 32 bits a store may keep it in, for this century and the next.
  assert.ok(2147123199 < 2 ** 31);
});

test("a component is released when it changed, or axx is on another major or minor", () => {
  assert.equal(released(v("0.8.4"), v("0.8.2026112001"), false), false);
  assert.equal(released(v("0.8.4"), v("0.8.2026112001"), true), true);
  assert.equal(released(v("0.9.0"), v("0.8.2026112001"), false), true);
  assert.equal(released(v("1.0.0"), v("0.9.2026112001"), false), true);
  assert.equal(released(v("0.8.4"), undefined, false), true);
});

// release-please itself, with the plugin between its commits and its release pull requests: no
// GitHub, the strategies it builds for the packages below.
async function plan({ axx, commits, latest, now }) {
  const github = { repository: { owner: "nimbusxr", repo: "axx", defaultBranch: "main" } };
  const packages = {
    ".": { releaseType: "simple", bumpMinorPreMajor: true, bumpPatchForMinorPreMajor: true },
    "ide/vscode": { releaseType: "simple", component: "vscode", includeComponentInTag: true },
    "extensions/axx-wiremock": { releaseType: "simple", component: "axx-wiremock", includeComponentInTag: true },
  };
  const strategies = {}, releases = {};
  for (const [path, config] of Object.entries(packages)) {
    strategies[path] = await buildStrategy({ ...config, github, path, targetBranch: "main" });
    const version = path === "." ? axx : latest[path];
    if (version) {
      releases[path] = { tag: new TagName(v(version), config.component, "-", true), sha: "abc", notes: "" };
    }
  }
  const byPath = Object.fromEntries(Object.keys(packages).map((path) => [path, (commits[path] ?? []).map((message, i) => ({ sha: `${path}${i}`, message }))]));
  const plugin = new Stamp(github, "main", packages, ["vscode", "axx-wiremock"], { logger: quiet, now: () => new Date(now) });
  const planned = await plugin.preconfigure(strategies, byPath, releases);
  const { parseConventionalCommits } = require("release-please/build/src/commit");
  const versions = {};
  for (const path of Object.keys(packages)) {
    const pr = await planned[path].buildReleasePullRequest(parseConventionalCommits(byPath[path], quiet), releases[path]);
    versions[path] = pr?.version?.toString();
  }
  return versions;
}

test("release-please releases a changed component with its stamp, and leaves the others", async () => {
  assert.deepEqual(
    await plan({
      axx: "0.8.4",
      latest: { "ide/vscode": "0.8.2026111501", "extensions/axx-wiremock": "0.8.2026111501" },
      commits: { "ide/vscode": ["fix: the Testing view lists a feature once"] },
      now: "2026-11-21T10:00:00Z",
    }),
    { ".": undefined, "ide/vscode": "0.8.2026112101", "extensions/axx-wiremock": undefined },
  );
});

test("release-please releases every component when axx moves to another minor", async () => {
  assert.deepEqual(
    await plan({
      axx: "0.8.4",
      latest: { "ide/vscode": "0.8.2026111501", "extensions/axx-wiremock": "0.8.2026111501" },
      // A breaking change of axx's, before 1.0: its minor.
      commits: { ".": ["feat!: a run reads axx.yaml's services in the order it gives them"] },
      now: "2026-11-21T10:00:00Z",
    }),
    { ".": "0.9.0", "ide/vscode": "0.9.2026112101", "extensions/axx-wiremock": "0.9.2026112101" },
  );
});

test("release-please takes the components from the versions it counted to axx's", async () => {
  assert.deepEqual(
    await plan({
      axx: "0.7.9",
      latest: { "ide/vscode": "0.1.0", "extensions/axx-wiremock": "0.1.7" },
      commits: {},
      now: "2026-11-21T10:00:00Z",
    }),
    { ".": undefined, "ide/vscode": "0.7.2026112101", "extensions/axx-wiremock": "0.7.2026112101" },
  );
});

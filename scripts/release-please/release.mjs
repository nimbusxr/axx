// SPDX-License-Identifier: Apache-2.0
// release-please, with axx's stamped versions (stamp.mjs), as release.yml runs it:
//
//   node release.mjs release   # tag what a merged release pull request released, then open or
//                              # update the release pull request; writes the step's outputs
//   node release.mjs dry-run   # print the release pull request it would open; changes nothing
//
// GH_TOKEN, REPO (owner/name); TARGET_BRANCH (main): a dry run reads another branch's
// release-please-config.json and commits, a pull request's say.
//
// It stands where googleapis/release-please-action stood, which cannot load a plugin: the same
// library, the same two steps, the same outputs (release_created, tag_name, version; a
// component's behind its path and two dashes).
import { appendFileSync } from "node:fs";
import { createRequire } from "node:module";
import { Stamp, type } from "./stamp.mjs";

const require = createRequire(import.meta.url);
const { GitHub, Manifest, VERSION, registerPlugin } = require("release-please");

const config = "release-please-config.json";
const manifestFile = ".release-please-manifest.json";

registerPlugin(type, (options) =>
  new Stamp(options.github, options.targetBranch, options.repositoryConfig, options.type.components ?? [], { logger: options.logger }));

const mode = process.argv[2];
const token = process.env.GH_TOKEN;
const [owner, repo] = (process.env.REPO ?? "").split("/");
const branch = process.env.TARGET_BRANCH || "main";
if (!["release", "dry-run"].includes(mode) || !token || !owner || !repo) {
  console.error("usage: GH_TOKEN=... REPO=owner/name [TARGET_BRANCH=main] node release.mjs release|dry-run");
  process.exit(2);
}

console.log(`release-please ${VERSION} on ${owner}/${repo}@${branch}`);
const github = await GitHub.create({ owner, repo, token, defaultBranch: branch });
const load = () => Manifest.fromManifest(github, branch, config, manifestFile);

if (mode === "dry-run") {
  const prs = await (await load()).buildPullRequests();
  if (prs.length === 0) {
    console.log("No release pull request: nothing to release.");
  }
  for (const pr of prs) {
    console.log(`\nRelease pull request: ${pr.title}`);
    for (const release of pr.body.releaseData) {
      console.log(`  ${release.component || "axx"} ${release.version}`);
    }
    console.log("  files: " + pr.updates.map((u) => u.path).sort().join(", "));
    console.log("\n" + pr.body.toString());
  }
  process.exit(0);
}

const output = (path, key, value) => {
  const name = path === "." ? key : `${path}--${key}`;
  console.log(`${name}=${value}`);
  if (process.env.GITHUB_OUTPUT) {
    appendFileSync(process.env.GITHUB_OUTPUT, `${name}=${value}\n`);
  }
};
const releases = (await (await load()).createReleases()).filter(Boolean);
for (const release of releases) {
  const path = release.path || ".";
  output(path, "release_created", true);
  output(path, "tag_name", release.tagName);
  output(path, "version", release.version);
}
if (process.env.GITHUB_OUTPUT) {
  appendFileSync(process.env.GITHUB_OUTPUT, `paths_released=${JSON.stringify(releases.map((r) => r.path || "."))}\n`);
}
const prs = (await (await load()).createPullRequests()).filter(Boolean);
for (const pr of prs) {
  console.log(`release pull request #${pr.number}: ${pr.title}`);
}

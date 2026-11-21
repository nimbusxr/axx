// SPDX-License-Identifier: Apache-2.0
// The versions of what ships beside axx (the WireMock image, the editor plugins): axx's major and
// minor, then the day and which release of that day it is, as one number: 0.8.2026112101. Its
// first two numbers say which axx it is for; its third is a stamp, not a patch to match.
//
// release-please counts versions. This plugin, registered with it (release.mjs), gives it these
// instead: before release-please plans its release pull request, each component that changed
// gets its stamped version, and when axx moves to another major or minor, so does every
// component, changed or not.
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { ManifestPlugin } = require("release-please/build/src/plugin");
const { buildStrategy } = require("release-please/build/src/factory");
const { parseConventionalCommits } = require("release-please/build/src/commit");

/** The plugin's name in release-please-config.json's plugins. */
export const type = "axx-stamp";

/** The day as a number, 20261121, in UTC. */
export function day(now) {
  return now.getUTCFullYear() * 10000 + (now.getUTCMonth() + 1) * 100 + now.getUTCDate();
}

/** Whether a component is released: it changed, it never was, or axx is on another major.minor than its last release. */
export function released(axx, latest, changed) {
  return changed || !latest || latest.major !== axx.major || latest.minor !== axx.minor;
}

/**
 * A component's next version: axx's major and minor, then today and the release's number that
 * day, from 01. A day has 99 at most: the number is two digits of the stamp.
 */
export function stamped(axx, latest, today) {
  const sameLine = latest && latest.major === axx.major && latest.minor === axx.minor;
  const n = sameLine && Math.floor(latest.patch / 100) === today ? (latest.patch % 100) + 1 : 1;
  if (n > 99) {
    throw new Error(`a component has 99 releases a day at most: ${latest.major}.${latest.minor}.${latest.patch} is the 99th`);
  }
  return `${axx.major}.${axx.minor}.${today * 100 + n}`;
}

export class Stamp extends ManifestPlugin {
  /**
   * @param components the components' names (release-please-config.json's "component") that take
   *   axx's major and minor
   * @param now the day's clock, for tests
   */
  constructor(github, targetBranch, repositoryConfig, components, { logger, now = () => new Date() } = {}) {
    super(github, targetBranch, repositoryConfig, logger);
    this.components = new Set(components);
    this.now = now;
  }

  async preconfigure(strategiesByPath, commitsByPath, releasesByPath) {
    const next = async (path) => {
      const pr = await strategiesByPath[path].buildReleasePullRequest(
        parseConventionalCommits(commitsByPath[path] ?? [], this.logger),
        releasesByPath[path],
      );
      return pr?.version;
    };
    // axx is the root package: the version this release gives it, or the one it has.
    const root = ".";
    if (!strategiesByPath[root]) {
      throw new Error(`the ${type} plugin takes axx's version from the root package ("."), and release-please-config.json has none`);
    }
    const axx = (await next(root)) ?? releasesByPath[root]?.tag.version;
    if (!axx) {
      this.logger.warn(`${type}: axx has no version yet; the components keep release-please's`);
      return strategiesByPath;
    }
    const today = day(this.now());
    const strategies = { ...strategiesByPath };
    for (const path of Object.keys(strategiesByPath)) {
      const component = await strategiesByPath[path].getComponent();
      if (path === root || !component || !this.components.has(component)) {
        continue;
      }
      const latest = releasesByPath[path]?.tag.version;
      const changed = Boolean(await next(path));
      if (!released(axx, latest, changed)) {
        continue;
      }
      const version = stamped(axx, latest, today);
      this.logger.info(`${type}: ${component} is released as ${version} (axx ${axx.major}.${axx.minor}${changed ? "" : ", unchanged"})`);
      strategies[path] = await buildStrategy({
        ...this.repositoryConfig[path],
        github: this.github,
        path,
        targetBranch: this.targetBranch,
        releaseAs: version,
      });
      if (!changed) {
        // Nothing of the component changed: a release needs a line to say why there is one.
        (commitsByPath[path] ??= []).push({
          sha: "",
          message: `chore(${component}): released for axx ${axx.major}.${axx.minor}\n\nRelease-As: ${version}`,
        });
      }
    }
    return strategies;
  }
}

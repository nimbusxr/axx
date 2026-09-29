package lighthouse

import _ "embed"

// lighthouseVersion is the Lighthouse the pack runs (Apache-2.0, Google):
// downloaded from npm, never distributed with axx. npm/package-lock.json
// pins it and every package it loads, each with its integrity
// (TestTheLockfilePinsLighthouse checks that they agree); internal/npm
// installs them.
//
// Lighthouse drives the browser through its DevTools protocol, so its
// version must follow the Chromium of the Playwright the web-core pack drives
// (driver.CoreVersion): 13.5.0 audits Chromium 151, Playwright 1.62.1's.
// To upgrade it, change npm/package.json and run
// `npm install --package-lock-only --ignore-scripts` in npm/.
const lighthouseVersion = "13.5.0"

//go:embed npm/package-lock.json
var lockfile []byte

// unused are the packages Lighthouse depends on but never loads here, left
// out with those only they need: Sentry sends Lighthouse's errors to its
// authors when asked to, which axx never does.
var unused = []string{"@sentry/node"}

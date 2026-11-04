// SPDX-License-Identifier: Apache-2.0
// Bundles the extension into dist/extension.js.
//   node esbuild.mjs               development build with a source map
//   node esbuild.mjs --watch       rebuild on change
//   node esbuild.mjs --production  minified, no source map (used by vsce)
//   node esbuild.mjs --tests       the unit tests in test/unit, to dist/test/*.test.mjs
import { cpSync } from 'node:fs';
import * as esbuild from 'esbuild';

const production = process.argv.includes('--production');
const watch = process.argv.includes('--watch');

// axx's JSON Schemas, which package.json's yamlValidation gives Red Hat's YAML extension: axx.yaml
// has completion with no network.
cpSync(new URL('../../schemas/v0/', import.meta.url), new URL('dist/schemas/', import.meta.url), { recursive: true });

if (process.argv.includes('--tests')) {
  // The units under test don't import 'vscode', so they run in plain Node.js.
  await esbuild.build({
    entryPoints: ['test/unit/*.test.ts'],
    outdir: 'dist/test',
    outExtension: { '.js': '.mjs' },
    bundle: true,
    format: 'esm',
    platform: 'node',
    target: 'node20',
    packages: 'external',
    logLevel: 'warning',
  });
  process.exit(0);
}

const options = {
  entryPoints: ['src/extension.ts'],
  outfile: 'dist/extension.js',
  bundle: true,
  format: 'cjs',
  platform: 'node',
  target: 'node20', // the Node.js of the oldest supported VS Code (engines.vscode)
  external: ['vscode'],
  minify: production,
  sourcemap: !production,
  sourcesContent: false,
  logLevel: 'info',
};

if (watch) {
  await (await esbuild.context(options)).watch();
} else {
  await esbuild.build(options);
}

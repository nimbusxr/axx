// SPDX-License-Identifier: Apache-2.0
// Checks that package.json and the files it points to agree.
import assert from 'node:assert/strict';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { test } from 'node:test';

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');
const sources = () =>
  readdirSync(new URL('../src/', import.meta.url))
    .filter((f) => f.endsWith('.ts'))
    .map((f) => read(`src/${f}`))
    .join('\n');
const pkg = JSON.parse(read('package.json'));
const { contributes } = pkg;

test('the language, grammar and language configuration files exist and parse', () => {
  const [language] = contributes.languages;
  assert.equal(language.id, 'feature');
  assert.deepEqual(language.extensions, ['.feature']);
  const config = JSON.parse(read(language.configuration));
  assert.equal(config.comments.lineComment, '#');
  assert.ok(config.autoClosingPairs.some((p) => p.open === '"""' && p.close === '"""'));
  for (const pattern of Object.values(config.indentationRules)) {
    new RegExp(pattern); // throws if VS Code could not compile it either
  }

  const [grammar] = contributes.grammars;
  assert.equal(grammar.language, 'feature');
  assert.equal(JSON.parse(read(grammar.path)).scopeName, grammar.scopeName);
});

test('the extension activates for feature files and axx projects', () => {
  assert.ok(pkg.activationEvents.includes('onLanguage:feature'));
  assert.ok(pkg.activationEvents.includes('workspaceContains:**/axx.yaml'));
});

test('every contributed command is registered by the extension', () => {
  const source = sources();
  for (const { command } of contributes.commands) {
    assert.ok(source.includes(`'${command}'`), `${command} is not registered in src/`);
  }
});

test('settings read by the extension are contributed', () => {
  const properties = contributes.configuration.properties;
  assert.equal(properties['axx.path'].default, 'axx');
  // vscode-languageclient reads `<client id>.trace.server`; the client id is 'axx'.
  assert.ok(properties['axx.trace.server']);
  assert.match(sources(), /new LanguageClient\('axx',/);
});

test('the bundle entry point is what the build writes', () => {
  assert.equal(pkg.main, './dist/extension.js');
  assert.match(read('esbuild.mjs'), /outfile: 'dist\/extension\.js'/);
});

test('dependencies are pinned', () => {
  for (const deps of [pkg.dependencies, pkg.devDependencies]) {
    for (const [name, version] of Object.entries(deps)) {
      assert.match(version, /^\d+\.\d+\.\d+$/, `${name} is not pinned: ${version}`);
    }
  }
  assert.ok(existsSync(new URL('../package-lock.json', import.meta.url)));
});

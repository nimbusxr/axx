// SPDX-License-Identifier: Apache-2.0
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { test } from 'node:test';
import { configDir, kindOf, ProjectSchemas, runCommand, SCHEME, type Runner } from '../../src/schemas';

const bundled = path.resolve('dist', 'schemas'); // npm test runs in the package's folder

// A project with axx.yaml, and a file in it.
function project(): { dir: string; file: string } {
  const dir = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'axx-schemas-')));
  fs.writeFileSync(path.join(dir, 'axx.yaml'), 'version: 1\n');
  fs.mkdirSync(path.join(dir, 'fixtures'));
  return { dir, file: path.join(dir, 'axx.yaml') };
}

// An axx that says it is version, and whose axx.yaml schema has the key fromTheProjectsAxx; with
// fail, `axx schema` fails. Records what it is asked.
function fakeAxx(version: () => string, calls: string[], fail = false): Runner {
  return async (_command, args) => {
    calls.push(args.join(' '));
    if (args[0] === '--version') return `axx ${version()} (test)\n`;
    if (args[0] === 'schema' && !fail) {
      const kind = args[2];
      fs.writeFileSync(args[4], kind === 'config' ? '{"properties":{"fromTheProjectsAxx":{}}}' : '{"type":"object"}');
      return '';
    }
    return undefined;
  };
}

test('a file gets the schema of its kind, by its name', () => {
  assert.equal(kindOf('axx.yaml')?.kind, 'config');
  assert.equal(kindOf('axx.ci.yml')?.kind, 'config');
  assert.equal(kindOf('registrations.factory.yaml')?.kind, 'factory');
  assert.equal(kindOf('parcels.fixture.yml')?.kind, 'fixture');
  assert.equal(kindOf('parcel.prototype.yaml')?.kind, 'prototype');
  assert.equal(kindOf('compose.yaml'), undefined);
});

test('a file belongs to the nearest axx.yaml above it', () => {
  const { dir } = project();
  assert.equal(configDir(path.join(dir, 'fixtures', 'parcels.fixture.yaml')), dir);
  assert.equal(configDir(path.join(dir, 'axx.yaml')), dir);
});

test('axx.yaml gets the schema of the axx the project runs on, once that axx gave it', async () => {
  const { dir, file } = project();
  const calls: string[] = [];
  const schemas = new ProjectSchemas(bundled, () => '/bin/axx', fakeAxx(() => 'v9.9.9', calls));
  assert.equal(schemas.uri(file), `${SCHEME}://bundled/axx.schema.json`);
  assert.match(schemas.content(`${SCHEME}://bundled/axx.schema.json`) ?? '', /"services"/);
  await schemas.refresh(dir);
  const uri = schemas.uri(file);
  assert.match(uri ?? '', new RegExp(`^${SCHEME}://axx/[0-9a-f]+/axx.schema.json$`));
  assert.match(schemas.content(uri!) ?? '', /fromTheProjectsAxx/);
  assert.equal(schemas.uri(path.join(dir, 'compose.yaml')), undefined);
});

test('the same axx is asked for its schemas once; another one again', async () => {
  const { dir, file } = project();
  const calls: string[] = [];
  let version = 'v9.9.9';
  const changed: string[] = [];
  const schemas = new ProjectSchemas(bundled, () => '/bin/axx', fakeAxx(() => version, calls), (d) => changed.push(d));
  await schemas.refresh(dir);
  const first = schemas.uri(file);
  await schemas.refreshAll();
  assert.equal(calls.filter((c) => c.startsWith('schema')).length, 4);
  assert.equal(schemas.uri(file), first);
  version = 'v10.0.0';
  await schemas.refreshAll();
  assert.equal(calls.filter((c) => c.startsWith('schema')).length, 8);
  assert.notEqual(schemas.uri(file), first);
  assert.deepEqual(changed, [dir, dir]);
});

test('an axx that cannot give its schemas, or none at all, leaves this extension\'s', async () => {
  const { dir, file } = project();
  const failing = new ProjectSchemas(bundled, () => '/bin/axx', fakeAxx(() => 'v9.9.9', [], true));
  await failing.refresh(dir);
  assert.equal(failing.uri(file), `${SCHEME}://bundled/axx.schema.json`);
  const missing = new ProjectSchemas(bundled, () => undefined, fakeAxx(() => 'v9.9.9', []));
  await missing.refresh(dir);
  assert.equal(missing.uri(file), `${SCHEME}://bundled/axx.schema.json`);
});

test('the window coming back checks at most every 30 seconds', async () => {
  const { dir } = project();
  const calls: string[] = [];
  const schemas = new ProjectSchemas(bundled, () => '/bin/axx', fakeAxx(() => 'v9.9.9', calls));
  await schemas.refresh(dir);
  await schemas.refreshOnFront(100_000);
  await schemas.refreshOnFront(110_000);
  await schemas.refreshOnFront(131_000);
  assert.equal(calls.filter((c) => c === '--version').length, 3);
});

test('a real process: an axx script writes its schemas', { skip: process.platform === 'win32' }, async () => {
  const { dir, file } = project();
  const axx = path.join(dir, 'axx');
  fs.writeFileSync(
    axx,
    [
      '#!/bin/sh',
      'case "$1" in',
      '  --version) echo "axx v9.9.9 (test)" ;;',
      `  schema) printf '%s' '{"properties":{"fromTheProjectsAxx":{}}}' > "$5" ;;`,
      '  *) exit 2 ;;',
      'esac',
    ].join('\n'),
    { mode: 0o755 },
  );
  const schemas = new ProjectSchemas(bundled, () => axx, runCommand);
  await schemas.refresh(dir);
  assert.match(schemas.content(schemas.uri(file)!) ?? '', /fromTheProjectsAxx/);
});

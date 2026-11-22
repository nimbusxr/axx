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
// fail, `axx schema` fails; with early, it is from before the overlays had a schema of their own,
// and fails for that kind. Records what it is asked.
function fakeAxx(version: () => string, calls: string[], fail = false, early = false): Runner {
  return async (_command, args) => {
    calls.push(args.join(' '));
    if (args[0] === '--version') return `axx ${version()} (test)\n`;
    if (args[0] === 'schema' && !fail && !(early && args[2] === 'overlay')) {
      const kind = args[2];
      fs.writeFileSync(args[4], kind === 'config' ? '{"properties":{"fromTheProjectsAxx":{}}}' : '{"type":"object"}');
      return '';
    }
    return undefined;
  };
}

test('a file gets the schema of its kind, by its name', () => {
  assert.equal(kindOf('axx.yaml')?.kind, 'config');
  assert.equal(kindOf('axx.yml')?.kind, 'config');
  assert.equal(kindOf('axx.ci.yml')?.kind, 'overlay');
  assert.equal(kindOf('axx.local.yaml')?.kind, 'overlay');
  assert.equal(kindOf('axx.a.b.yaml'), undefined);
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
  assert.equal(calls.filter((c) => c.startsWith('schema')).length, 5);
  assert.equal(schemas.uri(file), first);
  version = 'v10.0.0';
  await schemas.refreshAll();
  assert.equal(calls.filter((c) => c.startsWith('schema')).length, 10);
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

test('a profile\'s file gets the overlays\' schema, which requires nothing that axx.yaml says already', () => {
  const { dir } = project();
  const schemas = new ProjectSchemas(bundled, () => undefined);
  const uri = schemas.uri(path.join(dir, 'axx.apollo.yaml'));
  assert.equal(uri, `${SCHEME}://bundled/axx-overlay.schema.json`);
  const overlay = JSON.parse(schemas.content(uri!) ?? '{}');
  const config = JSON.parse(schemas.content(`${SCHEME}://bundled/axx.schema.json`) ?? '{}');
  // A service in axx.yaml has a command; one a profile's file switches off need not say it again.
  assert.deepEqual(config.$defs.Services.additionalProperties.required, ['command']);
  assert.equal(overlay.$defs.Services.additionalProperties.required, undefined);
  assert.deepEqual(
    Object.keys(overlay.$defs.Services.additionalProperties.properties).sort(),
    Object.keys(config.$defs.Services.additionalProperties.properties).sort(),
  );
});

test('an axx from before the overlays had a schema gives its others, and this extension\'s is used for overlays', async () => {
  const { dir, file } = project();
  const schemas = new ProjectSchemas(bundled, () => '/bin/axx', fakeAxx(() => 'v0.7.0', [], false, true));
  await schemas.refresh(dir);
  assert.match(schemas.content(schemas.uri(file)!) ?? '', /fromTheProjectsAxx/);
  const overlay = schemas.content(schemas.uri(path.join(dir, 'axx.ci.yaml'))!) ?? '';
  assert.match(overlay, /axx-overlay\.schema\.json/);
  assert.match(overlay, /"services"/);
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

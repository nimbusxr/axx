// SPDX-License-Identifier: Apache-2.0
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { test } from 'node:test';
import { findProfiles, profilesFor } from '../../src/profiles';

function project(files: Record<string, string>): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'axx-profiles-'));
  for (const [name, content] of Object.entries(files)) fs.writeFileSync(path.join(dir, name), content);
  return dir;
}

test('finds the profiles of axx.yaml in order, then the axx.<name>.yaml files', () => {
  const dir = project({
    'axx.yaml': [
      'version: 1',
      'profiles:',
      '  # one per platform',
      '  web:',
      '    run: {uses: [web-core]}',
      '',
      '  ios:',
      '    run:',
      '      uses: [mobile-ios]',
      '  "android": {run: {uses: [mobile-android]}}',
      'apps:',
      '  parcels:',
      '    command: ./parcels',
    ].join('\n'),
    'axx.watch.yaml': 'run: {watch: true}\n',
    'axx.ci.yaml': 'run: {workers: 4}\n',
    'axx.ios.yaml': 'run: {workers: 1}\n',
    'axx.local.yaml': 'run: {workers: 2}\n',
    'axx-packs.yaml': 'packs: [rest]\n',
  });
  assert.deepEqual(findProfiles(dir), ['web', 'ios', 'android', 'ci', 'watch']);
});

test('reads a one-line profiles mapping and axx.yml', () => {
  const dir = project({ 'axx.yml': 'profiles: {web: {run: {uses: [web-core]}}, ios: {}}  # platforms\n' });
  assert.deepEqual(findProfiles(dir), ['web', 'ios']);
});

test('has none without profiles', () => {
  assert.deepEqual(findProfiles(project({ 'axx.yaml': 'version: 1\nrun:\n  paths: [features]\n' })), []);
  assert.deepEqual(findProfiles(path.join(os.tmpdir(), 'axx-profiles-missing')), []);
});

test("leaves out the chosen profiles a project doesn't have", () => {
  assert.deepEqual(profilesFor(['ios', 'watch'], ['web', 'ios', 'watch']), { apply: ['ios', 'watch'], missing: [] });
  assert.deepEqual(profilesFor(['ios', 'watch'], ['watch']), { apply: ['watch'], missing: ['ios'] });
});

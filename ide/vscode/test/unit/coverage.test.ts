// SPDX-License-Identifier: Apache-2.0
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { covered, parseLcov } from '../../src/coverage';
import { parseIdeAnnouncement } from '../../src/ideMarker';
import { runArguments } from '../../src/browsers';

test('parseLcov reads what axx writes, and what other tools do', () => {
  const files = parseLcov(
    [
      'TN:',
      'SF:src/parcels.go',
      'FN:3,ship',
      'FN:9,12,close',
      'FNDA:4,ship',
      'FNDA:0,close',
      'DA:3,4',
      'DA:4,0',
      'BRDA:4,0,0,1',
      'BRDA:4,0,1,-',
      'end_of_record',
      'SF:src/parcels.go',
      'DA:4,2',
      'end_of_record',
      'SF:/abs/other.js',
      'DA:1,1',
      'end_of_record',
      '',
    ].join('\n'),
  );
  assert.equal(files.length, 2);
  const [parcels, other] = files;
  assert.equal(parcels.path, 'src/parcels.go');
  assert.deepEqual([...parcels.lines], [[3, 4], [4, 2]]);
  assert.deepEqual(parcels.functions, [{ name: 'ship', line: 3, hits: 4 }, { name: 'close', line: 9, hits: 0 }]);
  assert.deepEqual(parcels.branches.get(4), [1, 0]);
  assert.deepEqual(covered(parcels), { lines: [2, 2], branches: [1, 2], functions: [1, 2] });
  assert.equal(other.path, '/abs/other.js');
  assert.deepEqual(covered(other), { lines: [1, 1], branches: [0, 0], functions: [0, 0] });
});

test('the coverage announcement names the run lcov report', () => {
  assert.deepEqual(parseIdeAnnouncement('[AXX-IDE] coverage path=/work/shop/.axx/coverage/lcov.info'), {
    kind: 'coverage',
    path: '/work/shop/.axx/coverage/lcov.info',
  });
  assert.equal(parseIdeAnnouncement('[AXX-IDE] coverage path=relative/lcov.info'), undefined);
});

test('a run with coverage asks axx for it', () => {
  assert.deepEqual(runArguments(['features/a.feature'], { coverage: true }), ['run', '--format', 'teamcity', '--coverage', 'features/a.feature']);
});

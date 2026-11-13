// Records how @jridgewell/resolve-uri 3.1.2 (trace-mapping's) names the
// sources of a source map with no URL of its own: resolve(input, ""). Run
// once, never by hand:
//
//   npm install @jridgewell/resolve-uri@3.1.2
//   node record-uri.js > uri.json
'use strict';
const resolve = require('@jridgewell/resolve-uri');

const inputs = [
  '', 'a.ts', './a.ts', '../a.ts', '../../src/a.ts', './src/../a.ts', 'src/./a.ts', 'src//a.ts',
  'src/', 'src/..', '..', '.', './', '/a.ts', '/src/../a.ts', '/../a.ts', '//cdn.example.com/a.ts',
  'webpack://app/./src/a.ts', 'webpack:///./src/a.ts?1234', 'http://example.com/src/../a.ts',
  'https://example.com:8443/a/b/../c.ts#x', 'file:///home/dev/a.ts', 'file://host/a.ts', 'file:a.ts',
  'file://c:/a.ts', '?q', '#h', 'a.ts?v=2', 'src/a.ts#top', 'src/../../a.ts', 'a/b/../../../c.ts',
];
process.stdout.write(JSON.stringify(inputs.map((i) => [i, resolve(i, '')]), null, 1) + '\n');

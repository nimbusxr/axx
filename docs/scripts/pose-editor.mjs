import { mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { isAxePose } from '../src/lib/axe-pose.mjs';

// Local development only. Store the chosen pose where both the editor and Codex
// can read it, without changing the public animation or committing draft values.
export function poseEditor() {
 const directory = new URL('../.astro/', import.meta.url);
 const file = new URL('axe-pose.json', directory);
 let writes = Promise.resolve();
 return {
  name: 'axx-pose-editor',
  apply: 'serve',
  config: () => ({ server: { watch: { ignored: ['**/.astro/axe-pose.json*'] } } }),
  configureServer(server) {
   server.middlewares.use('/__axe-pose', async (request, response) => {
    const send = (status, body) => {
     response.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
     response.end(JSON.stringify(body));
    };
    try {
     if (request.method === 'GET') {
      try { send(200, JSON.parse(await readFile(file, 'utf8'))); }
      catch (error) { if (error.code === 'ENOENT') send(200, { pose: null }); else throw error; }
      return;
     }
     if (request.method !== 'PUT') { send(405, { error: 'Use GET or PUT.' }); return; }
     const origin = request.headers.origin;
     if (!origin || new URL(origin).host !== request.headers.host || !request.headers['content-type']?.startsWith('application/json')) {
      send(403, { error: 'Save from the local pose editor.' }); return;
     }
     let body = '';
     for await (const chunk of request) {
      body += chunk;
      if (body.length > 4096) { send(413, { error: 'Pose is too large.' }); return; }
     }
     let pose;
     try { pose = JSON.parse(body); }
     catch { send(400, { error: 'Invalid JSON.' }); return; }
     if (!isAxePose(pose)) { send(400, { error: 'Invalid pose values.' }); return; }
     const saved = { pose, savedAt: new Date().toISOString() };
     const write = writes.then(async () => {
      await mkdir(directory, { recursive: true });
      const temporary = new URL('axe-pose.json.tmp', directory);
      await writeFile(temporary, `${JSON.stringify(saved, null, 2)}\n`);
      await rename(temporary, file);
     });
     writes = write.catch(() => {});
     await write;
     send(200, saved);
    } catch {
     send(500, { error: 'Could not read or save the pose.' });
    }
   });
  },
 };
}

import assert from 'node:assert/strict';
import {createServer, type IncomingMessage, type ServerResponse} from 'node:http';
import {test, type TestContext} from 'node:test';
import {DaemonClient} from '../src/daemon.js';
import {AdapterError} from '../src/core.js';

async function client(t: TestContext, handle: (request: IncomingMessage, response: ServerResponse) => void, timeoutMs = 1000) {
  const server = createServer(handle);
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise<void>((resolve) => { server.close(() => resolve()); server.closeAllConnections(); }));
  return new DaemonClient({url: `http://127.0.0.1:${(server.address() as {port: number}).port}`, timeoutMs, sessionId: 'trusted-session'});
}

test('response interruption after write admission remains explicitly uncertain', async (t) => {
  const daemon = await client(t, (_request, response) => {
    response.writeHead(200, {'Content-Type': 'application/json'});
    response.flushHeaders();
    response.write('{"admitted":');
  }, 100);
  await assert.rejects(daemon.post('/api/v1/instructor/commands', {command: 'weiter'}), (error: unknown) =>
    error instanceof AdapterError && error.code === 'DAEMON_UNAVAILABLE' && /may have been admitted/.test(error.message));
});

test('malformed successful write response retains the same uncertainty contract', async (t) => {
  const daemon = await client(t, (_request, response) => response.end('not json'));
  await assert.rejects(daemon.put('/api/v1/sessions/s/workspace/file', {}), (error: unknown) =>
    error instanceof AdapterError && error.code === 'DAEMON_CONTRACT' && /inspect its state/.test(error.message));
});

test('daemon redirects are refused and trusted session identity comes from the host', async (t) => {
  let redirected = 0;
  const daemon = await client(t, (request, response) => {
    assert.equal(request.headers['x-ao-session-id'], 'trusted-session');
    if (request.url === '/redirect-target') redirected++;
    response.writeHead(302, {Location: '/redirect-target'}).end();
  });
  await assert.rejects(daemon.get('/healthz'), (error: unknown) => error instanceof AdapterError && error.code === 'DAEMON_UNAVAILABLE');
  assert.equal(redirected, 0);
});

test('oversized daemon write response is bounded and cannot imply a safe retry', async (t) => {
  const daemon = await client(t, (_request, response) => response.end(JSON.stringify({content: 'x'.repeat(4 * 1024 * 1024)})));
  await assert.rejects(daemon.post('/api/v1/instructor/commands', {}), (error: unknown) =>
    error instanceof AdapterError && error.code === 'RESPONSE_TOO_LARGE' && /may have been admitted/.test(error.message));
});

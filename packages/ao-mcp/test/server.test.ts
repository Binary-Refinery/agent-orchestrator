import assert from 'node:assert/strict';
import {createServer, request as httpRequest} from 'node:http';
import {mkdtemp, writeFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {test, type TestContext} from 'node:test';
import {Client, StreamableHTTPClientTransport} from '@modelcontextprotocol/client';
import {StdioClientTransport} from '@modelcontextprotocol/client/stdio';
import {configSchema} from '../src/config.js';
import {startHttp} from '../src/http.js';
import {record} from '../src/core.js';
import type {AuditEvent} from '../src/server.js';

async function setup(t: TestContext) {
  const calls: string[] = [];
  const daemon = createServer((request, response) => {
    calls.push(request.url ?? '');
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/healthz') response.end(JSON.stringify({status: 'ok', service: 'ao', pid: 1234, version: 'test-build'}));
    else if (request.url === '/readyz') response.end(JSON.stringify({status: 'ready'}));
    else if (request.url === '/api/v1/instructor/status') response.end(JSON.stringify({id: 'snapshot-one', works: [
      {id: 'work-one', projectId: 'project-one', goal: 'private task', evidence: [], launches: []},
      {id: 'foreign', projectId: 'other-project', goal: 'hidden-foreign-secret', evidence: [], launches: []},
    ]}));
    else response.writeHead(404).end(JSON.stringify({code: 'ROUTE_NOT_FOUND', requestId: 'real-request-id', message: 'Missing route.'}));
  });
  await new Promise<void>((resolve) => daemon.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise<void>((resolve) => {daemon.close(() => resolve()); daemon.closeAllConnections();}));
  const daemonUrl = `http://127.0.0.1:${(daemon.address() as {port: number}).port}`;
  const config = configSchema.parse({daemonUrl, transport: 'http', access: {principal: 'test-host', projectIds: ['project-one']}, http: {port: 3334, tokenEnv: 'AO_MCP_TOKEN'}});
  const token = 'a'.repeat(64);
  const audit: AuditEvent[] = [];
  const http = await startHttp(config, token, (event) => audit.push(event), 0);
  t.after(() => http.close());
  return {config, token, audit, calls, url: http.url};
}

async function rpc(url: string, token: string, message: unknown, headers: Record<string, string> = {}) {
  const response = await fetch(url, {method: 'POST', headers: {
    Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream',
    'MCP-Protocol-Version': '2025-11-25', ...headers,
  }, body: typeof message === 'string' ? message : JSON.stringify(message)});
  const text = await response.text();
  const data = text.startsWith('event:') || text.startsWith('data:')
    ? JSON.parse(text.split('\n').filter((line) => line.startsWith('data:')).at(-1)!.slice(5))
    : text ? JSON.parse(text) : undefined;
  return {response, data};
}

async function rawStatus(url: string, headers: Record<string, string>, body: string): Promise<number> {
  return new Promise((resolve, reject) => {
    const request = httpRequest(url, {method: 'POST', headers}, (response) => {
      response.resume();
      response.once('end', () => resolve(response.statusCode!));
    });
    request.once('error', reject);
    request.end(body);
  });
}

test('independent modern SDK and raw legacy clients discover the same scoped tools', async (t) => {
  const f = await setup(t);
  const client = new Client({name: 'modern-protocol-probe', version: '1.0'});
  await client.connect(new StreamableHTTPClientTransport(new URL(f.url), {requestInit: {headers: {Authorization: `Bearer ${f.token}`}}}));
  t.after(() => client.close());
  const listed = await client.listTools();
  assert.ok(listed.tools.some((tool) => tool.name === 'ao_work_trace'));
  assert.ok(!listed.tools.some((tool) => tool.name === 'ao_work_command' || tool.name === 'ao_workspace_update'));
  const status = await client.callTool({name: 'ao_instructor_status', arguments: {}});
  assert.equal(status.isError, undefined);
  assert.ok(!JSON.stringify(status).includes('hidden-foreign-secret'));
  const initialized = await rpc(f.url, f.token, {jsonrpc: '2.0', id: 1, method: 'initialize', params: {
    protocolVersion: '2025-11-25', capabilities: {}, clientInfo: {name: 'raw-protocol-probe', version: '1.0'},
  }});
  assert.equal(initialized.data.result.protocolVersion, '2025-11-25');
  const notice = await rpc(f.url, f.token, {jsonrpc: '2.0', method: 'notifications/initialized'});
  assert.equal(notice.response.status, 202);
  const legacyList = await rpc(f.url, f.token, {jsonrpc: '2.0', id: 2, method: 'tools/list', params: {}});
  assert.deepEqual(legacyList.data.result.tools.map((tool: {name: string}) => tool.name).sort(), listed.tools.map((tool) => tool.name).sort());
  const health = await rpc(f.url, f.token, {jsonrpc: '2.0', id: 3, method: 'tools/call', params: {name: 'ao_runtime_info', arguments: {}}});
  assert.equal(health.data.result.structuredContent.result.health.pid, 1234);
  assert.equal(f.audit.at(-1)?.principal, 'test-host');
  assert.ok(!JSON.stringify(f.audit).includes('private task'));
});

test('HTTP rejects missing credentials, forged host/origin and oversized bodies before AO calls', async (t) => {
  const f = await setup(t);
  const body = JSON.stringify({jsonrpc: '2.0', id: 1, method: 'tools/list', params: {}});
  assert.equal((await fetch(f.url, {method: 'POST', body})).status, 401);
  assert.equal((await fetch(f.url, {method: 'POST', headers: {Authorization: 'Bearer wrong'}, body})).status, 401);
  // fetch controls Host in some runtimes; a raw HTTP request exercises rebinding directly.
  assert.equal(await rawStatus(f.url, {Authorization: `Bearer ${f.token}`, Host: 'evil.example'}, body), 403);
  assert.equal((await fetch(f.url, {method: 'POST', headers: {Authorization: `Bearer ${f.token}`, Origin: 'https://evil.example'}, body})).status, 403);
  const oversized = await fetch(f.url, {method: 'POST', headers: {
    Authorization: `Bearer ${f.token}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream',
  }, body: 'x'.repeat(1024 * 1024 + 1)});
  assert.equal(oversized.status, 413);
  assert.deepEqual(f.calls, []);
});

test('invalid MCP requests, unknown tools and authority injection never reach AO', async (t) => {
  const f = await setup(t);
  const malformed = await rpc(f.url, f.token, '{invalid');
  assert.equal(malformed.response.status, 400);
  const unknown = await rpc(f.url, f.token, {jsonrpc: '2.0', id: 5, method: 'unknown/method', params: {}});
  assert.equal(unknown.data.error.code, -32601);
  const forbidden = await rpc(f.url, f.token, {jsonrpc: '2.0', id: 6, method: 'tools/call', params: {name: 'ao_work_command', arguments: {command: 'go'}}});
  assert.ok(forbidden.data.error || forbidden.data.result?.isError);
  const injection = await rpc(f.url, f.token, {jsonrpc: '2.0', id: 7, method: 'tools/call', params: {name: 'ao_runtime_info', arguments: {approval: 'GO'}}});
  assert.ok(injection.data.error || injection.data.result?.isError);
  assert.deepEqual(f.calls, []);
});

test('tool inventory conservatively describes file replacement and repository execution', async (t) => {
  const f = await setup(t);
  const config = configSchema.parse({...f.config, access: {...f.config.access, role: 'developer'}});
  const http = await startHttp(config, f.token, () => {}, 0);
  t.after(() => http.close());
  const inventory = await rpc(http.url, f.token, {jsonrpc: '2.0', id: 1, method: 'tools/list', params: {}});
  const tools = inventory.data.result.tools as {name: string; annotations: Record<string, boolean>}[];
  for (const name of ['ao_workspace_update', 'ao_run_checks', 'ao_work_command']) {
    const tool = tools.find((item) => item.name === name)!;
    assert.equal(tool.annotations.readOnlyHint, false);
    assert.equal(tool.annotations.destructiveHint, true);
    assert.equal(tool.annotations.openWorldHint, true);
  }
  assert.equal(tools.find((tool) => tool.name === 'ao_work_trace')!.annotations.readOnlyHint, true);
  assert.deepEqual(f.calls, []);
});

test('stdio CLI speaks MCP without mixing logs into stdout and keeps daemon errors intact', async (t) => {
  const f = await setup(t);
  const directory = await mkdtemp(join(tmpdir(), 'ao-mcp-client-'));
  t.after(() => rm(directory, {recursive: true, force: true}));
  const configPath = join(directory, 'config.json');
  await writeFile(configPath, JSON.stringify({...f.config, transport: 'stdio'}));
  const transport = new StdioClientTransport({command: process.execPath, args: [fileURLToPath(new URL('../src/cli.js', import.meta.url)), '--config', configPath], stderr: 'pipe'});
  const client = new Client({name: 'stdio-protocol-probe', version: '1.0'});
  t.after(() => client.close());
  await client.connect(transport);
  const health = await client.callTool({name: 'ao_runtime_info', arguments: {}});
  assert.equal(record(record(health.structuredContent).result).version, 'test-build');
  const missing = await client.callTool({name: 'ao_work_trace', arguments: {workId: 'missing'}});
  assert.equal(missing.isError, true);
  const error = record(record(missing.structuredContent).error);
  assert.equal(error.code, 'ROUTE_NOT_FOUND');
  assert.equal(error.requestId, 'real-request-id');
});

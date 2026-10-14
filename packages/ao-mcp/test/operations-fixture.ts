import {createServer, type IncomingMessage, type ServerResponse} from 'node:http';
import type {AddressInfo} from 'node:net';
import type {TestContext} from 'node:test';
import {DaemonClient} from '../src/daemon.js';
import {operationTools} from '../src/operations.js';
import type {Access} from '../src/core.js';

export interface Call {method: string; path: string; body?: Record<string, unknown>}
export interface Reply {status?: number; body: unknown}
export const operator: Access = {principal: 'operator-one', role: 'operator', projectIds: ['project-one']};
export const work = {
  id: 'work-one', projectId: 'project-one', name: 'Local task', goal: 'Complete the task', source: 'manual', sourceRef: 'intake-one',
  sessionIds: ['session-one'], state: 'ready', phase: 'implementation', summary: 'Ready', revision: 8,
  policyVersion: 'policy-one', policy: {commands: [], environment: {API_KEY: 'secret-policy'}},
  publicationGrants: [{approval: 'secret-grant'}], evidence: [], launches: [], actions: ['weiter', 'stopp', 'abbrechen'],
};
export const foreignWork = {...work, id: 'work-other', projectId: 'project-other', name: 'foreign-private-name', goal: 'foreign-private-goal'};
export const registry = [
  {name: 'Weiter', aliases: ['weiter', 'weiter alle'], action: 'weiter', mutates: true, snapshotRequired: true},
  {name: 'Stopp', aliases: ['stopp', 'stop'], action: 'stopp', mutates: true, snapshotRequired: true},
  {name: 'Abbrechen', aliases: ['abbrechen', 'cancel'], action: 'abbrechen', mutates: true, snapshotRequired: true},
  {name: 'Go', aliases: ['go'], action: 'go', mutates: true, snapshotRequired: true},
  {name: 'Neu', aliases: ['neu'], action: 'neu', mutates: true},
];
export const dispatch = {id: 'dispatch-one', workId: 'work-one', projectId: 'project-one', state: 'pending',
  nativeReceipt: {runtimeId: 'runtime-one', acceptedAt: '2026-10-14T07:00:00Z'}, message: 'Perform the original task'};
export const details = {work, events: [{id: 'event-one', workId: 'work-one', kind: 'registered', detail: 'Original task'}], dispatches: [dispatch]};

/** Loopback HTTP fixture, using the actual controller/domain wire shapes. */
export async function fixture(t: TestContext, respond: (call: Call) => Reply | Promise<Reply>, access: Access = operator) {
  const calls: Call[] = [];
  const server = createServer(async (request: IncomingMessage, response: ServerResponse) => {
    try {
      const chunks: Buffer[] = [];
      for await (const chunk of request) chunks.push(Buffer.from(chunk));
      const raw = Buffer.concat(chunks).toString('utf8');
      const call: Call = {method: request.method ?? 'GET', path: request.url ?? '/', ...(raw ? {body: JSON.parse(raw) as Record<string, unknown>} : {})};
      calls.push(call);
      const reply = await respond(call);
      response.writeHead(reply.status ?? 200, {'Content-Type': 'application/json'});
      response.end(JSON.stringify(reply.body));
    } catch (error) {
      response.writeHead(500, {'Content-Type': 'application/json'});
      response.end(JSON.stringify({code: 'FIXTURE_ERROR', message: String(error)}));
    }
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise<void>((resolve, reject) => {server.close((error) => error ? reject(error) : resolve()); server.closeAllConnections();}));
  const url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const tools = operationTools(new DaemonClient({url}), access);
  return {calls, execute: async (name: string, input: unknown) => {
    const tool = tools.find((item) => item.name === name);
    if (!tool) throw new Error(`Unknown fixture tool: ${name}`);
    return await tool.execute(input) as Record<string, unknown>;
  }};
}

export function standard(call: Call): Reply {
  if (call.path === '/api/v1/instructor/commands' && call.method === 'GET') return {body: {commands: registry}};
  if (call.path === '/api/v1/instructor/works/work-one') return {body: details};
  if (call.path === '/api/v1/instructor/commands' && call.method === 'POST') return {body: {batchId: 'batch-one', dispatches: [dispatch], skipped: []}};
  return {status: 404, body: {code: 'ROUTE_NOT_FOUND', message: 'Not found', requestId: 'request-missing'}};
}

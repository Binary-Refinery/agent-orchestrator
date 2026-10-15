import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import type {AddressInfo} from 'node:net';
import test, {type TestContext} from 'node:test';
import {AdapterError, type Access} from '../src/core.js';
import {DaemonClient, DaemonError} from '../src/daemon.js';
import {instructorTools} from '../src/instructor.js';

const access: Access = {principal: 'instructor-client', role: 'operator', projectIds: ['project-one']};
const work = {id: 'work-one', projectId: 'project-one', source: 'issue', sourceRef: 'owner/repo#42', issueId: 'owner/repo#42',
  name: '#42 Actual issue', state: 'ready', revision: 8, phase: 'intake', sessionIds: [], evidence: [], launches: []};
const recovery = {workId: work.id, workRevision: 8, policyVersion: 'policy-one', sessionId: 'orchestrator-one',
  sessionCreatedAt: '2026-10-14T12:34:56.123456789+02:30', providerConversationId: 'provider/thread with spaces', controllerGeneration: 'generation/one',
  ready: false, action: 'restore', reasonCode: 'CONTROLLER_UNAVAILABLE', reason: 'Restore the existing conversation'};
const recoveryInput = {workId: recovery.workId, workRevision: recovery.workRevision, policyVersion: recovery.policyVersion,
  sessionId: recovery.sessionId, sessionCreatedAt: recovery.sessionCreatedAt, providerConversationId: recovery.providerConversationId,
  controllerGeneration: recovery.controllerGeneration};
const intakeInput = {projectId: 'project-one', repository: 'owner/repo', number: 42};
interface Call {method: string; path: string; body?: Record<string, unknown>}
interface Reply {status?: number; body?: unknown; raw?: string; close?: boolean}

async function fixture(t: TestContext, respond: (call: Call) => Reply, policy: Access = access) {
  const calls: Call[] = [];
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const raw = Buffer.concat(chunks).toString('utf8');
    const call: Call = {method: request.method ?? 'GET', path: request.url ?? '/', ...(raw ? {body: JSON.parse(raw) as Record<string, unknown>} : {})};
    calls.push(call);
    const reply = respond(call);
    if (reply.close) {request.socket.destroy(); return;}
    response.writeHead(reply.status ?? 200, {'Content-Type': 'application/json'});
    response.end(reply.raw ?? JSON.stringify(reply.body));
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise<void>((resolve, reject) => {
    server.close((error) => error ? reject(error) : resolve());
    server.closeAllConnections();
  }));
  const tools = instructorTools(new DaemonClient({url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`, timeoutMs: 1000}), policy);
  return {calls, tools, execute: async (name: string, input: unknown) => {
    const tool = tools.find((candidate) => candidate.name === name);
    assert.ok(tool, name);
    return await tool.execute(input) as Record<string, unknown>;
  }};
}

function standard(call: Call): Reply {
  if (call.path === '/api/v1/instructor/works/work-one') return {body: {work, events: [], dispatches: []}};
  if (call.path === '/api/v1/instructor/works/work-one/recovery') return {body: recovery};
  return {status: 404, body: {code: 'ROUTE_NOT_FOUND', message: 'Not found', requestId: 'missing-request'}};
}

test('six Instructor tools retain explicit profiles and conservative mutation hints', async (t) => {
  const f = await fixture(t, standard);
  assert.deepEqual(f.tools.map((tool) => [tool.name, tool.role, tool.readOnly, tool.destructive, tool.openWorld]), [
    ['ao_issue_candidates', 'observer', true, false, false],
    ['ao_issue_intake', 'operator', false, true, true],
    ['ao_project_orchestrators', 'observer', true, false, false],
    ['ao_orchestrator_ensure', 'operator', false, true, true],
    ['ao_work_recovery', 'observer', true, false, false],
    ['ao_recover_controller', 'operator', false, true, true],
  ]);
});

test('issue candidates preserve native issue facts and boundedness while excluding unknown nested fields', async (t) => {
  const f = await fixture(t, () => ({body: {projectId: 'project-one', repository: 'owner/repo', capturedAt: '2026-10-14T12:00:00Z',
    truncated: true, config: {token: 'secret-value'}, issues: [{number: 42, title: 'Actual issue', body: 'Untrusted external issue text',
      url: 'https://github.com/owner/repo/issues/42', state: 'in_progress', linkedWorkId: 'work-one', bodyTruncated: true,
      labels: ['architecture', {secret: 'nested-secret'}], assignees: ['operator', {secret: 'nested-secret'}], token: 'secret-value'}]}}));
  const result = await f.execute('ao_issue_candidates', {projectId: 'project-one', number: 42});
  assert.equal(f.calls[0]?.path, '/api/v1/instructor/projects/project-one/issues?number=42');
  assert.deepEqual(result, {projectId: 'project-one', repository: 'owner/repo', capturedAt: '2026-10-14T12:00:00Z', truncated: true,
    issues: [{number: 42, title: 'Actual issue', body: 'Untrusted external issue text', url: 'https://github.com/owner/repo/issues/42',
      state: 'in_progress', linkedWorkId: 'work-one', bodyTruncated: true, labels: ['architecture'], assignees: ['operator']}]});
  await f.execute('ao_issue_candidates', {projectId: 'project-one'});
  assert.equal(f.calls[1]?.path, '/api/v1/instructor/projects/project-one/issues');
});

test('issue observations fail closed for another project or another exact preview', async (t) => {
  let reply = {projectId: 'project-other', repository: 'foreign-private-repo', issues: [] as unknown[]};
  const f = await fixture(t, () => ({body: reply}));
  await assert.rejects(f.execute('ao_issue_candidates', {projectId: 'project-one'}), (error: unknown) => error instanceof AdapterError && error.code === 'OUT_OF_SCOPE' && !error.message.includes('foreign-private'));
  reply = {projectId: 'project-one', repository: 'owner/repo', issues: [{number: 43, title: 'foreign-private-issue'}]};
  await assert.rejects(f.execute('ao_issue_candidates', {projectId: 'project-one', number: 42}), (error: unknown) => error instanceof AdapterError && error.code === 'DAEMON_CONTRACT' && !error.message.includes('foreign-private'));
});

test('missing or null daemon collections are contract failures rather than apparently empty backlogs or sessions', async (t) => {
  for (const collection of [undefined, null]) {
    const f = await fixture(t, (call) => ({body: call.path.startsWith('/api/v1/sessions') ? {sessions: collection} : {projectId: 'project-one', issues: collection}}));
    for (const name of ['ao_issue_candidates', 'ao_project_orchestrators']) {
      await assert.rejects(f.execute(name, {projectId: 'project-one'}), (error: unknown) => error instanceof AdapterError && error.code === 'DAEMON_CONTRACT');
    }
    assert.ok(f.calls.every((call) => call.method === 'GET'));
  }
});

test('issue intake sends the native repository fence and returns curated actual identity without launching', async (t) => {
  const f = await fixture(t, () => ({status: 201, body: {...work, policy: {token: 'secret-value'}, publicationGrants: [{approval: 'GO secret'}],
    gate: {id: 'gate-one', title: 'Human input', exactApproval: 'GO secret'}, unknown: 'secret-value'}}));
  const result = await f.execute('ao_issue_intake', {...intakeInput, name: ' Distinct issue name '});
  assert.deepEqual(f.calls, [{method: 'POST', path: '/api/v1/instructor/projects/project-one/issues/intake', body: {repository: 'owner/repo', number: 42, name: ' Distinct issue name '}}]);
  assert.equal((result.work as Record<string, unknown>).issueId, 'owner/repo#42');
  assert.equal((result.work as Record<string, unknown>).sourceRef, 'owner/repo#42');
  assert.equal(result.execution, 'unknown');
  assert.equal(JSON.stringify(result).includes('secret'), false);
});

test('native issue intake can reuse an existing work without rewriting its recorded origin', async (t) => {
  const existing = {...work, source: 'manual', sourceRef: 'original/manual identity', state: 'done'};
  const f = await fixture(t, () => ({status: 201, body: existing}));
  const result = await f.execute('ao_issue_intake', intakeInput);
  assert.equal((result.work as Record<string, unknown>).sourceRef, 'original/manual identity');
  assert.equal((result.work as Record<string, unknown>).state, 'done');
  assert.equal(f.calls.length, 1);
});

test('project orchestrator listing excludes foreign projects, workers and private nested configuration', async (t) => {
  const own = {id: 'orchestrator-one', projectId: 'project-one', kind: 'orchestrator', harness: 'codex', mode: 'chat',
    status: 'exited', statusReadiness: 'unavailable', isTerminated: true, activity: {state: 'exited', lastActivityAt: '2026-10-14T12:00:00Z', token: 'secret-value'},
    reviewerConfig: {token: 'secret-value'}, prs: [{body: 'secret-value'}], metadata: {providerConversationId: 'secret-value'}};
  const f = await fixture(t, () => ({body: {sessions: [own, {...own, id: 'foreign-private-session', projectId: 'project-other'},
    {...own, id: 'worker-private-session', kind: 'worker'}], token: 'secret-value'}}));
  const result = await f.execute('ao_project_orchestrators', {projectId: 'project-one'});
  assert.equal(f.calls[0]?.path, '/api/v1/sessions?project=project-one&orchestratorOnly=true');
  assert.equal((result.sessions as unknown[]).length, 1);
  const session = (result.sessions as Record<string, unknown>[])[0];
  assert.equal(session?.statusReadiness, 'unavailable');
  assert.equal(session?.isTerminated, true);
  for (const marker of ['foreign-private', 'worker-private', 'secret-value', 'metadata', 'prs', 'reviewerConfig']) assert.equal(JSON.stringify(result).includes(marker), false, marker);
});

test('orchestrator ensure fixes clean:false and does not infer readiness from minimal or stopped-existing response', async (t) => {
  const f = await fixture(t, () => ({status: 201, body: {orchestrator: {id: 'orchestrator-one', projectId: 'project-one',
    projectName: 'Project', ready: true, status: 'exited', config: {token: 'secret-value'}}, started: true, token: 'secret-value'}}));
  const result = await f.execute('ao_orchestrator_ensure', {projectId: 'project-one'});
  assert.deepEqual(f.calls, [{method: 'POST', path: '/api/v1/orchestrators', body: {projectId: 'project-one', clean: false}}]);
  assert.deepEqual(result, {orchestrator: {id: 'orchestrator-one', projectId: 'project-one', projectName: 'Project'}, readiness: 'unknown', execution: 'unknown'});
});

test('every project input is scope-checked before daemon I/O', async (t) => {
  const f = await fixture(t, standard);
  for (const [name, input] of [
    ['ao_issue_candidates', {projectId: 'project-other'}],
    ['ao_issue_intake', {...intakeInput, projectId: 'project-other'}],
    ['ao_project_orchestrators', {projectId: 'project-other'}],
    ['ao_orchestrator_ensure', {projectId: 'project-other'}],
  ] as const) await assert.rejects(f.execute(name, input), (error: unknown) => error instanceof AdapterError && error.code === 'OUT_OF_SCOPE');
  assert.equal(f.calls.length, 0);
});

test('direct mutation handlers require operator role before any daemon I/O', async (t) => {
  const f = await fixture(t, standard, {...access, role: 'observer'});
  for (const [name, input] of [['ao_issue_intake', intakeInput], ['ao_orchestrator_ensure', {projectId: 'project-one'}], ['ao_recover_controller', recoveryInput]] as const) {
    await assert.rejects(f.execute(name, input), (error: unknown) => error instanceof AdapterError && error.code === 'FORBIDDEN');
  }
  assert.equal(f.calls.length, 0);
});

test('raw observer handlers reject malformed roles and project scope before I/O', async (t) => {
  for (const policy of [{...access, role: 'constructor'}, {...access, projectIds: 'project-one/foreign'}] as unknown as Access[]) {
    const f = await fixture(t, standard, policy);
    for (const [name, input] of [['ao_issue_candidates', {projectId: 'project-one'}], ['ao_project_orchestrators', {projectId: 'project-one'}], ['ao_work_recovery', {workId: 'work-one'}]] as const) {
      await assert.rejects(f.execute(name, input), (error: unknown) => error instanceof AdapterError && ['FORBIDDEN', 'OUT_OF_SCOPE'].includes(error.code));
    }
    assert.equal(f.calls.length, 0);
  }
});

test('strict inputs reject authority, launch and recovery injections without I/O', async (t) => {
  const f = await fixture(t, standard);
  for (const injected of [{clean: true}, {mode: 'tui'}, {approvalMode: 'bypass-permissions'}, {prompt: 'Run this'}, {model: 'unapproved'}, {harness: 'opencode'}]) {
    await assert.rejects(f.execute('ao_orchestrator_ensure', {projectId: 'project-one', ...injected}));
  }
  for (const injected of [{issueId: 'foreign#1'}, {source: 'manual'}, {policy: {}}, {approval: 'GO'}, {workId: 'foreign-work'}]) {
    await assert.rejects(f.execute('ao_issue_intake', {...intakeInput, ...injected}));
  }
  await assert.rejects(f.execute('ao_recover_controller', {...recoveryInput, approval: 'GO'}));
  await assert.rejects(f.execute('ao_work_recovery', {workId: 'work-one', sessionId: 'injected'}));
  for (const number of [0, -1, 1.2, Number.MAX_SAFE_INTEGER + 1, '42']) {
    await assert.rejects(f.execute('ao_issue_candidates', {projectId: 'project-one', number}));
    await assert.rejects(f.execute('ao_issue_intake', {...intakeInput, number}));
  }
  for (const sessionCreatedAt of ['2026-02-30T12:00:00Z', '2026-10-14T12:34Z', '2026-10-14T12:34:56.1234567890Z']) {
    await assert.rejects(f.execute('ao_recover_controller', {...recoveryInput, sessionCreatedAt}));
  }
  assert.equal(f.calls.length, 0);
});

test('work recovery first authorizes exact work then returns only captured controller facts', async (t) => {
  const f = await fixture(t, (call) => call.path.endsWith('/recovery') ? {body: {...recovery, approval: 'GO secret-value', config: {token: 'secret-value'}}} : standard(call));
  assert.deepEqual(await f.execute('ao_work_recovery', {workId: work.id}), recovery);
  assert.deepEqual(f.calls.map((call) => call.path), ['/api/v1/instructor/works/work-one', '/api/v1/instructor/works/work-one/recovery']);
});

test('recovery authorization refuses a foreign work before observation or mutation', async (t) => {
  const f = await fixture(t, () => ({body: {work: {...work, projectId: 'project-other'}}}));
  for (const [name, input] of [['ao_work_recovery', {workId: 'work-one'}], ['ao_recover_controller', recoveryInput]] as const) {
    await assert.rejects(f.execute(name, input), (error: unknown) => error instanceof AdapterError && error.code === 'OUT_OF_SCOPE');
  }
  assert.ok(f.calls.every((call) => call.method === 'GET' && !call.path.endsWith('/recovery')));
});

test('recovery mutation preserves all captured fields including nanoseconds and timezone without refreshing them', async (t) => {
  const f = await fixture(t, (call) => call.method === 'POST' ? {body: {...recovery, ready: true, action: 'none'}} : standard(call));
  const result = await f.execute('ao_recover_controller', recoveryInput);
  assert.deepEqual(f.calls, [{method: 'GET', path: '/api/v1/instructor/works/work-one'},
    {method: 'POST', path: '/api/v1/instructor/works/work-one/recovery', body: recoveryInput}]);
  assert.equal(result.sessionCreatedAt, recoveryInput.sessionCreatedAt);
  assert.equal(result.providerConversationId, recoveryInput.providerConversationId);
  assert.equal(result.ready, true, 'Only the daemon recovery observation establishes this value.');
});

test('legacy empty controller-generation captures remain exact rather than being invented', async (t) => {
  const input = {...recoveryInput, controllerGeneration: ''};
  const f = await fixture(t, (call) => call.method === 'POST' ? {body: {...recovery, controllerGeneration: ''}} : standard(call));
  const result = await f.execute('ao_recover_controller', input);
  assert.equal(f.calls.find((call) => call.method === 'POST')?.body?.controllerGeneration, '');
  assert.equal(result.controllerGeneration, '');
});

test('literal zero RFC3339 time and empty opaque recovery captures are forwarded without normalization', async (t) => {
  const input = {...recoveryInput, sessionCreatedAt: '0001-01-01T00:00:00Z', providerConversationId: '', controllerGeneration: ''};
  const f = await fixture(t, (call) => call.method === 'POST' ? {body: {...recovery, ...input, ready: true, action: 'none'}} : standard(call));
  const result = await f.execute('ao_recover_controller', input);
  assert.deepEqual(f.calls.find((call) => call.method === 'POST')?.body, input);
  assert.equal(result.sessionCreatedAt, input.sessionCreatedAt);
  assert.equal(result.providerConversationId, '');
  assert.equal(result.controllerGeneration, '');
});

test('stale recovery errors preserve envelope metadata, redact foreign choices and do not refresh or retry', async (t) => {
  const f = await fixture(t, (call) => call.method === 'POST' ? {status: 409, body: {code: 'INSTRUCTOR_CONFLICT',
    message: 'foreign-private-name foreign-work-id', requestId: 'stale-recovery'}} : standard(call));
  await assert.rejects(f.execute('ao_recover_controller', {...recoveryInput, workRevision: 7}), (error: unknown) => {
    assert.ok(error instanceof DaemonError);
    assert.equal(error.code, 'INSTRUCTOR_CONFLICT');
    assert.equal(error.status, 409);
    assert.equal(error.requestId, 'stale-recovery');
    assert.equal(error.message.includes('foreign-'), false);
    assert.match(error.message, /inspect.*before retrying/i);
    assert.equal(error.cause, undefined);
    return true;
  });
  assert.equal(f.calls.length, 2);
  assert.equal(f.calls[1]?.body?.workRevision, 7);
});

test('read failures preserve code, status and request ID without raw native portfolio text', async (t) => {
  const f = await fixture(t, () => ({status: 503, body: {code: 'ISSUES_UNAVAILABLE', message: 'foreign-private-configuration', requestId: 'issue-probe'}}));
  await assert.rejects(f.execute('ao_issue_candidates', {projectId: 'project-one'}), (error: unknown) => {
    assert.ok(error instanceof DaemonError);
    assert.equal(error.status, 503);
    assert.equal(error.code, 'ISSUES_UNAVAILABLE');
    assert.equal(error.requestId, 'issue-probe');
    assert.equal(error.message.includes('foreign-private'), false);
    return true;
  });
});

test('admitted mutations with interrupted or malformed responses remain uncertain and are attempted only once', async (t) => {
  for (const reply of [{close: true}, {raw: '{invalid-json'}]) {
    const f = await fixture(t, (call) => call.method === 'POST' ? reply : standard(call));
    for (const [name, input] of [['ao_issue_intake', intakeInput], ['ao_orchestrator_ensure', {projectId: 'project-one'}], ['ao_recover_controller', recoveryInput]] as const) {
      const before = f.calls.filter((call) => call.method === 'POST').length;
      await assert.rejects(f.execute(name, input), (error: unknown) => error instanceof AdapterError
        && ['DAEMON_UNAVAILABLE', 'DAEMON_CONTRACT'].includes(error.code) && /mutation may have been admitted.*inspect.*before retrying/i.test(error.message));
      assert.equal(f.calls.filter((call) => call.method === 'POST').length, before + 1);
    }
  }
});

test('mismatched mutation projections cannot leak returned foreign work or controller data', async (t) => {
  const f = await fixture(t, (call) => {
    if (call.method !== 'POST') return standard(call);
    if (call.path === '/api/v1/orchestrators') return {body: {orchestrator: {id: 'foreign-private-controller', projectId: 'project-other'}}};
    if (call.path.endsWith('/issues/intake')) return {body: {...work, projectId: 'project-other', name: 'foreign-private-work'}};
    return {body: {...recovery, workId: 'foreign-private-work', reason: 'foreign-private-reason'}};
  });
  for (const [name, input] of [['ao_issue_intake', intakeInput], ['ao_orchestrator_ensure', {projectId: 'project-one'}], ['ao_recover_controller', recoveryInput]] as const) {
    await assert.rejects(f.execute(name, input), (error: unknown) => error instanceof AdapterError && error.code === 'OUT_OF_SCOPE'
      && !error.message.includes('foreign-private') && /inspect.*before retrying/i.test(error.message));
  }
});

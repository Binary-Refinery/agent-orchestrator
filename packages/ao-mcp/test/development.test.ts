import assert from 'node:assert/strict';
import {createServer, type IncomingMessage} from 'node:http';
import {setImmediate, setTimeout} from 'node:timers/promises';
import {test, type TestContext} from 'node:test';
import {AdapterError, type Access, type ToolDefinition} from '../src/core.js';
import {DaemonClient, DaemonError} from '../src/daemon.js';
import {CheckJobs, developmentTools, type CheckJob} from '../src/development.js';

const access: Access = {principal: 'developer-a', role: 'developer', projectIds: ['project-a']};
const attached = {workId: 'work-a', sessionId: 'session-a'};
const profile = {command: ['approved-check', 'all'], timeoutSeconds: 5};
const work = () => ({id: 'work-a', projectId: 'project-a', sessionIds: ['session-a'], state: 'running', phase: 'implement', sha: 'a'.repeat(40), policy: {profiles: {push: profile, full: profile}}, evidence: [{id: 'evidence-a', workId: 'work-a', kind: 'check', verified: true}]});

interface Request {method: string; url: URL; body: unknown; headers: IncomingMessage['headers']}
interface Reply {status?: number; body: unknown}

async function fixture(t: TestContext, override: (request: Request) => Reply | undefined | Promise<Reply | undefined> = () => undefined) {
  const requests: Request[] = [];
  let content = 'original\n';
  let fingerprint = 'fingerprint-a';
  const server = createServer(async (incoming, outgoing) => {
    try {
      const chunks: Buffer[] = [];
      for await (const chunk of incoming) chunks.push(Buffer.from(chunk));
      const raw = Buffer.concat(chunks).toString('utf8');
      const request: Request = {method: incoming.method ?? 'GET', url: new URL(incoming.url ?? '/', 'http://127.0.0.1'), body: raw ? JSON.parse(raw) as unknown : undefined, headers: incoming.headers};
      requests.push(request);
      let reply = await override(request);
      if (!reply && request.url.pathname === '/api/v1/instructor/works/work-a') reply = {body: {work: work()}};
      if (!reply && request.url.pathname === '/api/v1/sessions/session-a') reply = {body: {session: {id: 'session-a', projectId: 'project-a'}}};
      if (!reply && request.url.pathname === '/api/v1/instructor/works/work-a/members') reply = {body: {
        workId: 'work-a', projectId: 'project-a', workRevision: 1, atomic: false,
        members: ['a', 'b'].map((suffix) => ({id: `member-${suffix}`, kind: 'session', role: 'worker', association: 'current', status: 'running', sessionId: `session-${suffix}`, instanceId: `instance-${suffix}`, openTarget: {kind: 'session', projectId: 'project-a', sessionId: `session-${suffix}`}})),
      }};
      const base = '/api/v1/instructor/works/work-a/sessions/session-a/workspace/';
      if (request.url.pathname.includes('/workspace/') && (request.url.searchParams.get('projectId') !== 'project-a' || request.url.searchParams.get('instanceId') !== 'instance-a')) {
        reply = {status: 400, body: {code: 'WORKSPACE_SCOPE_REQUIRED', message: 'Fenced workspace scope is required'}};
      }
      if (!reply && request.url.pathname === `${base}file`) {
        const data = request.body as {path: string; content: string; expectedFileFingerprint: string} | undefined;
        const path = request.method === 'PUT' ? data?.path : request.url.searchParams.get('path');
        if (request.method === 'PUT' && data?.expectedFileFingerprint !== fingerprint) {
          reply = {status: 409, body: {error: 'conflict', code: 'WORKSPACE_FILE_STALE', message: 'File changed while it was being edited', requestId: 'request-stale', details: {fileFingerprint: fingerprint}}};
        } else {
          if (data) { content = data.content; fingerprint = 'fingerprint-b'; }
          reply = {body: {sessionId: 'session-a', path, status: 'modified', binary: false, deleted: false, editable: true, content, contentTruncated: false, diff: '+updated', diffTruncated: false, workspaceVersion: 'snapshot-a', fileFingerprint: fingerprint}};
        }
      }
      if (!reply && request.url.pathname === `${base}tree`) reply = {body: {sessionId: 'session-a', path: request.url.searchParams.get('path') ?? '', entries: [{name: 'main.ts', path: 'src/main.ts', type: 'file', status: 'modified'}], truncated: false}};
      if (!reply && request.url.pathname === `${base}search`) reply = {body: {sessionId: 'session-a', query: request.url.searchParams.get('query'), results: [{path: 'src/main.ts', status: 'modified', size: 9, binary: false, fileFingerprint: fingerprint}], truncated: false, nextCursor: 'next-page'}};
      if (!reply && request.url.pathname === `${base}diffs`) reply = {body: {sessionId: 'session-a', workspaceVersion: 'snapshot-a', groups: [{patch: '+updated', includedPaths: ['src/main.ts'], truncated: false, deferred: [], errors: []}]}};
      if (!reply && request.url.pathname === '/api/v1/instructor/works/work-a/checks') reply = {body: work()};
      outgoing.writeHead(reply?.status ?? (reply ? 200 : 404), {'content-type': 'application/json'});
      outgoing.end(JSON.stringify(reply?.body ?? {code: 'ROUTE_NOT_FOUND', message: 'Unknown fixture route'}));
    } catch (error) {
      outgoing.writeHead(500, {'content-type': 'application/json'});
      outgoing.end(JSON.stringify({code: 'FIXTURE_ERROR', message: String(error)}));
    }
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => { server.closeAllConnections(); await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve())); });
  const address = server.address();
  assert(address && typeof address === 'object');
  const client = new DaemonClient({url: `http://127.0.0.1:${address.port}`, sessionId: 'trusted-session'});
  return {client, requests};
}

function tool(tools: ToolDefinition[], name: string) {
  const selected = tools.find((item) => item.name === name);
  assert(selected, `Missing ${name}`);
  return selected;
}

async function finished(tools: ToolDefinition[], jobId: string): Promise<CheckJob> {
  for (let i = 0; i < 50; i++) {
    const job = await tool(tools, 'ao_check_job_status').execute({jobId}) as CheckJob;
    if (job.state !== 'running') return job;
    await setTimeout(5);
  }
  assert.fail('Check job did not finish');
}

test('workspace tools use existing API shapes and preserve pagination/snapshot evidence', async (t) => {
  const {client, requests} = await fixture(t);
  const tools = developmentTools(client, access, new CheckJobs());
  const tree = await tool(tools, 'ao_workspace_tree').execute(attached) as {path: string; entries: unknown[]};
  assert.equal(tree.path, '');
  assert.equal(tree.entries.length, 1);
  const search = await tool(tools, 'ao_workspace_search').execute({...attached, query: 'main', limit: 12, cursor: 'prior-page'}) as {nextCursor: string};
  assert.equal(search.nextCursor, 'next-page');
  const searchRequest = requests.find((request) => request.url.pathname.endsWith('/search'));
  assert.equal(searchRequest?.url.searchParams.get('limit'), '12');
  assert.equal(searchRequest?.url.searchParams.get('cursor'), 'prior-page');
  const read = await tool(tools, 'ao_workspace_read').execute({...attached, path: 'src/main.ts'}) as {content: string; fileFingerprint: string};
  assert.equal(read.content, 'original\n');
  assert.equal(read.fileFingerprint, 'fingerprint-a');
  await tool(tools, 'ao_workspace_diff').execute({...attached, paths: ['src/main.ts'], workspaceVersion: 'snapshot-a'});
  assert.deepEqual(requests.find((request) => request.url.pathname.endsWith('/diffs'))?.body, {paths: ['src/main.ts'], scope: 'combined', contextLines: 3, ignoreWhitespace: false, workspaceVersion: 'snapshot-a'});
  assert(requests.every((request) => request.headers['x-ao-session-id'] === 'trusted-session'));
  const workspaceRequests = requests.filter((request) => request.url.pathname.includes('/workspace/'));
  assert(workspaceRequests.every((request) => request.url.pathname.startsWith('/api/v1/instructor/works/work-a/sessions/session-a/workspace/')));
  assert(workspaceRequests.every((request) => request.url.searchParams.get('projectId') === 'project-a' && request.url.searchParams.get('instanceId') === 'instance-a'));
});

test('workspace edits require the observed fingerprint and surface daemon stale conflicts', async (t) => {
  const {client, requests} = await fixture(t);
  const update = tool(developmentTools(client, access, new CheckJobs()), 'ao_workspace_update');
  await assert.rejects(update.execute({...attached, path: 'src/main.ts', content: 'updated'}), {name: 'ZodError'});
  assert.equal(requests.length, 0);
  const result = await update.execute({...attached, path: 'src/main.ts', content: 'updated', expectedFileFingerprint: 'fingerprint-a'}) as {content: string; fileFingerprint: string};
  assert.deepEqual({content: result.content, fingerprint: result.fileFingerprint}, {content: 'updated', fingerprint: 'fingerprint-b'});
  await assert.rejects(update.execute({...attached, path: 'src/main.ts', content: 'overwritten', expectedFileFingerprint: 'fingerprint-a'}), (error: unknown) => error instanceof DaemonError && error.code === 'WORKSPACE_FILE_STALE' && error.status === 409 && error.requestId === 'request-stale');
  await update.execute({...attached, path: 'src/main.ts', content: 'resolved', expectedFileFingerprint: 'fingerprint-b'});
});

test('source access rejects roles, detached sessions and cross-project sessions before workspace I/O', async (t) => {
  const {client, requests} = await fixture(t, (request) => request.url.pathname === '/api/v1/sessions/session-a' ? {body: {session: {id: 'session-a', projectId: 'project-b'}}} : undefined);
  for (const role of ['observer', 'operator'] as const) {
    await assert.rejects(tool(developmentTools(client, {...access, role}), 'ao_workspace_read').execute({...attached, path: 'main.ts'}), {code: 'FORBIDDEN'});
  }
  assert.equal(requests.length, 0);
  await assert.rejects(tool(developmentTools(client, access), 'ao_workspace_read').execute({...attached, sessionId: 'detached', path: 'main.ts'}), {code: 'OUT_OF_SCOPE'});
  await assert.rejects(tool(developmentTools(client, {...access, projectIds: ['project-a', 'project-b']}), 'ao_workspace_tree').execute(attached), {code: 'OUT_OF_SCOPE'});
  assert(!requests.some((request) => request.url.pathname.includes('/workspace/')));
});

test('every workspace/check action revalidates work project membership', async (t) => {
  const {client, requests} = await fixture(t, (request) => request.url.pathname === '/api/v1/instructor/works/work-a' ? {body: {work: {...work(), projectId: 'outside'}}} : undefined);
  const tools = developmentTools(client, access, new CheckJobs());
  const calls: [string, unknown][] = [
    ['ao_workspace_tree', attached], ['ao_workspace_search', {...attached, query: 'main'}],
    ['ao_workspace_read', {...attached, path: 'main.ts'}], ['ao_workspace_diff', {...attached, paths: ['main.ts']}],
    ['ao_workspace_update', {...attached, path: 'main.ts', content: 'new', expectedFileFingerprint: 'fingerprint-a'}],
    ['ao_run_checks', {...attached, operationId: 'attempt-a', profiles: ['push']}],
  ];
  for (const [name, input] of calls) await assert.rejects(tool(tools, name).execute(input), {code: 'OUT_OF_SCOPE'});
  assert(requests.every((request) => request.url.pathname === '/api/v1/instructor/works/work-a'));
});

test('workspace paths reject traversal and platform-specific escape forms before I/O', async (t) => {
  const {client, requests} = await fixture(t);
  const tools = developmentTools(client, access);
  for (const path of ['../secrets', 'a/../b', '/etc/passwd', 'C:/Windows/file', '\\server\\file', 'a\\b', 'a\0b', 'a//b', './file', 'file\n']) {
    await assert.rejects(tool(tools, 'ao_workspace_read').execute({...attached, path}), {name: 'ZodError'});
    await assert.rejects(tool(tools, 'ao_workspace_diff').execute({...attached, paths: [path]}), {name: 'ZodError'});
  }
  await assert.rejects(tool(tools, 'ao_workspace_update').execute({...attached, path: 'main.ts', content: 'ü'.repeat(140_000), expectedFileFingerprint: 'fingerprint-a'}), {name: 'ZodError'});
  assert.equal(requests.length, 0);
});

test('workspace responses for a different session are rejected', async (t) => {
  const {client} = await fixture(t, (request) => request.url.pathname.endsWith('/workspace/file') ? {body: {sessionId: 'other-session', path: 'main.ts', content: 'outside'}} : undefined);
  await assert.rejects(tool(developmentTools(client, access), 'ao_workspace_read').execute({...attached, path: 'main.ts'}), {code: 'DAEMON_CONTRACT'});
});

test('nested workspace records are projected without unknown configuration fields', async (t) => {
  const {client} = await fixture(t, (request) => request.url.pathname.endsWith('/workspace/tree') ? {body: {sessionId: 'session-a', path: '', entries: [{name: 'main.ts', path: 'main.ts', type: 'file', credentials: 'must-not-cross'}], truncated: false, configuration: {token: 'must-not-cross'}}} : undefined);
  const result = await tool(developmentTools(client, access), 'ao_workspace_tree').execute(attached);
  assert.deepEqual(result, {sessionId: 'session-a', path: '', entries: [{name: 'main.ts', path: 'main.ts', type: 'file'}], truncated: false});
});

test('approved checks return an async handle, retain verified evidence and reuse operation IDs', async (t) => {
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  const {client, requests} = await fixture(t, async (request) => {
    if (!request.url.pathname.endsWith('/checks')) return undefined;
    await held;
    return {body: work()};
  });
  const tools = developmentTools(client, access, new CheckJobs());
  const input = {...attached, operationId: 'attempt-a', profiles: ['push', 'full']};
  const start = tool(tools, 'ao_run_checks');
  const job = await start.execute(input) as CheckJob;
  assert.equal(job.state, 'running');
  const duplicate = await start.execute(input) as CheckJob;
  assert.equal(duplicate.jobId, job.jobId);
  await assert.rejects(start.execute({...input, profiles: ['push']}), {code: 'IDEMPOTENCY_CONFLICT'});
  release();
  const done = await finished(tools, job.jobId);
  assert.equal(done.state, 'completed');
  assert.deepEqual((done.result as {evidence: unknown[]}).evidence, work().evidence);
  assert(!Object.hasOwn(done.result as object, 'policy'));
  assert.equal(requests.filter((request) => request.url.pathname.endsWith('/checks')).length, 1);
  assert.deepEqual(requests.find((request) => request.url.pathname.endsWith('/checks'))?.body, {sessionId: 'session-a', profiles: ['push', 'full']});
});

test('unapproved profiles and arbitrary command overrides cannot start a check', async (t) => {
  const {client, requests} = await fixture(t);
  const start = tool(developmentTools(client, access, new CheckJobs()), 'ao_run_checks');
  await assert.rejects(start.execute({...attached, operationId: 'attempt-a', profiles: ['shell']}), {code: 'PROFILE_NOT_APPROVED'});
  await assert.rejects(start.execute({...attached, operationId: 'attempt-a', profiles: ['push'], command: ['evil']}), {name: 'ZodError'});
  await assert.rejects(start.execute({...attached, operationId: 'attempt-a', profiles: ['push', 'push']}), {name: 'ZodError'});
  assert(!requests.some((request) => request.url.pathname.endsWith('/checks')));
});

test('check handles are principal-owned and status revalidates current project scope', async (t) => {
  const {client} = await fixture(t);
  const jobs = new CheckJobs();
  const tools = developmentTools(client, access, jobs);
  const job = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'attempt-a', profiles: ['push']}) as CheckJob;
  await finished(tools, job.jobId);
  await assert.rejects(tool(developmentTools(client, {...access, principal: 'developer-b'}, jobs), 'ao_check_job_status').execute({jobId: job.jobId}), {code: 'JOB_NOT_FOUND'});
  await assert.rejects(tool(developmentTools(client, {...access, projectIds: []}, jobs), 'ao_check_job_status').execute({jobId: job.jobId}), {code: 'OUT_OF_SCOPE'});
});

test('daemon check errors preserve error envelope and retain uncertain request locks', async (t) => {
  const {client} = await fixture(t, (request) => request.url.pathname.endsWith('/checks') ? {status: 409, body: {code: 'INSTRUCTOR_CONFLICT', message: 'Work changed', requestId: 'request-check'}} : undefined);
  const tools = developmentTools(client, access, new CheckJobs());
  const job = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'attempt-a', profiles: ['push']}) as CheckJob;
  const result = await finished(tools, job.jobId);
  assert.equal(result.state, 'uncertain');
  assert.deepEqual(result.error, {code: 'INSTRUCTOR_CONFLICT', message: 'Work changed', status: 409, requestId: 'request-check'});
  await assert.rejects(tool(tools, 'ao_run_checks').execute({...attached, operationId: 'attempt-b', profiles: ['push']}), {code: 'CHECKS_BUSY'});
});

test('checks coordinate across principals and block edits until completion', async (t) => {
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  const {client, requests} = await fixture(t, async (request) => {
    if (!request.url.pathname.endsWith('/checks')) return undefined;
    await held;
    return {body: work()};
  });
  const jobs = new CheckJobs();
  const tools = developmentTools(client, access, jobs);
  const job = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'attempt-a', profiles: ['push']}) as CheckJob;
  await assert.rejects(tool(developmentTools(client, {...access, principal: 'developer-b'}, jobs), 'ao_run_checks').execute({...attached, operationId: 'attempt-b', profiles: ['push']}), {code: 'CHECKS_BUSY'});
  await assert.rejects(tool(tools, 'ao_workspace_update').execute({...attached, path: 'main.ts', content: 'new', expectedFileFingerprint: 'fingerprint-a'}), {code: 'CHECKS_BUSY'});
  assert(!requests.some((request) => request.method === 'PUT'));
  release();
  await finished(tools, job.jobId);
  await tool(tools, 'ao_workspace_update').execute({...attached, path: 'main.ts', content: 'new', expectedFileFingerprint: 'fingerprint-a'});
});

test('uncertain check admission is never retried and keeps the session locked', async () => {
  const jobs = new CheckJobs();
  let attempts = 0;
  const input = {...attached, operationId: 'attempt-a', profiles: ['push']};
  const job = jobs.start(access, input, async () => { attempts++; throw new AdapterError('DAEMON_UNAVAILABLE', 'Timed out; admission uncertain'); });
  await setImmediate();
  assert.equal(jobs.get(access, job.jobId).state, 'uncertain');
  assert.equal(jobs.start(access, input, async () => undefined).jobId, job.jobId);
  assert.equal(attempts, 1);
  assert.throws(() => jobs.start(access, {...input, operationId: 'attempt-b'}, async () => undefined), {code: 'CHECKS_BUSY'});
  assert.throws(() => jobs.assertIdle(attached.sessionId), {code: 'CHECKS_BUSY'});
});

test('check handles are bounded and malformed successful responses remain uncertain', async (t) => {
  const {client} = await fixture(t, (request) => request.url.pathname.endsWith('/checks') ? {body: {id: 'wrong-work', projectId: 'project-a'}} : undefined);
  const jobs = new CheckJobs(1, 1);
  const tools = developmentTools(client, access, jobs);
  const job = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'attempt-a', profiles: ['push']}) as CheckJob;
  const result = await finished(tools, job.jobId);
  assert.equal(result.state, 'uncertain');
  assert.equal(result.error?.code, 'DAEMON_CONTRACT');
  assert.throws(() => jobs.start(access, {workId: 'work-b', sessionId: 'session-b', operationId: 'attempt-b', profiles: ['push']}, async () => undefined), {code: 'JOB_LIMIT'});
});

test('check profiles above the bounded total timeout cannot be submitted', async (t) => {
  const {client, requests} = await fixture(t, (request) => request.url.pathname === '/api/v1/instructor/works/work-a' ? {body: {work: {...work(), policy: {profiles: {push: {...profile, timeoutSeconds: 46}}}}}} : undefined);
  await assert.rejects(tool(developmentTools(client, access, new CheckJobs()), 'ao_run_checks').execute({...attached, operationId: 'attempt-a', profiles: ['push']}), {code: 'CHECK_DEADLINE_LIMIT'});
  assert(!requests.some((request) => request.url.pathname.endsWith('/checks')));
});

test('an admitted workspace PUT excludes checks and concurrent edits for its session and work', async (t) => {
  let release!: () => void;
  let entered!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  const admitted = new Promise<void>((resolve) => { entered = resolve; });
  const {client, requests} = await fixture(t, async (request) => {
    if (request.url.pathname === '/api/v1/instructor/works/work-a') return {body: {work: {...work(), sessionIds: ['session-a', 'session-b']}}};
    if (request.url.pathname === '/api/v1/sessions/session-b') return {body: {session: {id: 'session-b', projectId: 'project-a'}}};
    if (request.method !== 'PUT') return undefined;
    entered();
    await held;
    return undefined;
  });
  const tools = developmentTools(client, access, new CheckJobs());
  const update = tool(tools, 'ao_workspace_update');
  const input = {...attached, path: 'main.ts', content: 'updated', expectedFileFingerprint: 'fingerprint-a'};
  const updating = update.execute(input);
  await admitted;
  try {
    await assert.rejects(tool(tools, 'ao_run_checks').execute({...attached, operationId: 'attempt-a', profiles: ['push']}), {code: 'CHECKS_BUSY'});
    await assert.rejects(tool(tools, 'ao_run_checks').execute({...attached, sessionId: 'session-b', operationId: 'attempt-b', profiles: ['push']}), {code: 'CHECKS_BUSY'});
    await assert.rejects(update.execute(input), {code: 'CHECKS_BUSY'});
    assert.equal(requests.filter((request) => request.method === 'PUT').length, 1);
    assert(!requests.some((request) => request.url.pathname.endsWith('/checks')));
  } finally { release(); }
  await updating;
  const job = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'attempt-c', profiles: ['push']}) as CheckJob;
  await finished(tools, job.jobId);
});

test('uncertain workspace writes retain admission locks and never replay edits', async (t) => {
  const {client, requests} = await fixture(t, (request) => request.method === 'PUT' ? {status: 500, body: {code: 'AFTER_WRITE_FAILURE', message: 'Refresh failed after replacement'}} : undefined);
  const tools = developmentTools(client, access, new CheckJobs());
  const update = tool(tools, 'ao_workspace_update');
  const input = {...attached, path: 'main.ts', content: 'updated', expectedFileFingerprint: 'fingerprint-a'};
  await assert.rejects(update.execute(input), {code: 'AFTER_WRITE_FAILURE'});
  await assert.rejects(update.execute(input), {code: 'CHECKS_BUSY'});
  await assert.rejects(tool(tools, 'ao_run_checks').execute({...attached, operationId: 'attempt-a', profiles: ['push']}), {code: 'CHECKS_BUSY'});
  assert.equal(requests.filter((request) => request.method === 'PUT').length, 1);
  assert(!requests.some((request) => request.url.pathname.endsWith('/checks')));
});

test('default check jobs isolate daemon origins and survive client recreation for the same origin', async (t) => {
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  const daemonA = await fixture(t, async (request) => {
    if (!request.url.pathname.endsWith('/checks')) return undefined;
    await held;
    return {body: {...work(), summary: 'daemon-a'}};
  });
  const daemonB = await fixture(t, (request) => request.url.pathname.endsWith('/checks') ? {body: {...work(), summary: 'daemon-b'}} : undefined);
  const input = {...attached, operationId: 'same-attempt', profiles: ['push']};
  const toolsA = developmentTools(daemonA.client, access);
  const toolsB = developmentTools(daemonB.client, access);
  const jobA = await tool(toolsA, 'ao_run_checks').execute(input) as CheckJob;
  try {
    const jobB = await tool(toolsB, 'ao_run_checks').execute(input) as CheckJob;
    assert.notEqual(jobA.jobId, jobB.jobId);
    const resultB = await finished(toolsB, jobB.jobId);
    assert.equal((resultB.result as {summary: string}).summary, 'daemon-b');
    await assert.rejects(tool(toolsA, 'ao_check_job_status').execute({jobId: jobB.jobId}), {code: 'JOB_NOT_FOUND'});
    const recreated = developmentTools(new DaemonClient({url: daemonA.client.url.origin}), access);
    const same = await tool(recreated, 'ao_run_checks').execute(input) as CheckJob;
    assert.equal(same.jobId, jobA.jobId);
    assert.equal(daemonB.requests.filter((request) => request.url.pathname.endsWith('/checks')).length, 1);
  } finally { release(); }
  const resultA = await finished(toolsA, jobA.jobId);
  assert.equal((resultA.result as {summary: string}).summary, 'daemon-a');
  assert.equal(daemonA.requests.filter((request) => request.url.pathname.endsWith('/checks')).length, 1);
});

test('check profile discovery returns only runnable names/timeouts from the scoped pinned policy', async (t) => {
  const {client} = await fixture(t, (request) => request.url.pathname === '/api/v1/instructor/works/work-a' ? {body: {work: {...work(), policyVersion: 'pinned-policy', policy: {profiles: {push: {...profile, command: ['secret-command'], environment: {TOKEN: 'secret-token'}}, full: profile, unconfigured: {command: [], timeoutSeconds: 5}}, credentials: 'secret-policy'}}}} : undefined);
  const result = await tool(developmentTools(client, access), 'ao_check_profiles').execute({workId: 'work-a'});
  assert.deepEqual(result, {workId: 'work-a', policyVersion: 'pinned-policy', maxTotalTimeoutSeconds: 45, profiles: [{name: 'full', timeoutSeconds: 5, available: true}, {name: 'push', timeoutSeconds: 5, available: true}]});
});

test('check profile discovery enforces role and current project scope', async (t) => {
  const {client, requests} = await fixture(t);
  await assert.rejects(tool(developmentTools(client, {...access, role: 'observer'}), 'ao_check_profiles').execute({workId: 'work-a'}), {code: 'FORBIDDEN'});
  assert.equal(requests.length, 0);
  await assert.rejects(tool(developmentTools(client, {...access, projectIds: []}), 'ao_check_profiles').execute({workId: 'work-a'}), {code: 'OUT_OF_SCOPE'});
});

for (const status of [400, 409]) {
  test(`HTTP ${status} after profile side effects cannot admit another check or edit`, async (t) => {
    let executions = 0;
    const {client, requests} = await fixture(t, (request) => {
      if (!request.url.pathname.endsWith('/checks')) return undefined;
      // A profile executed before AO detected changed workspace/commit (400)
      // or a final work revision conflict (409). These are not preflight rejects.
      executions++;
      return {status, body: {code: status === 400 ? 'INSTRUCTOR_INVALID' : 'INSTRUCTOR_CONFLICT', message: status === 400 ? 'commit changed during check execution' : 'revision changed after checks', requestId: `after-execution-${status}`}};
    });
    const tools = developmentTools(client, access, new CheckJobs());
    const input = {...attached, operationId: 'first-execution', profiles: ['push']};
    const first = await tool(tools, 'ao_run_checks').execute(input) as CheckJob;
    const result = await finished(tools, first.jobId);
    assert.equal(result.state, 'uncertain');
    assert.equal(result.error?.status, status);
    assert.equal(executions, 1);
    const repeated = await tool(tools, 'ao_run_checks').execute(input) as CheckJob;
    assert.equal(repeated.jobId, first.jobId);
    await assert.rejects(tool(tools, 'ao_run_checks').execute({...input, operationId: 'second-execution'}), {code: 'CHECKS_BUSY'});
    await assert.rejects(tool(tools, 'ao_workspace_update').execute({...attached, path: 'main.ts', content: 'overwrite', expectedFileFingerprint: 'fingerprint-a'}), {code: 'CHECKS_BUSY'});
    assert.equal(executions, 1);
    assert(!requests.some((request) => request.method === 'PUT'));
  });
}

test('check admission budgets the complete batch and caps the client deadline below daemon REST timeout', async (t) => {
  const {client, requests} = await fixture(t, (request) => request.url.pathname === '/api/v1/instructor/works/work-a' ? {body: {work: {...work(), policy: {profiles: {push: {...profile, timeoutSeconds: 20}, full: {...profile, timeoutSeconds: 25}, extra: {...profile, timeoutSeconds: 1}}}}}} : undefined);
  const deadlines: (number | undefined)[] = [];
  class InspectingClient extends DaemonClient {
    override post<T = unknown>(path: string, body: unknown, timeoutMs?: number): Promise<T> {
      deadlines.push(timeoutMs);
      return super.post<T>(path, body, timeoutMs);
    }
  }
  const tools = developmentTools(new InspectingClient({url: client.url.origin}), access, new CheckJobs());
  await assert.rejects(tool(tools, 'ao_run_checks').execute({...attached, operationId: 'too-long', profiles: ['push', 'full', 'extra']}), {code: 'CHECK_DEADLINE_LIMIT'});
  assert(!requests.some((request) => request.url.pathname.endsWith('/checks')));
  const boundary = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'within-deadline', profiles: ['push', 'full']}) as CheckJob;
  assert.equal((await finished(tools, boundary.jobId)).state, 'completed');
  const short = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'short-profile', profiles: ['extra']}) as CheckJob;
  assert.equal((await finished(tools, short.jobId)).state, 'completed');
  assert.deepEqual(deadlines, [55_000, 55_000]);
});

test('short approved profiles survive legitimate daemon preflight beyond their native timeout plus ten seconds', {timeout: 25_000}, async (t) => {
  const delay = new AbortController();
  t.after(() => delay.abort());
  let delivered!: () => void;
  const responseReady = new Promise<void>((resolve) => { delivered = resolve; });
  const {client, requests} = await fixture(t, async (request) => {
    if (request.url.pathname === '/api/v1/instructor/works/work-a') return {body: {work: {...work(), policy: {profiles: {short: {...profile, timeoutSeconds: 1}}}}}};
    if (!request.url.pathname.endsWith('/checks')) return undefined;
    // Preflight is outside the native profile timeout. The previous 1+10s
    // transport budget would abort this real HTTP request before its response.
    await setTimeout(12_000, undefined, {signal: delay.signal});
    delivered();
    return {body: work()};
  });
  const tools = developmentTools(client, access, new CheckJobs());
  const job = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'slow-preflight', profiles: ['short']}) as CheckJob;
  assert.equal(job.state, 'running');
  await responseReady;
  const result = await finished(tools, job.jobId);
  assert.equal(result.state, 'completed');
  assert.equal(result.error, undefined);
  assert.equal(requests.filter((request) => request.url.pathname.endsWith('/checks')).length, 1);
});

test('profile discovery marks long approved profiles unavailable under the current REST deadline', async (t) => {
  const {client, requests} = await fixture(t, (request) => request.url.pathname === '/api/v1/instructor/works/work-a' ? {body: {work: {...work(), policy: {profiles: {short: profile, full: {...profile, timeoutSeconds: 900}}}}}} : undefined);
  const tools = developmentTools(client, access, new CheckJobs());
  assert.deepEqual(await tool(tools, 'ao_check_profiles').execute({workId: 'work-a'}), {workId: 'work-a', maxTotalTimeoutSeconds: 45, profiles: [
    {name: 'full', timeoutSeconds: 900, available: false, unavailableReason: 'CHECK_DEADLINE_LIMIT'},
    {name: 'short', timeoutSeconds: 5, available: true},
  ]});
  await assert.rejects(tool(tools, 'ao_run_checks').execute({...attached, operationId: 'full-profile', profiles: ['full']}), {code: 'CHECK_DEADLINE_LIMIT'});
  assert(!requests.some((request) => request.url.pathname.endsWith('/checks')));
});

test('missing fenced workspace routes fail closed without falling back to legacy session routes', async (t) => {
  const {client, requests} = await fixture(t, (request) => {
    if (request.url.pathname.startsWith('/api/v1/instructor/works/work-a/sessions/session-a/workspace/')) return {status: 404, body: {code: 'ROUTE_NOT_FOUND', message: 'Scoped workspace API is unavailable'}};
    if (request.url.pathname.startsWith('/api/v1/sessions/session-a/workspace/')) return {body: {sessionId: 'session-a', path: 'main.ts', content: 'legacy unsafe result'}};
    return undefined;
  });
  const tools = developmentTools(client, access, new CheckJobs());
  const calls: [string, unknown][] = [
    ['ao_workspace_tree', attached], ['ao_workspace_search', {...attached, query: 'main'}],
    ['ao_workspace_read', {...attached, path: 'main.ts'}], ['ao_workspace_diff', {...attached, paths: ['main.ts']}],
    ['ao_workspace_update', {...attached, path: 'main.ts', content: 'new', expectedFileFingerprint: 'fingerprint-a'}],
  ];
  for (const [name, input] of calls) await assert.rejects(tool(tools, name).execute(input), {code: 'ROUTE_NOT_FOUND'});
  const workspaceRequests = requests.filter((request) => request.url.pathname.includes('/workspace/'));
  assert.equal(workspaceRequests.length, calls.length);
  assert(workspaceRequests.every((request) => request.url.pathname.startsWith('/api/v1/instructor/works/work-a/sessions/session-a/workspace/')));
  assert(workspaceRequests.every((request) => request.url.searchParams.get('instanceId') === 'instance-a' && request.url.searchParams.get('projectId') === 'project-a'));
});

test('historical session membership cannot authorize workspace access', async (t) => {
  const {client, requests} = await fixture(t, (request) => request.url.pathname.endsWith('/members') ? {body: {workId: 'work-a', projectId: 'project-a', atomic: false, members: [{kind: 'session', sessionId: 'session-a', instanceId: 'old-instance', association: 'historical', openTarget: {kind: 'session', projectId: 'project-a', sessionId: 'session-a'}}]}} : undefined);
  await assert.rejects(tool(developmentTools(client, access, new CheckJobs()), 'ao_workspace_read').execute({...attached, path: 'main.ts'}), {code: 'OUT_OF_SCOPE'});
  assert(!requests.some((request) => request.url.pathname.includes('/workspace/')));
});

test('workspace requests preserve the captured immutable instance and surface fenced rejection', async (t) => {
  const {client, requests} = await fixture(t, (request) => {
    if (!request.url.pathname.endsWith('/workspace/file')) return undefined;
    assert.equal(request.url.searchParams.get('instanceId'), 'instance-a');
    assert.equal(request.url.searchParams.get('projectId'), 'project-a');
    return {status: 409, body: {code: 'INSTRUCTOR_SESSION_INSTANCE_CHANGED', message: 'Session was replaced after scope capture', requestId: 'instance-fence'}};
  });
  await assert.rejects(tool(developmentTools(client, access, new CheckJobs()), 'ao_workspace_read').execute({...attached, path: 'main.ts'}), (error: unknown) => error instanceof DaemonError && error.code === 'INSTRUCTOR_SESSION_INSTANCE_CHANGED' && error.requestId === 'instance-fence');
  assert.equal(requests.filter((request) => request.url.pathname.includes('/workspace/')).length, 1);
});

for (const status of [400, 404]) {
  test(`HTTP ${status} after workspace replacement keeps edit and check admission locked`, async (t) => {
    let replacements = 0;
    const {client, requests} = await fixture(t, (request) => {
      if (request.method !== 'PUT') return undefined;
      // Replacement succeeded, then its refreshed read failed after an external
      // change. A generic 4xx cannot establish that the write did not happen.
      replacements++;
      return {status, body: {code: status === 404 ? 'WORKSPACE_FILE_NOT_FOUND' : 'WORKSPACE_GIT_STATE_UNAVAILABLE', message: 'Refreshed workspace read failed after replacement'}};
    });
    const tools = developmentTools(client, access, new CheckJobs());
    const input = {...attached, path: 'main.ts', content: 'written', expectedFileFingerprint: 'fingerprint-a'};
    await assert.rejects(tool(tools, 'ao_workspace_update').execute(input), (error: unknown) => error instanceof DaemonError && error.status === status);
    await assert.rejects(tool(tools, 'ao_workspace_update').execute(input), {code: 'CHECKS_BUSY'});
    await assert.rejects(tool(tools, 'ao_run_checks').execute({...attached, operationId: 'after-uncertain-edit', profiles: ['push']}), {code: 'CHECKS_BUSY'});
    assert.equal(replacements, 1);
    assert.equal(requests.filter((request) => request.method === 'PUT').length, 1);
    assert(!requests.some((request) => request.url.pathname.endsWith('/checks')));
  });
}

test('exact scoped workspace preflight errors permit a fresh attempt after scope is resolved', async (t) => {
  for (const [status, code] of [[409, 'INSTRUCTOR_WORKSPACE_SCOPE_CHANGED'], [409, 'INSTRUCTOR_WORKSPACE_BUSY'], [400, 'INSTRUCTOR_WORKSPACE_SCOPE_REQUIRED']] as const) {
    let attempts = 0;
    const {client} = await fixture(t, (request) => {
      if (request.method !== 'PUT') return undefined;
      attempts++;
      return attempts === 1 ? {status, body: {code, message: 'Preflight rejected before replacement'}} : undefined;
    });
    const tools = developmentTools(client, access, new CheckJobs());
    const input = {...attached, path: 'main.ts', content: 'written', expectedFileFingerprint: 'fingerprint-a'};
    await assert.rejects(tool(tools, 'ao_workspace_update').execute(input), {code});
    const result = await tool(tools, 'ao_workspace_update').execute(input) as {content: string};
    assert.equal(result.content, 'written');
    assert.equal(attempts, 2);
  }
});

test('exact daemon checks deadline preflight permits a smaller batch without replaying rejected admission', async (t) => {
  let attempts = 0;
  let executions = 0;
  const {client} = await fixture(t, (request) => {
    if (!request.url.pathname.endsWith('/checks')) return undefined;
    attempts++;
    if (attempts === 1) return {status: 400, body: {code: 'INSTRUCTOR_CHECKS_DEADLINE_PREEXECUTION', message: 'Configured runtime request deadline is too short; no checks admitted'}};
    executions++;
    return {body: work()};
  });
  const tools = developmentTools(client, access, new CheckJobs());
  const input = {...attached, operationId: 'rejected-batch', profiles: ['push', 'full']};
  const first = await tool(tools, 'ao_run_checks').execute(input) as CheckJob;
  assert.equal((await finished(tools, first.jobId)).state, 'failed');
  assert.equal(executions, 0);
  assert.equal((await tool(tools, 'ao_run_checks').execute(input) as CheckJob).jobId, first.jobId);
  assert.equal(attempts, 1);
  const smaller = await tool(tools, 'ao_run_checks').execute({...input, operationId: 'smaller-batch', profiles: ['push']}) as CheckJob;
  assert.equal((await finished(tools, smaller.jobId)).state, 'completed');
  assert.equal(executions, 1);
  assert.equal(attempts, 2);
});

test('own completed check results remain pollable after the worker is historical, replaced or missing', async (t) => {
  let membership: 'current' | 'historical' | 'replaced' | 'missing' = 'current';
  let foreignProject = false;
  const {client, requests} = await fixture(t, (request) => {
    if (foreignProject && request.url.pathname === '/api/v1/instructor/works/work-a') return {body: {work: {...work(), projectId: 'outside-project'}}};
    if (membership !== 'current' && request.url.pathname.endsWith('/members')) return {body: {
      workId: 'work-a', projectId: 'project-a', atomic: false,
      members: membership === 'missing' ? [] : [{kind: 'session', sessionId: 'session-a', instanceId: membership === 'replaced' ? 'replacement-instance' : 'instance-a', association: membership === 'historical' ? 'historical' : 'current', openTarget: {kind: 'session', projectId: 'project-a', sessionId: 'session-a'}}],
    }};
    if (membership === 'missing' && request.url.pathname === '/api/v1/sessions/session-a') return {status: 404, body: {code: 'SESSION_NOT_FOUND', message: 'Worker has exited'}};
    return undefined;
  });
  const jobs = new CheckJobs();
  const tools = developmentTools(client, access, jobs);
  const job = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'finished-worker', profiles: ['push']}) as CheckJob;
  // Let the background request complete without polling the MCP status tool;
  // the first user-visible poll occurs only after membership becomes historical.
  for (let i = 0; i < 50 && jobs.get(access, job.jobId).state === 'running'; i++) await setTimeout(5);
  assert.equal(jobs.get(access, job.jobId).state, 'completed');
  const captured = requests.length;
  for (const state of ['historical', 'replaced', 'missing'] as const) {
    membership = state;
    const result = await tool(tools, 'ao_check_job_status').execute({jobId: job.jobId}) as CheckJob;
    assert.equal(result.state, 'completed');
    assert.deepEqual((result.result as {evidence: unknown[]}).evidence, work().evidence);
  }
  assert(requests.slice(captured).every((request) => request.url.pathname === '/api/v1/instructor/works/work-a'));
  foreignProject = true;
  await assert.rejects(tool(tools, 'ao_check_job_status').execute({jobId: job.jobId}), {code: 'OUT_OF_SCOPE'});
});

test('opaque approved profile names preserve spaces, slashes, Unicode and exact whitespace in discovery and execution', async (t) => {
  const names = ['unit tests', 'lint/fast', '測試集', ' exact profile ', '__proto__'];
  const {client, requests} = await fixture(t, (request) => request.url.pathname === '/api/v1/instructor/works/work-a' ? {body: {work: {...work(), policy: {profiles: Object.fromEntries(names.map((name) => [name, profile]))}}}} : undefined);
  const tools = developmentTools(client, access, new CheckJobs());
  const discovered = await tool(tools, 'ao_check_profiles').execute({workId: 'work-a'}) as {profiles: {name: string; timeoutSeconds: number; available: boolean}[]};
  assert.deepEqual(new Set(discovered.profiles.map((entry) => entry.name)), new Set(names));
  assert(discovered.profiles.every((entry) => entry.timeoutSeconds === 5 && entry.available));
  await assert.rejects(tool(tools, 'ao_run_checks').execute({...attached, operationId: 'unknown-opaque', profiles: ['unit tests ']}), {code: 'PROFILE_NOT_APPROVED'});
  await assert.rejects(tool(tools, 'ao_run_checks').execute({...attached, operationId: 'unknown-inherited', profiles: ['constructor']}), {code: 'PROFILE_NOT_APPROVED'});
  assert(!requests.some((request) => request.url.pathname.endsWith('/checks')));
  const job = await tool(tools, 'ao_run_checks').execute({...attached, operationId: 'opaque-profiles', profiles: names}) as CheckJob;
  assert.equal((await finished(tools, job.jobId)).state, 'completed');
  assert.deepEqual(requests.find((request) => request.url.pathname.endsWith('/checks'))?.body, {sessionId: 'session-a', profiles: names});
});

test('check profile names reject reserved business evidence, empty names and oversized names before I/O', async (t) => {
  const {client, requests} = await fixture(t);
  const run = tool(developmentTools(client, access, new CheckJobs()), 'ao_run_checks');
  for (const name of ['', 'p'.repeat(513), 'task-packet', 'ci', 'ci-failed', 'review-report', 'triage', 'delta', 'delta-changes', 'attribution', 'traceability']) {
    await assert.rejects(run.execute({...attached, operationId: 'invalid-profile', profiles: [name]}), {name: 'ZodError'});
  }
  assert.equal(requests.length, 0);
});

import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import type {AddressInfo} from 'node:net';
import test, {type TestContext} from 'node:test';
import type {Access} from '../src/core.js';
import {DaemonClient, DaemonError} from '../src/daemon.js';
import {diagnosticTools} from '../src/diagnostics.js';

const access: Access = {principal: 'diagnostic-developer', role: 'developer', projectIds: ['project-a']};
const attached = {workId: 'work-a', memberId: 'session:instance-a'};
const member = {id: attached.memberId, kind: 'session', association: 'current', role: 'worker', sessionId: 'session-a',
  instanceId: 'instance-a', runtimeGeneration: 'generation-a', openTarget: {kind: 'session', sessionId: 'session-a', projectId: 'project-a'}};
const snapshot = () => ({workId: 'work-a', projectId: 'project-a', workRevision: 7, atomic: false, members: [{...member}], coordinator: {key: 'project-a:orchestrator'}});
const diagnostic = () => ({schema: 'ao.instructor.member-diagnostics.v1', workId: 'work-a', projectId: 'project-a', workRevision: 7,
  memberId: attached.memberId, kind: 'session', sessionId: 'session-a', instanceId: 'instance-a', runtimeGeneration: 'generation-a',
  capturedAt: '2026-10-14T17:00:00.123456789Z', atomic: true, persistedOnly: true,
  session: {mode: 'chat', activityState: 'exited', isTerminated: true, harness: 'codex', model: 'recorded-model', effort: 'high',
    providerConversationId: 'native-conversation-a', runtimeLaunchId: 'runtime-a', controllerGeneration: 'generation-a', controllerError: 'Provider HTTP 400: unsupported parameter'},
  timeline: {available: true, scope: 'session', conversationId: 'conversation-a', latestSequence: 20,
    conversationUpdatedAt: '2026-10-14T17:00:00.987654321Z', items: [] as Record<string, unknown>[], turns: [] as Record<string, unknown>[],
    oldestSequence: 0, hasMoreBefore: false, truncated: false, redacted: false}});
type Call = {method: string; url: URL};
type Reply = {status?: number; body: unknown};

async function fixture(t: TestContext, respond: (call: Call) => Reply | undefined = () => undefined, policy: Access = access) {
  const calls: Call[] = [];
  const server = createServer((request, response) => {
    const call = {method: request.method ?? 'GET', url: new URL(request.url ?? '/', 'http://127.0.0.1')};
    calls.push(call);
    const supplied = respond(call);
    const reply = supplied ?? (call.url.pathname === '/api/v1/instructor/works/work-a' ? {body: {work: {id: 'work-a', projectId: 'project-a'}}}
      : call.url.pathname === '/api/v1/instructor/works/work-a/members' ? {body: snapshot()}
        : /\/members\/[^/]+\/(?:diagnostics|timeline)$/u.test(call.url.pathname) ? {body: diagnostic()}
          : {status: 404, body: {code: 'ROUTE_NOT_FOUND', message: 'No matching route'}});
    response.writeHead(reply.status ?? 200, {'Content-Type': 'application/json'});
    response.end(JSON.stringify(reply.body));
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise<void>((resolve, reject) => { server.close((error) => error ? reject(error) : resolve()); server.closeAllConnections(); }));
  const tools = diagnosticTools(new DaemonClient({url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`}), policy);
  return {calls, tools, execute: async (name: string, input: unknown = attached): Promise<Record<string, unknown>> => {
    const tool = tools.find((candidate) => candidate.name === name);
    assert(tool);
    return await tool.execute(input) as Record<string, unknown>;
  }};
}

function timeline(result: Record<string, unknown>): Record<string, unknown> { return result.timeline as Record<string, unknown>; }

test('diagnostics read retained terminated-session facts through the exact immutable fence', async (t) => {
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/members') ? {body: {...snapshot(), members: [{...member, association: 'historical', status: 'terminated'}]}}
    : url.pathname.endsWith('/diagnostics') ? {body: {...diagnostic(), session: {...diagnostic().session, environment: {TOKEN: 'secret-env'}}, account: 'secret-account'}} : undefined);
  const result = await f.execute('ao_member_diagnostics');
  assert.deepEqual(result.session, diagnostic().session);
  assert.equal(result.capturedAt, '2026-10-14T17:00:00.123456789Z');
  assert.equal(result.atomic, true);
  assert.equal(result.persistedOnly, true);
  assert.equal(JSON.stringify(result).includes('secret-'), false);
  const request = f.calls.at(-1)!;
  assert.equal(request.url.pathname, '/api/v1/instructor/works/work-a/members/session%3Ainstance-a/diagnostics');
  assert.deepEqual(Object.fromEntries(request.url.searchParams), {projectId: 'project-a', sessionId: 'session-a', instanceId: 'instance-a', runtimeGeneration: 'generation-a'});
  assert(f.calls.every((call) => call.method === 'GET' && !call.url.pathname.startsWith('/api/v1/sessions/')));
});

test('both diagnostic tools require developer scope before any request', async (t) => {
  for (const role of ['observer', 'operator'] as const) {
    const f = await fixture(t, () => undefined, {...access, role});
    for (const name of ['ao_member_diagnostics', 'ao_member_timeline']) await assert.rejects(f.execute(name), {code: 'FORBIDDEN'});
    assert.equal(f.calls.length, 0);
  }
});

test('foreign work projects stop before member capture or any transcript request', async (t) => {
  const f = await fixture(t, () => ({body: {work: {id: 'work-a', projectId: 'foreign-project'}}}));
  await assert.rejects(f.execute('ao_member_timeline'), {code: 'OUT_OF_SCOPE'});
  assert.equal(f.calls.length, 1);
});

test('historical display IDs, unknown associations and foreign targets cannot authorize reads', async (t) => {
  for (const invalid of [
    {...member, association: 'legacy_unknown'}, {...member, association: 'historical', openTarget: undefined},
    {...member, instanceId: ''}, {...member, openTarget: {...member.openTarget, projectId: 'foreign-project'}},
    {...member, openTarget: {...member.openTarget, sessionId: 'replacement-session'}},
  ]) {
    const f = await fixture(t, ({url}) => url.pathname.endsWith('/members') ? {body: {...snapshot(), members: [invalid]}} : undefined);
    await assert.rejects(f.execute('ao_member_timeline'));
    assert.equal(f.calls.length, 2);
  }
});

test('a member replaced after capture is rejected by the authoritative route without fallback or retry', async (t) => {
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/timeline') ? {status: 409, body: {code: 'INSTRUCTOR_SESSION_INSTANCE_CHANGED', message: 'Session replaced after capture', requestId: 'instance-fence'}} : undefined);
  await assert.rejects(f.execute('ao_member_timeline'), (error: unknown) => error instanceof DaemonError
    && error.code === 'INSTRUCTOR_SESSION_INSTANCE_CHANGED' && error.status === 409 && error.requestId === 'instance-fence');
  assert.equal(f.calls.length, 3);
  assert.equal(f.calls.at(-1)?.url.searchParams.get('instanceId'), 'instance-a');
});

test('an explicitly empty captured generation is still sent as an exact required fence', async (t) => {
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/members') ? {body: {...snapshot(), members: [{...member, runtimeGeneration: undefined}]}}
    : url.pathname.endsWith('/diagnostics') ? {body: {...diagnostic(), runtimeGeneration: '', session: {...diagnostic().session, controllerGeneration: '', controllerError: 'startup failed before controller generation'}}} : undefined);
  const result = await f.execute('ao_member_diagnostics');
  assert.equal(result.runtimeGeneration, '');
  assert.deepEqual(f.calls.at(-1)?.url.searchParams.getAll('runtimeGeneration'), ['']);
});

test('reviewer and worker namespaces remain distinct even with the same display ID', async (t) => {
  const reviewMember = {...member, id: 'review:session-a:run-a', kind: 'review', reviewId: 'session-a', reviewRunId: 'run-a',
    openTarget: {...member.openTarget, kind: 'review', reviewId: 'session-a', reviewRunId: 'run-a'}};
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/members') ? {body: {...snapshot(), members: [{...member}, reviewMember]}}
    : url.pathname.endsWith('/timeline') ? {body: {...diagnostic(), memberId: reviewMember.id, kind: 'review',
      review: {reviewId: 'session-a', reviewRunId: 'run-a', status: 'failed', controllerError: 'native reviewer failed', account: 'secret-account'},
      timeline: {...diagnostic().timeline, scope: 'review', scopeNote: 'Review history may include other passes.'}}} : undefined);
  const result = await f.execute('ao_member_timeline', {...attached, memberId: reviewMember.id});
  assert.equal(result.kind, 'review');
  assert.deepEqual(result.review, {reviewId: 'session-a', reviewRunId: 'run-a', status: 'failed', controllerError: 'native reviewer failed'});
  assert.equal(timeline(result).scope, 'review');
  assert.equal(timeline(result).scopeNote, 'Review history may include other passes.');
  assert(f.calls.at(-1)?.url.pathname.includes('review%3Asession-a%3Arun-a'));
});

test('review owner/run mismatches fail closed before exposing a different review', async (t) => {
  const reviewMember = {...member, id: 'review:review-a:run-a', kind: 'review', reviewId: 'review-a', reviewRunId: 'run-a',
    openTarget: {...member.openTarget, kind: 'review', reviewId: 'review-a', reviewRunId: 'run-a'}};
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/members') ? {body: {...snapshot(), members: [reviewMember]}}
    : url.pathname.endsWith('/timeline') ? {body: {...diagnostic(), memberId: reviewMember.id, kind: 'review',
      review: {reviewId: 'review-a', reviewRunId: 'foreign-run'}, timeline: {...diagnostic().timeline, scope: 'review'}}} : undefined);
  await assert.rejects(f.execute('ao_member_timeline', {...attached, memberId: reviewMember.id}), {code: 'DAEMON_CONTRACT'});
});

test('orchestrator timelines clearly retain project scope across works', async (t) => {
  const coordinator = {...member, id: 'coordinator:instance-a', role: 'orchestrator'};
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/members') ? {body: {...snapshot(), members: [], coordinator: {key: 'project-a:orchestrator', current: coordinator}}}
    : url.pathname.endsWith('/timeline') ? {body: {...diagnostic(), memberId: coordinator.id, timeline: {...diagnostic().timeline, scope: 'project', scopeNote: 'Project-wide history; items are not attributed to this work.'}}} : undefined);
  const result = await f.execute('ao_member_timeline', {...attached, memberId: coordinator.id});
  assert.equal(timeline(result).scope, 'project');
  assert.match(String(timeline(result).scopeNote), /not attributed to this work/u);
});

test('a retained stopped orchestrator remains diagnosable without a current coordinator', async (t) => {
  const stopped = {...member, id: 'coordinator:instance-a', role: 'orchestrator', association: 'historical', status: 'terminated'};
  const captured = {...snapshot(), members: [], coordinator: {key: 'project-a:orchestrator', stopped}};
  assert.equal(Object.hasOwn(captured.coordinator, 'current'), false);
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/members') ? {body: captured}
    : url.pathname.endsWith('/diagnostics') || url.pathname.endsWith('/timeline') ? {body: {...diagnostic(), memberId: stopped.id,
      timeline: {...diagnostic().timeline, scope: 'project', scopeNote: 'Retained project-wide history; items are not attributed to this work.'}}} : undefined);
  for (const name of ['ao_member_diagnostics', 'ao_member_timeline']) {
    const result = await f.execute(name, {...attached, memberId: stopped.id});
    assert.equal((result.session as Record<string, unknown>).isTerminated, true);
    assert.equal(timeline(result).scope, 'project');
    assert.match(String(timeline(result).scopeNote), /not attributed to this work/u);
  }
  const reads = f.calls.filter((call) => /\/(?:diagnostics|timeline)$/u.test(call.url.pathname));
  assert.equal(reads.length, 2);
  assert(reads.every((call) => call.url.pathname.includes('/coordinator%3Ainstance-a/')
    && call.url.searchParams.get('projectId') === 'project-a'
    && call.url.searchParams.get('sessionId') === 'session-a'
    && call.url.searchParams.get('instanceId') === 'instance-a'
    && call.url.searchParams.get('runtimeGeneration') === 'generation-a'));
  assert(f.calls.every((call) => call.method === 'GET' && call.url.pathname.startsWith('/api/v1/instructor/')));
});

test('stopped coordinator metadata cannot authorize a replacement or an unproven incarnation', async (t) => {
  for (const stopped of [{...member, id: 'coordinator:instance-a', association: 'historical', instanceId: ''},
    {...member, id: 'coordinator:instance-a', association: 'historical', openTarget: undefined}]) {
    const f = await fixture(t, ({url}) => url.pathname.endsWith('/members') ? {body: {...snapshot(), members: [], coordinator: {key: 'project-a:orchestrator', stopped}}} : undefined);
    await assert.rejects(f.execute('ao_member_diagnostics', {...attached, memberId: stopped.id}));
    assert.equal(f.calls.length, 2);
  }
});

test('old daemons fail closed on new routes and diagnostic errors redact credentials', async (t) => {
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/timeline') ? {status: 404, body: {code: 'ROUTE_NOT_FOUND', message: 'No scoped API; Authorization: Bearer secret-auth-token', requestId: 'missing-route'}} : undefined);
  await assert.rejects(f.execute('ao_member_timeline'), (error: unknown) => error instanceof DaemonError && error.status === 404
    && error.code === 'ROUTE_NOT_FOUND' && error.requestId === 'missing-route' && !error.message.includes('secret-auth-token'));
  assert.equal(f.calls.length, 3);
  assert(f.calls.every((call) => call.method === 'GET' && call.url.pathname.startsWith('/api/v1/instructor/')));
});

test('escaped quotes inside JSON credentials are fully redacted from diagnostic text and daemon errors', async (t) => {
  const text = `${JSON.stringify({password: 'left"secret-suffix'})}\n${JSON.stringify({password: 'Bearer prefix"right-secret'})}`;
  let fail = false;
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/timeline') ? fail
    ? {status: 409, body: {code: 'INSTRUCTOR_CONFLICT', message: text, requestId: 'escaped-secret-error'}}
    : {body: {...diagnostic(), timeline: {...diagnostic().timeline, items: [{kind: 'message', id: 'escaped-message', sequence: 1, revision: 1, role: 'assistant', text}]}}} : undefined);
  const result = await f.execute('ao_member_timeline');
  const encoded = JSON.stringify(result);
  for (const secret of ['secret-suffix', 'right-secret', 'prefix', 'left']) assert.equal(encoded.includes(secret), false, secret);
  assert.equal(timeline(result).redacted, true);
  fail = true;
  await assert.rejects(f.execute('ao_member_timeline'), (error: unknown) => error instanceof DaemonError
    && error.code === 'INSTRUCTOR_CONFLICT' && error.requestId === 'escaped-secret-error'
    && !['secret-suffix', 'right-secret', 'prefix', 'left'].some((secret) => error.message.includes(secret)));
});

test('partial PEM and multiline or unfinished quoted credentials are redacted from native daemon errors', async (t) => {
  const pemStart = (kind: string) => '-'.repeat(5) + `BEGIN ${kind}PRIVATE KEY` + '-'.repeat(5);
  const cases = [
    'Native failure: ' + pemStart('') + '\nsecret-partial-key',
    'Native failure: ' + pemStart('RSA ') + '\n' + 'secret-long-key'.repeat(2000) + '\n-----END RSA PRIVATE KEY-----',
    'Native failure: client_secret="secret-first-line\nsecret-second-line"',
    'Native failure: client_secret="secret-first-line\r\nsecret-second-line',
    'Native failure: ' + JSON.stringify({password: 'https://example.test/?token=prefix"right-secret'}),
    'Native failure: password="secret-trailing-backslash\\',
  ];
  for (const message of cases) {
    const f = await fixture(t, ({url}) => url.pathname.endsWith('/timeline') ? {status: 400,
      body: {code: 'NATIVE_PROVIDER_ERROR', message, requestId: 'native-error-a'}} : undefined);
    await assert.rejects(f.execute('ao_member_timeline'), (error: unknown) => error instanceof DaemonError
      && error.code === 'NATIVE_PROVIDER_ERROR' && error.status === 400 && error.requestId === 'native-error-a'
      && error.message.startsWith('Native failure:') && error.message.includes('[REDACTED')
      && !['secret-', 'right-secret', 'prefix'].some((secret) => error.message.includes(secret)));
  }
});

test('bounded timeline projection preserves revisions and failure facts while excluding credentials and arbitrary payloads', async (t) => {
  const native = diagnostic();
  native.session.controllerError = 'Provider HTTP 400 api_key="secret-api-key"; retry disabled';
  native.timeline.items = [
    {kind: 'message', id: 'message-a', turnId: 'turn-a', sequence: 20, revision: 4, role: 'assistant', origin: 'provider', streaming: true,
      text: 'See https://url-user:url-password@example.test/debug?token=secret-query&ok=yes; password=secret-password; ' + 'ghp_' + 'abcdefghijklmnopqrstuvwxyz0123456789' + '\nCookie: first=secret-cookie; session=secret-cookie-session',
      createdAt: '2026-10-14T17:00:00.123456789Z', content: [{data: 'secret-attachment'}]},
    {kind: 'activity', id: 'activity-a', turnId: 'turn-a', sequence: 19, revision: 2, activityKind: 'error', status: 'failed', text: 'Native provider returned 400: unsupported parameter',
      detail: {terminalInput: 'secret-terminal', arguments: {token: 'secret-mcp-token'}, reasoning: 'secret-reasoning'}, rawCommand: 'secret-command'},
  ];
  native.timeline.turns = [{id: 'turn-a', state: 'failed', errorMessage: 'Authorization: Bearer secret-turn-token', diff: {patch: 'secret-patch'}, plan: {reasoning: 'secret-plan'}}];
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/timeline') ? {body: native} : undefined);
  const result = await f.execute('ao_member_timeline');
  const encoded = JSON.stringify(result);
  assert.equal(encoded.includes('secret-'), false);
  assert.equal(encoded.includes('ghp_'), false);
  assert.equal(encoded.includes('url-user'), false);
  assert.equal(encoded.includes('url-password'), false);
  assert.equal(timeline(result).redacted, true);
  const items = timeline(result).items as Record<string, unknown>[];
  assert.equal(items[0]?.revision, 4);
  assert.equal(items[0]?.streaming, true);
  assert.equal(items[0]?.createdAt, '2026-10-14T17:00:00.123456789Z');
  assert.equal(items[1]?.text, 'Native provider returned 400: unsupported parameter');
  assert.match(String((result.session as Record<string, unknown>).controllerError), /HTTP 400/u);
});

test('excluded reasoning and raw tool activities cannot hide the admitted page cursor', async (t) => {
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/timeline') ? {body: {...diagnostic(), timeline: {...diagnostic().timeline,
    oldestSequence: 10, hasMoreBefore: true, nextBeforeSequence: 10,
    items: [{kind: 'activity', id: 'reasoning-a', sequence: 10, revision: 3, activityKind: 'reasoning', text: 'secret-reasoning'},
      {kind: 'activity', id: 'mcp-a', sequence: 11, revision: 1, activityKind: 'mcp_tool', text: 'secret-mcp-result', detail: {result: 'secret-result'}}]}}} : undefined);
  const result = await f.execute('ao_member_timeline', {...attached, beforeSequence: 30, limit: 2});
  assert.deepEqual(timeline(result).items, []);
  assert.equal(timeline(result).omittedItems, 2);
  assert.equal(timeline(result).hasMoreBefore, true);
  assert.equal(timeline(result).nextBeforeSequence, 10);
  assert.equal(timeline(result).truncated, true);
  assert.equal(JSON.stringify(result).includes('secret-'), false);
  assert.equal(f.calls.at(-1)?.url.searchParams.get('beforeSequence'), '30');
  assert.equal(f.calls.at(-1)?.url.searchParams.get('limit'), '2');
});

test('UTF-8 text and total page budgets remain bounded while every admitted item keeps its identity', async (t) => {
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/timeline') ? {body: {...diagnostic(), timeline: {...diagnostic().timeline,
    items: Array.from({length: 100}, (_, index) => ({kind: 'message', id: `message-${index}`, sequence: index + 1, revision: 1, role: 'assistant', text: '測試😀'.repeat(1500)}))}}} : undefined);
  const result = await f.execute('ao_member_timeline', {...attached, limit: 100});
  const items = timeline(result).items as {id: string; text: string}[];
  assert.equal(items.length, 100);
  assert.equal(items[99]?.id, 'message-99');
  assert(items.every((item) => Buffer.byteLength(item.text, 'utf8') <= 8192 && !item.text.includes('\uFFFD')));
  assert(Buffer.byteLength(JSON.stringify(result), 'utf8') <= 256 * 1024);
  assert.equal(timeline(result).truncated, true);
  assert.equal(result.workId, 'work-a');
  assert.equal(result.runtimeGeneration, 'generation-a');
});

test('unavailable TUI history is explicit and never triggers provider startup or filesystem access', async (t) => {
  const f = await fixture(t, ({url}) => url.pathname.endsWith('/timeline') ? {body: {...diagnostic(), session: {...diagnostic().session, mode: 'tui'},
    timeline: {...diagnostic().timeline, available: false, reason: 'TUI transcript is not persisted.'}}} : undefined);
  const result = await f.execute('ao_member_timeline');
  assert.equal(timeline(result).available, false);
  assert.equal(timeline(result).reason, 'TUI transcript is not persisted.');
  assert(f.calls.every((call) => call.method === 'GET' && call.url.pathname.startsWith('/api/v1/instructor/')));
});

test('strict tool schemas reject authority injection and invalid page sizes before HTTP', async (t) => {
  const f = await fixture(t);
  for (const injection of [{approval: 'go'}, {instanceId: 'invented-instance'}, {sessionId: 'foreign-session'}, {runtimeGeneration: ''}, {path: '/provider/auth.json'}]) {
    await assert.rejects(f.execute('ao_member_diagnostics', {...attached, ...injection}), {name: 'ZodError'});
  }
  for (const limit of [0, 101, 1.5]) await assert.rejects(f.execute('ao_member_timeline', {...attached, limit}), {name: 'ZodError'});
  for (const beforeSequence of [-1, Number.MAX_SAFE_INTEGER + 1]) await assert.rejects(f.execute('ao_member_timeline', {...attached, beforeSequence}), {name: 'ZodError'});
  assert.equal(f.calls.length, 0);
});

test('mixed owners, runtime generations and non-advancing cursors are refused', async (t) => {
  for (const changed of [{workId: 'foreign-work'}, {projectId: 'foreign-project'}, {memberId: 'session:replacement'},
    {sessionId: 'replacement-session'}, {instanceId: 'replacement-instance'}, {runtimeGeneration: 'replacement-generation'},
    {atomic: false}, {persistedOnly: false}, {timeline: {...diagnostic().timeline, oldestSequence: 20, hasMoreBefore: true, nextBeforeSequence: 20}},
    {timeline: {...diagnostic().timeline, oldestSequence: 0, hasMoreBefore: true, nextBeforeSequence: 0}},
  ]) {
    const f = await fixture(t, ({url}) => url.pathname.endsWith('/timeline') ? {body: {...diagnostic(), ...changed}} : undefined);
    await assert.rejects(f.execute('ao_member_timeline', {...attached, beforeSequence: 20}), {code: 'DAEMON_CONTRACT'});
  }
});

import assert from 'node:assert/strict';
import test from 'node:test';
import {AdapterError, record} from '../src/core.js';
import {DaemonError} from '../src/daemon.js';
import {details, dispatch, fixture, foreignWork, operator, registry, standard, work} from './operations-fixture.js';

const commandInput = {workId: 'work-one', command: 'Weiter', snapshotId: 'snapshot-original', idempotencyKey: 'intent-original'};
const intakeInput = {projectId: 'project-one', name: 'Task', goal: 'Requested behavior', sourceRef: 'intake-original', snapshotId: 'snapshot-original', idempotencyKey: 'intent-original'};

test('runtime identity preserves observed fields and missing version remains unknown', async (t) => {
  const f = await fixture(t, (call) => ({body: {status: call.path === '/healthz' ? 'ok' : 'ready', service: 'ao', pid: 33888,
    executablePath: 'C:\\ao\\ao.exe', environment: {secret: 'unrelated-secret'}}}));
  const result = await f.execute('ao_runtime_info', {});
  assert.equal(result.version, 'unknown');
  assert.deepEqual(result.health, {status: 'ok', service: 'ao', pid: 33888, executablePath: 'C:\\ao\\ao.exe'});
  assert.equal(JSON.stringify(result).includes('unrelated-secret'), false);
});

test('runtime readiness failure is observable without claiming readiness', async (t) => {
  const f = await fixture(t, (call) => call.path === '/healthz' ? {body: {status: 'ok', pid: 42}} :
    {status: 503, body: {code: 'NOT_READY', message: 'Initializing', requestId: 'probe-request'}});
  const result = await f.execute('ao_runtime_info', {});
  assert.deepEqual(result.readiness, {available: false, code: 'NOT_READY', message: 'Initializing', status: 503, requestId: 'probe-request'});
});

test('status filters projects, portfolio metadata and nested unknown values', async (t) => {
  const f = await fixture(t, () => ({body: {id: 'snapshot-original', baseId: 'snapshot-before', works: [
    {...work, summary: {secret: 'nested-secret'}, gate: {id: 'gate-one', kind: 'human', exactApproval: 'secret-approval', secret: 'nested-secret'},
      evidence: [{id: 'own-evidence', workId: work.id, kind: 'check', ref: 'own-reference', tokens: ['nested-secret']}, {id: 'foreign-evidence', workId: foreignWork.id, ref: 'foreign-private-reference'}],
      launches: [{role: 'worker', requested: {harness: 'codex', model: 'native-model', effort: 'high', env: 'nested-secret'}}]},
    foreignWork,
  ], metrics: {active: 999}, recoveries: {'work-other': {reason: 'foreign-private-recovery'}},
  commands: [{name: 'foreign-private-alias', action: 'weiter'}], commandConflicts: ['foreign-private-alias'], secret: 'portfolio-secret'}}));
  const result = await f.execute('ao_instructor_status', {projectId: 'project-one', baseId: 'snapshot-before'});
  assert.equal(result.id, 'snapshot-original');
  assert.equal(result.baseId, 'snapshot-before');
  assert.deepEqual(result.scope, ['project-one']);
  assert.equal((result.works as unknown[]).length, 1);
  const encoded = JSON.stringify(result);
  for (const forbidden of ['foreign-private', 'secret', 'metrics', 'recoveries', 'publicationGrants', 'exactApproval', 'commands', 'summary']) assert.equal(encoded.includes(forbidden), false, forbidden);
  assert.equal(f.calls[0]?.path, '/api/v1/instructor/status?baseId=snapshot-before');
});

test('project_view never returns the full project config or credential-bearing remote URLs', async (t) => {
  const f = await fixture(t, () => ({body: {project: {id: 'project-one', name: 'Local', kind: 'single_repo', path: 'C:\\repo',
    repo: 'https://secret-token@github.com/owner/repo', config: {API_KEY: 'secret-config'}, workspaceRepos: [{repo: 'private-repo'}]}}}));
  assert.deepEqual(await f.execute('ao_project_view', {projectId: 'project-one'}), {project: {id: 'project-one', name: 'Local', kind: 'single_repo', path: 'C:\\repo'}});
});

test('out-of-scope project inputs cause no HTTP request', async (t) => {
  const f = await fixture(t, standard);
  for (const name of ['ao_project_view', 'ao_instructor_status']) {
    await assert.rejects(f.execute(name, {projectId: 'project-other'}), (error: unknown) => error instanceof AdapterError && error.code === 'OUT_OF_SCOPE');
  }
  assert.equal(f.calls.length, 0);
});

test('work trace filters exact-work events and receipts and preserves accepted versus started', async (t) => {
  const f = await fixture(t, () => ({body: {...details,
    events: [...details.events, {workId: foreignWork.id, detail: 'foreign-private-event'}],
    dispatches: [{...dispatch, nativeReceipt: {...dispatch.nativeReceipt, arbitrary: {secret: 'nested-secret'}}},
      {...dispatch, workId: foreignWork.id, projectId: 'project-other', message: 'foreign-private-dispatch'},
      {...dispatch, workId: work.id, projectId: 'project-other', message: 'foreign-private-project'}]}}));
  const result = await f.execute('ao_work_trace', {workId: 'work-one', includeMembers: false, includeRecovery: false});
  const receipts = result.dispatches as Record<string, unknown>[];
  assert.equal(receipts.length, 1);
  assert.deepEqual(receipts[0]?.nativeReceipt, dispatch.nativeReceipt);
  assert.equal(Object.hasOwn(receipts[0]?.nativeReceipt as object, 'startedAt'), false);
  assert.equal(JSON.stringify(result).includes('foreign-private'), false);
  assert.equal(JSON.stringify(result).includes('nested-secret'), false);
});

test('trace retains mechanical phase failures without exposing nested replay material', async (t) => {
  const f = await fixture(t, () => ({body: {...details, work: {...work, phaseActions: [
    {id: 'phase-action', kind: 'check', phaseSequence: 4, producer: 'worker-one', state: 'uncertain',
      error: 'Check process disappeared before completion', startedAt: '2026-10-14T12:00:00Z',
      approval: 'private-approval', input: {token: 'private-token'}, completedAt: {secret: 'private-nested'}},
  ]}}}));
  const result = await f.execute('ao_work_trace', {workId: work.id, includeMembers: false, includeRecovery: false});
  assert.deepEqual((result.work as Record<string, unknown>).phaseActions, [{
    id: 'phase-action', kind: 'check', phaseSequence: 4, producer: 'worker-one', state: 'uncertain',
    error: 'Check process disappeared before completion', startedAt: '2026-10-14T12:00:00Z',
  }]);
  assert.equal(JSON.stringify(result).includes('private-'), false);
});

test('members and recovery are scoped projections; optional endpoint errors keep request IDs', async (t) => {
  const f = await fixture(t, (call) => {
    if (call.path.endsWith('/members')) return {body: {workId: work.id, projectId: work.projectId, workRevision: 8, atomic: false,
      coordinator: {key: 'project:orchestrator', extra: 'nested-secret',
        stopped: {id: 'coordinator:retained-instance', role: 'orchestrator', association: 'historical', status: 'terminated', instanceId: 'retained-instance', token: 'nested-secret'},
        current: {id: 'coordinator', role: 'orchestrator', confirmed: {harness: 'codex', model: 'actual', secret: 'nested-secret'}}},
      members: [{id: 'worker', role: 'worker', requested: {model: 'requested'}, confirmed: null, secret: 'nested-secret',
        openTarget: {kind: 'session', projectId: 'project-other', sessionId: 'foreign-private-session'}}]}};
    if (call.path.endsWith('/recovery')) return {status: 501, body: {code: 'NOT_IMPLEMENTED', message: 'Recovery observations unavailable', requestId: 'recovery-request'}};
    return standard(call);
  });
  const result = await f.execute('ao_work_trace', {workId: work.id});
  assert.equal(record(record(record(result.members).observation).coordinator).stopped !== undefined, true);
  assert.deepEqual(result.recovery, {available: false, code: 'NOT_IMPLEMENTED', message: 'Recovery observations unavailable', status: 501, requestId: 'recovery-request'});
  assert.equal(JSON.stringify(result.members).includes('nested-secret'), false);
  assert.equal(JSON.stringify(result.members).includes('foreign-private'), false);
  assert.equal(JSON.stringify(result.members).includes('"confirmed":null'), false);
});

test('trace refuses a foreign work and does not query its optional endpoints', async (t) => {
  const f = await fixture(t, () => ({body: {work: foreignWork, events: [], dispatches: []}}));
  await assert.rejects(f.execute('ao_work_trace', {workId: 'work-other'}), (error: unknown) => error instanceof AdapterError && error.code === 'OUT_OF_SCOPE');
  assert.equal(f.calls.length, 1);
});

test('optional observations of a different work are reported unavailable', async (t) => {
  const f = await fixture(t, (call) => call.path.endsWith('/members') ? {body: {workId: 'work-other', projectId: 'project-one', members: []}} :
    call.path.endsWith('/recovery') ? {body: {workId: 'work-other', reason: 'foreign-private-recovery'}} : standard(call));
  const result = await f.execute('ao_work_trace', {workId: work.id});
  assert.equal((result.members as Record<string, unknown>).available, false);
  assert.equal((result.recovery as Record<string, unknown>).available, false);
  assert.equal(JSON.stringify(result).includes('foreign-private-recovery'), false);
});

test('mutations require operator role even when called outside the MCP dispatcher', async (t) => {
  const f = await fixture(t, standard, {...operator, role: 'observer'});
  await assert.rejects(f.execute('ao_create_work', intakeInput), (error: unknown) => error instanceof AdapterError && error.code === 'FORBIDDEN');
  await assert.rejects(f.execute('ao_work_command', commandInput), (error: unknown) => error instanceof AdapterError && error.code === 'FORBIDDEN');
  assert.equal(f.calls.length, 0);
});

test('work command passes canonical action, exact work, original snapshot and key once', async (t) => {
  const f = await fixture(t, standard);
  const result = await f.execute('ao_work_command', {...commandInput, command: 'weiter alle'});
  assert.deepEqual(f.calls.filter((call) => call.method === 'POST'), [{method: 'POST', path: '/api/v1/instructor/commands',
    body: {command: 'weiter', snapshotId: 'snapshot-original', workIds: ['work-one'], idempotencyKey: 'intent-original'}}]);
  assert.equal(result.execution, 'unknown');
  assert.equal(result.batchId, 'batch-one');
});

test('command names and policy aliases must resolve to permitted canonical actions', async (t) => {
  const f = await fixture(t, (call) => call.path.endsWith('/work-one') ? {body: {...details, work: {...work,
    policy: {commands: [{name: 'Continue safely', aliases: ['friendly continue'], action: 'go'}, {name: 'Resume work', aliases: ['resume'], action: 'weiter'}]}}}} : standard(call));
  for (const command of ['go', 'friendly continue', 'Continue safely', 'neu', 'weiter work-other', 'recovery']) {
    await assert.rejects(f.execute('ao_work_command', {...commandInput, command}), (error: unknown) => error instanceof AdapterError && error.code === 'COMMAND_NOT_ALLOWED');
  }
  assert.equal(f.calls.some((call) => call.method === 'POST'), false);
  await f.execute('ao_work_command', {...commandInput, command: 'resume'});
  assert.equal(f.calls.find((call) => call.method === 'POST')?.body?.command, 'weiter');
});

test('ambiguous aliases cannot be escalated by registry ordering', async (t) => {
  const f = await fixture(t, (call) => call.path === '/api/v1/instructor/commands' && call.method === 'GET' ?
    {body: {commands: [...registry, {name: 'Misleading', aliases: ['Weiter'], action: 'go'}]}} : standard(call));
  await assert.rejects(f.execute('ao_work_command', commandInput), (error: unknown) => error instanceof AdapterError && error.code === 'COMMAND_NOT_ALLOWED');
  assert.equal(f.calls.some((call) => call.method === 'POST'), false);
});

test('strict schemas reject approval, publication and cohort injections before HTTP', async (t) => {
  const f = await fixture(t, standard);
  for (const injected of [{approval: 'GO'}, {approvals: []}, {workIds: ['work-other']}, {publicationOperation: 'merge'}, {requesterSessionId: 'human'}]) {
    await assert.rejects(f.execute('ao_work_command', {...commandInput, ...injected}));
  }
  await assert.rejects(f.execute('ao_create_work', {...intakeInput, policy: {transitions: []}}));
  assert.equal(f.calls.length, 0);
});

test('stale snapshot errors retain daemon code, status and request ID without retry', async (t) => {
  const f = await fixture(t, (call) => call.method === 'POST' ? {status: 409, body: {code: 'INSTRUCTOR_CONFLICT', message: 'Work revision changed', requestId: 'stale-request'}} : standard(call));
  await assert.rejects(f.execute('ao_work_command', commandInput), (error: unknown) => error instanceof DaemonError && error.status === 409 && error.code === 'INSTRUCTOR_CONFLICT' && error.requestId === 'stale-request');
  assert.equal(f.calls.filter((call) => call.method === 'POST').length, 1);
  assert.equal(f.calls.some((call) => call.path.includes('/status')), false);
});

test('a snapshot predating an authorized work cannot leak portfolio choices through command errors', async (t) => {
  const leakedMessage = 'work reference work-one matched 0 works; choose: foreign-private-name [work-other], foreign-secret-project [work-confidential]';
  const f = await fixture(t, (call) => call.method === 'POST' ? {status: 400, body: {
    code: 'INSTRUCTOR_VALIDATION', message: leakedMessage, requestId: 'pre-intake-snapshot-request',
  }} : standard(call));
  for (const [name, input] of [
    ['ao_work_command', {...commandInput, snapshotId: 'snapshot-before-work-existed'}],
    ['ao_create_work', {...intakeInput, snapshotId: 'snapshot-before-work-existed'}],
  ] as const) {
    await assert.rejects(f.execute(name, input), (error: unknown) => {
      assert.ok(error instanceof DaemonError);
      assert.equal(error.code, 'INSTRUCTOR_VALIDATION');
      assert.equal(error.status, 400);
      assert.equal(error.requestId, 'pre-intake-snapshot-request');
      assert.match(error.message, /scoped Instructor command failed/);
      for (const marker of ['foreign-private-name', 'work-other', 'foreign-secret-project', 'work-confidential']) {
        assert.equal(`${String(error)} ${JSON.stringify(error)}`.includes(marker), false, marker);
      }
      assert.equal(error.cause, undefined, 'The raw out-of-scope error must not be retained as a cause.');
      return true;
    });
  }
  const writes = f.calls.filter((call) => call.method === 'POST');
  assert.equal(writes.length, 2, 'Each explicit call makes one attempt, without automatic retry.');
  assert.ok(writes.every((call) => call.body?.snapshotId === 'snapshot-before-work-existed'));
  assert.ok(writes.every((call) => call.body?.idempotencyKey === 'intent-original'));
});

test('command registry errors also preserve envelope metadata without exposing raw daemon text', async (t) => {
  const f = await fixture(t, (call) => call.path === '/api/v1/instructor/commands' ? {status: 503, body: {
    code: 'INSTRUCTOR_UNAVAILABLE', message: 'foreign-private-policy registry unavailable', requestId: 'registry-request',
  }} : standard(call));
  await assert.rejects(f.execute('ao_work_command', commandInput), (error: unknown) => {
    assert.ok(error instanceof DaemonError);
    assert.equal(error.code, 'INSTRUCTOR_UNAVAILABLE');
    assert.equal(error.status, 503);
    assert.equal(error.requestId, 'registry-request');
    assert.equal(error.message.includes('foreign-private-policy'), false);
    return true;
  });
  assert.equal(f.calls.some((call) => call.method === 'POST'), false);
});

test('manual intake sends explicit provenance and never exposes a returned portfolio', async (t) => {
  const created = {...work, sourceRef: 'intake-original'};
  const f = await fixture(t, (call) => call.method === 'POST' ? {body: {snapshot: {id: 'snapshot-after', works: [created, foreignWork, {...work, id: 'other-own-work', sourceRef: 'different-intake'}],
    metrics: {active: 100}, recoveries: {secret: 'portfolio-secret'}}, dispatches: [dispatch, {...dispatch, workId: foreignWork.id, projectId: foreignWork.projectId, message: 'foreign-private-dispatch'}],
    skipped: ['work-other: foreign-private-reason']}} : standard(call));
  const result = await f.execute('ao_create_work', intakeInput);
  assert.deepEqual(f.calls.find((call) => call.method === 'POST')?.body, {command: 'neu', snapshotId: 'snapshot-original', idempotencyKey: 'intent-original',
    new: {projectId: 'project-one', name: 'Task', goal: 'Requested behavior', source: 'manual', sourceRef: 'intake-original'}});
  assert.equal(result.intakeDeduplication, 'project/source/sourceRef');
  assert.equal(result.execution, 'unknown');
  assert.equal((result.snapshot as {works: unknown[]}).works.length, 1);
  for (const forbidden of ['foreign-private', 'different-intake', 'portfolio-secret', 'metrics', 'recoveries']) assert.equal(JSON.stringify(result).includes(forbidden), false);
});

test('manual source references are opaque and retain slash, spaces and exact deduplication identity', async (t) => {
  const identities = new Map<string, string>();
  const f = await fixture(t, (call) => {
    if (call.method !== 'POST') return standard(call);
    const intake = call.body?.new as {sourceRef: string};
    const sourceRef = intake.sourceRef;
    let id = identities.get(sourceRef);
    if (!id) {id = `work-intake-${identities.size + 1}`; identities.set(sourceRef, id);}
    return {body: {snapshot: {id: 'snapshot-after', works: [{...work, id, sourceRef}]}, dispatches: [], skipped: []}};
  });
  for (const sourceRef of ['ticket/123', 'Distinct name', ' Distinct name ', 'Distinct name ']) {
    const input = {...intakeInput, sourceRef};
    const first = await f.execute('ao_create_work', input);
    const replay = await f.execute('ao_create_work', input);
    const firstWork = ((first.snapshot as {works: typeof work[]}).works)[0];
    const replayWork = ((replay.snapshot as {works: typeof work[]}).works)[0];
    assert.equal(firstWork?.sourceRef, sourceRef);
    assert.equal(replayWork?.sourceRef, sourceRef);
    assert.equal(firstWork?.id, replayWork?.id);
    const writes = f.calls.filter((call) => call.method === 'POST').slice(-2);
    assert.ok(writes.every((call) => (call.body?.new as {sourceRef: string}).sourceRef === sourceRef));
  }
  assert.equal(identities.size, 4, 'Whitespace-distinct references must not be merged by adapter normalization.');
});

test('manual source references reject empty and oversized values before any daemon request', async (t) => {
  const f = await fixture(t, standard);
  for (const sourceRef of ['', 'x'.repeat(181)]) await assert.rejects(f.execute('ao_create_work', {...intakeInput, sourceRef}));
  assert.equal(f.calls.length, 0);
});

test('work command filters a mixed command result down to its exact work', async (t) => {
  const otherOwn = {...work, id: 'other-own-work', goal: 'unselected-private-goal'};
  const f = await fixture(t, (call) => call.method === 'POST' ? {body: {batchId: 'batch-one', snapshot: {id: 'snapshot-result', works: [work, foreignWork, otherOwn], metrics: {active: 99}},
    details: [details, {work: foreignWork, events: [], dispatches: []}, {work: otherOwn, events: [], dispatches: []}],
    dispatches: [dispatch, {...dispatch, workId: 'other-own-work'}, {...dispatch, projectId: 'project-other'}], skipped: ['work-one: action unavailable in ready', 'other-own-work: unselected-private-reason']}} : standard(call));
  const result = await f.execute('ao_work_command', commandInput);
  assert.equal((result.dispatches as unknown[]).length, 1);
  assert.equal((result.details as unknown[]).length, 1);
  assert.deepEqual(result.skipped, ['work-one: action unavailable in ready']);
  for (const forbidden of ['foreign-private', 'unselected-private', 'other-own-work', 'metrics']) assert.equal(JSON.stringify(result).includes(forbidden), false);
});

test('idempotency conflict is surfaced exactly once with the unchanged key', async (t) => {
  const f = await fixture(t, (call) => call.method === 'POST' ? {status: 400, body: {code: 'INSTRUCTOR_VALIDATION', message: 'idempotencyKey already used for a different command', requestId: 'key-conflict'}} : standard(call));
  await assert.rejects(f.execute('ao_work_command', commandInput), (error: unknown) => error instanceof DaemonError && error.code === 'INSTRUCTOR_VALIDATION' && error.requestId === 'key-conflict');
  const writes = f.calls.filter((call) => call.method === 'POST');
  assert.equal(writes.length, 1);
  assert.equal(writes[0]?.body?.idempotencyKey, commandInput.idempotencyKey);
});

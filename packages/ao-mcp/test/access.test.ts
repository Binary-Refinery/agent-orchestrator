import assert from 'node:assert/strict';
import {createServer, type ServerResponse} from 'node:http';
import {test} from 'node:test';
import {assertProject, requireRole, type Access, type Role} from '../src/core.js';
import {DaemonClient} from '../src/daemon.js';
import {operationTools} from '../src/operations.js';
import {developmentTools} from '../src/development.js';
import {createToolCatalog} from '../src/server.js';

const client = new DaemonClient({url: 'http://127.0.0.1:1'});
const access: Access = {principal: 'embedded-host', role: 'developer', projectIds: ['project-a']};
const invalidRoles: unknown[] = ['admin', 'Developer', '', 'constructor', 'toString', '__proto__', undefined, null, 2, {}];

test('unknown and inherited access roles fail closed for every required role', () => {
  for (const role of invalidRoles) {
    const malformed = {...access, role} as Access;
    for (const required of ['observer', 'operator', 'developer'] as const) {
      assert.throws(() => requireRole(malformed, required), {code: 'FORBIDDEN'});
    }
    assert.throws(() => createToolCatalog(client, malformed), {code: 'FORBIDDEN'});
    assert.throws(() => requireRole(access, role as Role), {code: 'FORBIDDEN'});
  }
});

test('exported tool execution rejects malformed embedded access before daemon I/O', async () => {
  for (const role of invalidRoles) {
    const malformed = {...access, role} as Access;
    const command = operationTools(client, malformed).find((tool) => tool.name === 'ao_work_command')!;
    const edit = developmentTools(client, malformed).find((tool) => tool.name === 'ao_workspace_update')!;
    await assert.rejects(command.execute({workId: 'work-a', command: 'weiter', snapshotId: 'snapshot-a', idempotencyKey: 'attempt-a'}), {code: 'FORBIDDEN'});
    await assert.rejects(edit.execute({workId: 'work-a', sessionId: 'session-a', path: 'main.ts', content: 'changed', expectedFileFingerprint: 'original'}), {code: 'FORBIDDEN'});
  }
});

test('previously discovered tools recheck a role changed by their embedding host', async () => {
  const mutable = {...access};
  const command = createToolCatalog(client, mutable).find((tool) => tool.name === 'ao_work_command')!;
  mutable.role = 'administrator' as Role;
  await assert.rejects(command.execute({workId: 'work-a', command: 'weiter', snapshotId: 'snapshot-a', idempotencyKey: 'attempt-a'}), {code: 'FORBIDDEN'});
});

test('recognized roles retain their cumulative tool inventory', () => {
  for (const [role, count] of [['observer', 4], ['operator', 6], ['developer', 14]] as const) {
    assert.equal(createToolCatalog(client, {...access, role}).length, count);
  }
});

test('malformed embedding project scopes cannot authorize substring matches or status reads', async () => {
  for (const projectIds of ['project-a-other', null, undefined, {}, ['project-a', 1]]) {
    const malformed = {...access, projectIds} as Access;
    assert.throws(() => createToolCatalog(client, malformed), {code: 'OUT_OF_SCOPE'});
    assert.throws(() => assertProject(malformed, 'project-a'), {code: 'OUT_OF_SCOPE'});
    const status = operationTools(client, malformed).find((tool) => tool.name === 'ao_instructor_status')!;
    const project = operationTools(client, malformed).find((tool) => tool.name === 'ao_project_view')!;
    await assert.rejects(status.execute({}), {code: 'OUT_OF_SCOPE'});
    await assert.rejects(project.execute({projectId: 'project-a'}), {code: 'OUT_OF_SCOPE'});
  }
});

test('cached embedded tools reject malformed scope and identity introduced after discovery', async () => {
  const mutable = {...access};
  const catalog = createToolCatalog(client, mutable);
  mutable.projectIds = 'project-a-other' as unknown as string[];
  for (const [name, input] of [['ao_instructor_status', {}], ['ao_project_view', {projectId: 'project-a'}]] as const) {
    await assert.rejects(catalog.find((tool) => tool.name === name)!.execute(input), {code: 'OUT_OF_SCOPE'});
  }
  mutable.projectIds = ['project-a'];
  mutable.principal = '';
  await assert.rejects(catalog.find((tool) => tool.name === 'ao_runtime_info')!.execute({}), {name: 'ZodError'});
});

test('an admitted invocation retains its validated scope across HTTP awaits and later calls honor revocation', async (t) => {
  const initialProjects = ['project-a'];
  const mutable: Access = {...access, projectIds: initialProjects};
  let requests = 0;
  let pending: ServerResponse | undefined;
  let admitted!: () => void;
  const requestAdmitted = new Promise<void>((resolve) => {admitted = resolve;});
  const body = {id: 'snapshot-current', works: [
    {id: 'work-a', projectId: 'project-a', name: 'initial-allowed-work'},
    {id: 'work-b', projectId: 'project-b', name: 'newly-allowed-work'},
    {id: 'work-foreign', projectId: 'foreign', name: 'foreign-private-marker'},
  ]};
  const daemon = createServer((_request, response) => {
    requests++;
    response.setHeader('Content-Type', 'application/json');
    if (requests === 1) {pending = response; admitted();}
    else response.end(JSON.stringify(body));
  });
  await new Promise<void>((resolve) => daemon.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise<void>((resolve) => {
    if (pending && !pending.writableEnded) pending.end();
    daemon.close(() => resolve()); daemon.closeAllConnections();
  }));
  const localClient = new DaemonClient({url: `http://127.0.0.1:${(daemon.address() as {port: number}).port}`});
  const status = createToolCatalog(localClient, mutable).find((tool) => tool.name === 'ao_instructor_status')!;
  const invocation = status.execute({});
  await requestAdmitted;
  // Exercise both an in-place array mutation and replacement by a malformed
  // substring-matching value while the daemon response is still outstanding.
  initialProjects.push('project-b');
  mutable.projectIds = 'project-a/foreign' as unknown as string[];
  pending!.end(JSON.stringify(body));
  const first = await invocation as {scope: string[]; works: {id: string}[]};
  assert.deepEqual(first.scope, ['project-a']);
  assert.deepEqual(first.works.map((work) => work.id), ['work-a']);
  assert.equal(JSON.stringify(first).includes('foreign-private-marker'), false);
  await assert.rejects(status.execute({}), {code: 'OUT_OF_SCOPE'});
  assert.equal(requests, 1, 'A later malformed host policy must fail before another daemon request.');
  mutable.projectIds = ['project-b'];
  const next = await status.execute({}) as {scope: string[]; works: {id: string}[]};
  assert.deepEqual(next.scope, ['project-b']);
  assert.deepEqual(next.works.map((work) => work.id), ['work-b']);
  assert.equal(requests, 2);
});

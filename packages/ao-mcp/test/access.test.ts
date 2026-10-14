import assert from 'node:assert/strict';
import {test} from 'node:test';
import {requireRole, type Access, type Role} from '../src/core.js';
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

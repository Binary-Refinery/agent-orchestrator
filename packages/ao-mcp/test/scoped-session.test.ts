import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {test, type TestContext} from 'node:test';
import {type Access} from '../src/core.js';
import {DaemonClient, scopedSession, scopedWorkspacePath} from '../src/daemon.js';

const access: Access = {principal: 'developer', role: 'developer', projectIds: ['project-a']};
const member = {kind: 'session', sessionId: 'session-a', instanceId: 'instance-original', association: 'current',
  openTarget: {kind: 'session', projectId: 'project-a', sessionId: 'session-a'}};

async function fixture(t: TestContext, members: unknown[]) {
  const paths: string[] = [];
  const server = createServer((request, response) => {
    const path = new URL(request.url ?? '/', 'http://127.0.0.1').pathname;
    paths.push(path);
    const body = path.endsWith('/members')
      ? {workId: 'work-a', projectId: 'project-a', atomic: false, members}
      : path === '/api/v1/instructor/works/work-a'
        ? {work: {id: 'work-a', projectId: 'project-a', sessionIds: ['session-a']}}
        : {session: {id: 'session-a', projectId: 'project-a', instanceId: 'untrusted-session-field'}};
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify(body));
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise<void>((resolve) => { server.close(() => resolve()); server.closeAllConnections(); }));
  return {paths, client: new DaemonClient({url: `http://127.0.0.1:${(server.address() as {port: number}).port}`})};
}

test('historical display IDs do not authorize unrelated same-project replacement sessions', async (t) => {
  for (const association of ['historical', 'legacy_unknown']) {
    const {client, paths} = await fixture(t, [{...member, association}]);
    await assert.rejects(scopedSession(client, access, 'work-a', 'session-a'), {code: 'OUT_OF_SCOPE'});
    assert(!paths.includes('/api/v1/sessions/session-a'));
  }
});

test('missing and ambiguous current incarnation proof fail before session access', async (t) => {
  for (const members of [[], [{...member, instanceId: ''}], [member, {...member, instanceId: 'another-instance'}]]) {
    const {client, paths} = await fixture(t, members);
    await assert.rejects(scopedSession(client, access, 'work-a', 'session-a'), {code: 'OUT_OF_SCOPE'});
    assert(!paths.includes('/api/v1/sessions/session-a'));
  }
});

test('a current member cannot authorize a foreign project open target', async (t) => {
  const {client, paths} = await fixture(t, [{...member, openTarget: {...member.openTarget, projectId: 'project-b'}}]);
  await assert.rejects(scopedSession(client, access, 'work-a', 'session-a'), {code: 'OUT_OF_SCOPE'});
  assert(!paths.includes('/api/v1/sessions/session-a'));
});

test('workspace fencing carries observed member incarnation and cannot be overridden by query fields', async (t) => {
  const {client} = await fixture(t, [member]);
  const session = await scopedSession(client, access, 'work-a', 'session-a');
  const path = new URL(scopedWorkspacePath('work-a', 'session-a', session, 'file', {
    path: 'src/main.ts', projectId: 'attacker', instanceId: 'replacement', limit: 12,
  }), 'http://127.0.0.1');
  assert.equal(path.pathname, '/api/v1/instructor/works/work-a/sessions/session-a/workspace/file');
  assert.equal(path.searchParams.get('instanceId'), 'instance-original');
  assert.equal(path.searchParams.get('projectId'), 'project-a');
  assert.equal(path.searchParams.get('path'), 'src/main.ts');
  assert.equal(path.searchParams.get('limit'), '12');
});

test('workspace routes cannot be constructed without pinned incarnation proof', () => {
  assert.throws(() => scopedWorkspacePath('work-a', 'session-a', {id: 'session-a', projectId: 'project-a'}, 'file'), {code: 'DAEMON_CONTRACT'});
});

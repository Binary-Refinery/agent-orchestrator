import assert from 'node:assert/strict';
import {spawn, execFile, type ChildProcess} from 'node:child_process';
import {mkdtemp, mkdir, rm, writeFile} from 'node:fs/promises';
import {createServer} from 'node:net';
import {tmpdir} from 'node:os';
import {isAbsolute, join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {promisify} from 'node:util';
import test from 'node:test';
import {Client} from '@modelcontextprotocol/client';
import {InMemoryTransport} from '@modelcontextprotocol/server';
import {createAOMcpServer} from '../src/index.js';
import {record} from '../src/core.js';
import {DaemonClient, DaemonError} from '../src/daemon.js';

const binary = process.env.AO_MCP_TEST_DAEMON;
const run = promisify(execFile);

async function unusedLoopbackPort(): Promise<number> {
  const reservation = createServer();
  await new Promise<void>((resolve, reject) => {
    reservation.once('error', reject);
    reservation.listen(0, '127.0.0.1', resolve);
  });
  const port = (reservation.address() as {port: number}).port;
  await new Promise<void>((resolve, reject) => reservation.close((error) => error ? reject(error) : resolve()));
  return port;
}

async function stopOwnedProcess(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null || child.pid === undefined) return;
  const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()));
  child.kill('SIGTERM');
  let timer: NodeJS.Timeout | undefined;
  try {
    await Promise.race([exited, new Promise<void>((resolve) => {timer = setTimeout(resolve, 5_000);})]);
  } finally {if (timer) clearTimeout(timer);}
  if (child.exitCode === null && child.signalCode === null) {
    child.kill('SIGKILL');
    await exited;
  }
}

// This is an opt-in smoke against a real isolated Linux daemon, not a fixture
// server and not evidence of native-agent, Windows, or provider execution.
test('real isolated AO daemon supports scoped MCP observation and manual intake', {
  skip: binary ? false : 'AO_MCP_TEST_DAEMON not set; live-daemon smoke unavailable',
  timeout: 25_000,
}, async (t) => {
  assert.ok(binary && isAbsolute(binary), 'AO_MCP_TEST_DAEMON must be an absolute binary path');
  assert.equal(process.platform, 'linux', 'This isolated daemon smoke is qualified on Linux.');
  const directory = await mkdtemp(join(tmpdir(), 'ao-mcp-daemon-'));
  let daemon: ChildProcess | undefined;
  let client: Client | undefined;
  let server: ReturnType<typeof createAOMcpServer> | undefined;
  t.after(async () => {
    try { await client?.close(); }
    finally {
      try { await server?.close(); }
      finally {
        try { if (daemon) await stopOwnedProcess(daemon); }
        finally { await rm(directory, {recursive: true, force: true}); }
      }
    }
  });

  // Deliberately do not inherit AO state variables or account credentials.
  // AO_DATA_DIR isolates all AO-owned state without changing the user's HOME.
  // The daemon requires the real HOME for read-only native-home resolution.
  // Keep its value unchanged; fresh AO state has no native accounts to load.
  const env: NodeJS.ProcessEnv = {PATH: process.env.PATH, HOME: process.env.HOME, LANG: 'C.UTF-8',
    GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: '/dev/null'};
  const makeRepository = async (name: string): Promise<string> => {
    const path = join(directory, name);
    await mkdir(path);
    await run('git', ['-c', 'init.templateDir=', 'init', '--initial-branch=main', path], {env});
    await writeFile(join(path, 'README.md'), 'Isolated AO MCP smoke repository.\n');
    await run('git', ['-C', path, 'add', 'README.md'], {env});
    await run('git', ['-C', path, '-c', 'core.hooksPath=/dev/null', '-c', 'user.name=AO MCP Smoke',
      '-c', 'user.email=ao-mcp-smoke@example.invalid', 'commit', '-m', 'test: isolated smoke fixture'], {env});
    return path;
  };
  const repository = await makeRepository('project');
  const foreignRepository = await makeRepository('foreign-project');
  const port = await unusedLoopbackPort();
  const origin = `http://127.0.0.1:${port}`;
  let daemonLog = '';
  let spawnError: Error | undefined;
  daemon = spawn(binary, ['daemon'], {
    cwd: directory,
    env: {...env, AO_DATA_DIR: join(directory, 'data'), AO_RUN_FILE: join(directory, 'running.json'),
      AO_PORT: String(port), AO_INSTRUCTOR: '1', AO_SHUTDOWN_TIMEOUT: '2s',
      AO_TELEMETRY_EVENTS: 'off', AO_TELEMETRY_METRICS: 'off', AO_TELEMETRY_REMOTE: 'off', AO_CLOUD_OFFERING: 'off'},
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  const appendLog = (chunk: Buffer) => {daemonLog = (daemonLog + chunk.toString('utf8')).slice(-64 * 1024);};
  daemon.stdout?.on('data', appendLog);
  daemon.stderr?.on('data', appendLog);
  daemon.once('error', (error) => {spawnError = error;});
  const deadline = Date.now() + 10_000;
  let ready = false;
  while (Date.now() < deadline) {
    assert.equal(spawnError, undefined, `Cannot spawn daemon: ${spawnError?.message}`);
    assert.equal(daemon.exitCode, null, `Daemon exited during startup: ${daemonLog}`);
    try {
      const response = await fetch(`${origin}/readyz`, {signal: AbortSignal.timeout(300)});
      if (response.ok) {
        const identity = record(await response.json());
        assert.equal(identity.pid, daemon.pid, 'The selected port must belong to the owned test daemon.');
        assert.equal(identity.status, 'ready');
        ready = true;
        break;
      }
    } catch (error) {
      if (error instanceof assert.AssertionError) throw error;
    }
    await delay(50);
  }
  assert.ok(ready, `Isolated daemon did not become ready: ${daemonLog}`);

  const api = async (path: string, method = 'GET', body?: unknown): Promise<Record<string, unknown>> => {
    const response = await fetch(`${origin}${path}`, {method, headers: {'Content-Type': 'application/json'},
      ...(body === undefined ? {} : {body: JSON.stringify(body)}), signal: AbortSignal.timeout(5_000)});
    const data = record(await response.json());
    assert.ok(response.ok, `${method} ${path} failed (${response.status}): ${JSON.stringify(data)}`);
    return data;
  };

  // Fixture setup preserves the daemon's locked workflow, independent review,
  // human gates and evidence requirements. Explicit legacy role constraints let
  // us exercise registration without native-model discovery or any account.
  // This never authorizes or attempts a worker/orchestrator launch.
  const policy = await api('/api/v1/instructor/policy');
  const roles = record(policy.roles);
  for (const [name, harness] of [['worker', 'opencode'], ['reviewer', 'codex'], ['orchestrator', 'codex']]) {
    const role = record(roles[name!]);
    delete role.selectionSource;
    role.harness = harness;
  }
  await api('/api/v1/instructor/policy', 'PUT', policy);
  const projectResponse = await api('/api/v1/projects', 'POST', {path: repository, projectId: 'mcp-smoke', name: 'MCP smoke'});
  const project = record(projectResponse.project);
  assert.equal(project.id, 'mcp-smoke');
  await api('/api/v1/projects', 'POST', {path: foreignRepository, projectId: 'mcp-foreign', name: 'Foreign private smoke'});
  await api('/api/v1/instructor/works', 'POST', {projectId: 'mcp-foreign', name: 'Foreign private work',
    goal: 'foreign-private-goal-marker', source: 'manual', sourceRef: 'foreign-smoke-origin'});

  server = createAOMcpServer({daemonUrl: origin, transport: 'stdio',
    access: {principal: 'real-daemon-smoke', role: 'operator', projectIds: ['mcp-smoke']}}, () => {});
  client = new Client({name: 'isolated-real-daemon-smoke', version: '1.0'});
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  await server.connect(serverTransport);
  await client.connect(clientTransport);
  const call = async (name: string, args: Record<string, unknown> = {}): Promise<Record<string, unknown>> => {
    const response = await client!.callTool({name, arguments: args});
    assert.notEqual(response.isError, true, `${name} failed: ${JSON.stringify(response)}`);
    return record(record(response.structuredContent).result);
  };

  const runtime = await call('ao_runtime_info');
  assert.equal(record(runtime.health).pid, daemon.pid);
  assert.equal(record(runtime.health).status, 'ok');
  assert.equal(record(runtime.readiness).status, 'ready');
  assert.equal(record(runtime.health).executablePath, binary);
  assert.equal(record(runtime.health).workingDirectory, join(directory, 'data'));
  const view = await call('ao_project_view', {projectId: 'mcp-smoke'});
  assert.equal(record(view.project).id, 'mcp-smoke');
  assert.equal(record(view.project).path, repository);
  assert.equal(Object.hasOwn(record(view.project), 'config'), false);
  const initial = await call('ao_instructor_status', {projectId: 'mcp-smoke'});
  assert.ok(typeof initial.id === 'string');
  assert.deepEqual(initial.works, []);
  assert.equal(JSON.stringify(initial).includes('foreign-private'), false);

  const intake = {projectId: 'mcp-smoke', name: 'Real MCP smoke work', goal: 'Exercise real API intake without agent execution.',
    sourceRef: 'real-smoke-origin', snapshotId: initial.id, idempotencyKey: 'real-smoke-intent'};
  const admitted = await call('ao_create_work', intake);
  assert.equal(admitted.execution, 'unknown');
  assert.equal(admitted.intakeDeduplication, 'project/source/sourceRef');
  const admittedWorks = record(admitted.snapshot).works;
  assert.ok(Array.isArray(admittedWorks));
  assert.equal(admittedWorks.length, 1);
  const work = record(admittedWorks[0]);
  assert.equal(work.projectId, 'mcp-smoke');
  assert.equal(work.sourceRef, 'real-smoke-origin');
  assert.ok(typeof work.id === 'string');
  assert.equal(JSON.stringify(admitted).includes('foreign-private'), false);
  const repeated = await call('ao_create_work', intake);
  const repeatedWorks = record(repeated.snapshot).works;
  assert.ok(Array.isArray(repeatedWorks));
  assert.equal(repeatedWorks.length, 1);
  assert.equal(record(repeatedWorks[0]).id, work.id, 'Stable origin must return the same durable work.');

  const trace = await call('ao_work_trace', {workId: work.id});
  assert.equal(record(trace.work).id, work.id);
  assert.ok(Array.isArray(trace.events) && trace.events.some((event) => record(event).kind === 'registered'));
  assert.deepEqual(trace.dispatches, []);
  assert.deepEqual(record(trace.work).sessionIds, []);
  assert.equal(JSON.stringify(trace).includes('publicationGrants'), false);
  const direct = new DaemonClient({url: origin});
  const fenced = `/api/v1/instructor/works/${work.id}/sessions/missing-session/workspace`;
  await assert.rejects(direct.get(`${fenced}/file?path=main.ts`), (error: unknown) =>
    error instanceof DaemonError && error.status === 400 && error.code === 'INSTRUCTOR_WORKSPACE_SCOPE_REQUIRED');
  const scope = '?projectId=mcp-smoke&instanceId=missing-instance';
  for (const request of [
    () => direct.get(`${fenced}/tree${scope}`),
    () => direct.get(`${fenced}/search${scope}&query=main`),
    () => direct.get(`${fenced}/file${scope}&path=main.ts`),
    () => direct.post(`${fenced}/diffs${scope}`, {paths: ['main.ts']}),
    () => direct.put(`${fenced}/file${scope}`, {path: 'main.ts', content: 'must-not-write', expectedFileFingerprint: 'missing'}),
  ]) {
    await assert.rejects(request(), (error: unknown) => error instanceof DaemonError
      && error.status === 409 && error.code === 'INSTRUCTOR_WORKSPACE_SCOPE_CHANGED');
  }
  const forbidden = await client.callTool({name: 'ao_work_command', arguments: {
    workId: work.id, command: 'go', snapshotId: initial.id, idempotencyKey: 'forbidden-human-gate',
  }});
  assert.equal(forbidden.isError, true);
  assert.equal(record(record(forbidden.structuredContent).error).code, 'COMMAND_NOT_ALLOWED');
  const final = await call('ao_instructor_status', {projectId: 'mcp-smoke'});
  assert.ok(Array.isArray(final.works));
  assert.equal(final.works.length, 1);
  assert.equal(JSON.stringify(final).includes('foreign-private'), false);
  const sessions = await api('/api/v1/sessions');
  assert.deepEqual(sessions.sessions, [], 'Manual intake and rejected gate commands must not launch native sessions.');
  t.diagnostic(`Real Linux AO API smoke passed: owned daemon PID ${daemon.pid}, port ${port}; no native agents launched.`);
});

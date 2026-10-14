import * as z from 'zod/v4';
import {AdapterError, assertProject, defineTool, identifier, record, requireRole, type Access, type ToolDefinition} from './core.js';
import {DaemonClient, DaemonError, scopedWork} from './daemon.js';

// Every nested object is projected independently. In particular, pinned policy,
// grants, arbitrary configuration, and portfolio-wide metrics never cross MCP.
function scalars(value: unknown, fields: readonly string[]): Record<string, unknown> {
  const source = record(value);
  const output: Record<string, unknown> = {};
  for (const field of fields) {
    const item = source[field];
    if (typeof item === 'string' || typeof item === 'boolean' || typeof item === 'number' || item === null) output[field] = item;
  }
  return output;
}

function objects(value: unknown): Record<string, unknown>[] {
  if (value === undefined || value === null) return [];
  if (!Array.isArray(value)) throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned an unexpected collection shape.');
  return value.map(record);
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === 'string') : [];
}

const selectionFields = ['harness', 'model', 'effort'];
const receiptFields = ['runtimeId', 'observerRuntimeId', 'providerTurnId', 'deliveredAt', 'acceptedAt', 'startedAt', 'source'];
const workFields = ['id', 'projectId', 'name', 'goal', 'source', 'sourceRef', 'issueId', 'state', 'phase', 'phaseStartedAt',
  'phaseSequence', 'fixPhaseSequence', 'deltaPhaseSequence', 'summary', 'next', 'branch', 'sha', 'policyVersion',
  'fixRounds', 'openCriticalFindings', 'waitCondition', 'waitUntil', 'nudgedAt', 'revision', 'createdAt', 'updatedAt', 'progressAt'];
const dispatchFields = ['id', 'batchId', 'workId', 'projectId', 'targetSessionId', 'targetSessionInstanceId', 'targetCreatedAt',
  'targetRuntimeId', 'reportId', 'producerSessionId', 'producerRuntimeId', 'phase', 'phaseSequence', 'policyVersion',
  'snapshotId', 'workRevision', 'position', 'action', 'message', 'state', 'error', 'attempts', 'queuedAt', 'createdAt', 'updatedAt'];
const allowedActions = new Set(['weiter', 'stopp', 'abbrechen']);

function projectWork(value: unknown): Record<string, unknown> {
  const source = record(value);
  const output = scalars(source, workFields);
  output.sessionIds = strings(source.sessionIds);
  output.actions = strings(source.actions);
  output.uncertainDispatchIds = strings(source.uncertainDispatchIds);
  if (source.gate) output.gate = scalars(source.gate, ['id', 'kind', 'title', 'openedAt', 'transitionTo', 'dispatchId', 'targetSessionId']);
  if (source.suspendedGate) output.suspendedGate = scalars(source.suspendedGate, ['id', 'kind', 'title', 'openedAt', 'transitionTo']);
  if (source.heartbeat && record(source.heartbeat).workId === source.id) {
    output.heartbeat = scalars(source.heartbeat, ['workId', 'sessionId', 'role', 'at', 'summary']);
  }
  if (source.verification) output.verification = scalars(source.verification, ['inspected', 'passes', 'maxPasses', 'status', 'error', 'claims', 'verified', 'lastRunAt', 'nextRunAt']);
  output.evidence = objects(source.evidence).filter((item) => item.workId === source.id).map((item) => scalars(item,
    ['id', 'workId', 'policyVersion', 'sha', 'kind', 'phase', 'phaseSequence', 'profile', 'producer', 'role',
      'harness', 'model', 'effort', 'ref', 'verified', 'revoked', 'revocationReason', 'outcome', 'criticalFindings', 'createdAt']));
  output.launches = objects(source.launches).map((item) => {
    const launch = scalars(item, ['sessionId', 'sessionInstanceId', 'ownerSessionId', 'runtimeGeneration', 'reviewCreatedAt',
      'reviewId', 'reviewRunId', 'sha', 'role', 'phase', 'phaseSequence', 'status', 'launchedAt']);
    for (const field of ['requested', 'sent', 'confirmed']) if (item[field]) launch[field] = scalars(item[field], selectionFields);
    return launch;
  });
  return output;
}

function projectDispatch(value: unknown): Record<string, unknown> {
  const source = record(value);
  const output = scalars(source, dispatchFields);
  if (source.nativeReceipt) output.nativeReceipt = scalars(source.nativeReceipt, receiptFields);
  return output;
}

function projectDetails(value: unknown, workId: string, projectId: string): Record<string, unknown> {
  const source = record(value);
  const work = record(source.work);
  if (work.id !== workId || work.projectId !== projectId) throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned different work details.');
  const output: Record<string, unknown> = {
    work: projectWork(work),
    events: objects(source.events).filter((event) => event.workId === workId).map((event) => scalars(event, ['id', 'workId', 'kind', 'detail', 'at'])),
    dispatches: objects(source.dispatches).filter((item) => item.workId === workId && item.projectId === projectId).map(projectDispatch),
  };
  if (source.budgetUsage) output.budgetUsage = scalars(source.budgetUsage, ['concurrentSessions', 'sessions', 'turns', 'delegations']);
  return output;
}

function projectMember(value: unknown, projectId: string): Record<string, unknown> {
  const source = record(value);
  const output = scalars(source, ['id', 'kind', 'role', 'association', 'status', 'sessionId', 'reviewId', 'reviewRunId',
    'instanceId', 'runtimeGeneration', 'name', 'phase', 'createdAt', 'reason']);
  for (const field of ['requested', 'sent', 'confirmed']) if (source[field]) output[field] = scalars(source[field], selectionFields);
  if (source.openTarget && record(source.openTarget).projectId === projectId) {
    output.openTarget = scalars(source.openTarget, ['kind', 'projectId', 'sessionId', 'reviewId', 'reviewRunId']);
  }
  return output;
}

function projectMembers(value: unknown, workId: string, projectId: string): Record<string, unknown> {
  const source = record(value);
  if (source.workId !== workId || source.projectId !== projectId) throw new AdapterError('OUT_OF_SCOPE', 'Member observations belong to a different work or project.');
  const output = scalars(source, ['workId', 'projectId', 'workRevision', 'capturedAt', 'atomic']);
  output.members = objects(source.members).map((member) => projectMember(member, projectId));
  if (source.coordinator) {
    const coordinator = record(source.coordinator);
    const projected = scalars(coordinator, ['key']);
    if (coordinator.current) projected.current = projectMember(coordinator.current, projectId);
    output.coordinator = projected;
  }
  return output;
}

function projectRecovery(value: unknown, workId: string): Record<string, unknown> {
  const source = record(value);
  if (source.workId !== workId) throw new AdapterError('OUT_OF_SCOPE', 'Recovery observation belongs to a different work.');
  return scalars(source, ['workId', 'workRevision', 'policyVersion', 'sessionId', 'sessionCreatedAt', 'providerConversationId',
    'controllerGeneration', 'ready', 'action', 'reasonCode', 'reason']);
}

function observationFailure(error: unknown): Record<string, unknown> {
  if (error instanceof DaemonError) return {available: false, code: error.code, message: error.message, status: error.status, ...(error.requestId ? {requestId: error.requestId} : {})};
  if (error instanceof AdapterError) return {available: false, code: error.code, message: error.message};
  throw error;
}

function normalize(value: string): string { return value.trim().toLocaleLowerCase('en-US'); }

async function scopedCommandResponse<T>(request: () => Promise<T>): Promise<T> {
  try { return await request(); }
  catch (error) {
    // Command admission can resolve a work against an older portfolio snapshot.
    // Its validation message may enumerate every captured work, including work
    // outside this adapter's scope. Preserve diagnostics, never that raw text.
    if (error instanceof DaemonError) {
      throw new DaemonError(error.code, 'The scoped Instructor command failed. Inspect the requested work and snapshot before retrying.', error.status, error.requestId);
    }
    throw error;
  }
}

async function resolveAction(client: DaemonClient, command: string, work?: Record<string, unknown>): Promise<string> {
  const response = record(await scopedCommandResponse(() => client.get('/api/v1/instructor/commands')));
  const registry = objects(response.commands);
  // The daemon re-resolves a selected work against its pinned policy registry.
  if (work?.policy) registry.push(...objects(record(work.policy).commands));
  const key = normalize(command);
  const matches = registry.filter((entry) => [entry.name, entry.action, ...strings(entry.aliases)]
    .some((spelling) => typeof spelling === 'string' && normalize(spelling) === key));
  const actions = new Set(matches.map((entry) => entry.action));
  if (actions.size !== 1 || typeof matches[0]?.action !== 'string') {
    throw new AdapterError('COMMAND_NOT_ALLOWED', 'Use one exact, unambiguous registered command name or alias.');
  }
  return matches[0].action;
}

function projectResult(value: unknown, projectId: string, select: (work: Record<string, unknown>) => boolean, exactWorkId?: string): Record<string, unknown> {
  const source = record(value);
  const output = scalars(source, ['batchId']);
  const selectedIds = new Set<string>(exactWorkId ? [exactWorkId] : []);
  if (source.snapshot) {
    const snapshot = record(source.snapshot);
    const works = objects(snapshot.works).filter((work) => work.projectId === projectId && select(work));
    for (const work of works) if (typeof work.id === 'string') selectedIds.add(work.id);
    output.snapshot = {...scalars(snapshot, ['id', 'baseId', 'createdAt']), works: works.map(projectWork)};
  }
  const details = objects(source.details).filter((detail) => {
    const work = record(detail.work);
    return work.projectId === projectId && select(work);
  });
  for (const detail of details) {
    const work = record(detail.work);
    if (typeof work.id === 'string') selectedIds.add(work.id);
  }
  output.details = details.map((detail) => projectDetails(detail, record(detail.work).id as string, projectId));
  output.dispatches = objects(source.dispatches).filter((dispatch) => dispatch.projectId === projectId && typeof dispatch.workId === 'string' && selectedIds.has(dispatch.workId)).map(projectDispatch);
  output.skipped = strings(source.skipped).filter((item) => [...selectedIds].some((id) => item.startsWith(`${id}:`)));
  // An HTTP command response proves admission only; provider receipts are
  // separately preserved above and a missing execution receipt stays unknown.
  output.execution = 'unknown';
  return output;
}

export function operationTools(client: DaemonClient, access: Access): ToolDefinition[] {
  return [
    defineTool('ao_runtime_info', 'Read real daemon health/readiness identity. Missing version remains unknown; this does not prove an agent executed.', 'observer', true, z.strictObject({}), async () => {
      const health = record(await client.get('/healthz'));
      let readiness: Record<string, unknown>;
      try { readiness = scalars(await client.get('/readyz'), ['status', 'service', 'pid', 'version', 'executablePath', 'workingDirectory', 'startupWorkingDirectory', 'appImagePath']); }
      catch (error) { readiness = observationFailure(error); }
      return {
        health: scalars(health, ['status', 'service', 'pid', 'version', 'executablePath', 'workingDirectory', 'startupWorkingDirectory', 'appImagePath']),
        readiness,
        version: typeof health.version === 'string' ? health.version : typeof readiness.version === 'string' ? readiness.version : 'unknown',
      };
    }),
    defineTool('ao_project_view', 'Read project identity and launch health without configuration or credentials.', 'observer', true,
      z.strictObject({projectId: identifier}), async ({projectId}) => {
        assertProject(access, projectId);
        const response = record(await client.get(`/api/v1/projects/${encodeURIComponent(projectId)}`));
        const project = record(response.project);
        if (project.id !== projectId) throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned a different project.');
        return {project: scalars(project, ['id', 'name', 'kind', 'path', 'defaultBranch', 'agent', 'folderMissing', 'resolveError'])};
      }),
    defineTool('ao_instructor_status', 'Capture a project-scoped Instructor snapshot. Portfolio metrics, recoveries, policy and grants are omitted.', 'observer', true,
      z.strictObject({projectId: identifier.optional(), baseId: identifier.optional()}), async ({projectId, baseId}) => {
        if (projectId) assertProject(access, projectId);
        const response = record(await client.get(`/api/v1/instructor/status${baseId ? `?baseId=${encodeURIComponent(baseId)}` : ''}`));
        const works = objects(response.works).filter((work) => typeof work.projectId === 'string' && access.projectIds.includes(work.projectId) && (!projectId || work.projectId === projectId));
        // A portfolio registry can include private project-specific aliases.
        // Resolve commands internally for the exact selected work instead.
        return {...scalars(response, ['id', 'baseId', 'createdAt']), scope: projectId ? [projectId] : [...access.projectIds], works: works.map(projectWork)};
      }),
    defineTool('ao_work_trace', 'Read exact-work events, dispatch receipts, members and controller recovery observations. Acceptance is distinct from execution.', 'observer', true,
      z.strictObject({workId: identifier, includeMembers: z.boolean().default(true), includeRecovery: z.boolean().default(true)}), async ({workId, includeMembers, includeRecovery}) => {
        const details = await scopedWork(client, access, workId);
        const projectId = record(details.work).projectId as string;
        const result = projectDetails(details, workId, projectId);
        if (includeMembers) {
          try { result.members = {available: true, observation: projectMembers(await client.get(`/api/v1/instructor/works/${encodeURIComponent(workId)}/members`), workId, projectId)}; }
          catch (error) { result.members = observationFailure(error); }
        }
        if (includeRecovery) {
          try { result.recovery = {available: true, observation: projectRecovery(await client.get(`/api/v1/instructor/works/${encodeURIComponent(workId)}/recovery`), workId)}; }
          catch (error) { result.recovery = observationFailure(error); }
        }
        return result;
      }),
    defineTool('ao_create_work', 'Register manual work in one configured project against an explicit snapshot. Keep the complete request, key and stable sourceRef on retry; the daemon deduplicates intake by project/source/sourceRef, not the command key. Does not start an agent.', 'operator', false,
      z.strictObject({projectId: identifier, name: z.string().trim().min(1).max(120), goal: z.string().trim().min(1).max(8192), sourceRef: z.string().min(1).max(180), snapshotId: identifier, idempotencyKey: identifier}),
      async ({projectId, name, goal, sourceRef, snapshotId, idempotencyKey}) => {
        requireRole(access, 'operator');
        assertProject(access, projectId);
        const action = await resolveAction(client, 'neu');
        if (action !== 'neu') throw new AdapterError('COMMAND_NOT_ALLOWED', 'The registered intake command has an unexpected action.');
        const result = await scopedCommandResponse(() => client.post('/api/v1/instructor/commands', {command: action, snapshotId, idempotencyKey, new: {projectId, name, goal, source: 'manual', sourceRef}}));
        return {...projectResult(result, projectId, (work) => work.source === 'manual' && work.sourceRef === sourceRef), intakeDeduplication: 'project/source/sourceRef'};
      }),
    defineTool('ao_work_command', 'Continue, pause or cancel exactly one authorized work using an exact registered name or alias. Requires the original captured snapshot and stable key; never grants approvals or publication authority. HTTP acceptance does not prove execution.', 'operator', false,
      z.strictObject({workId: identifier, command: z.string().trim().min(1).max(80), snapshotId: identifier, idempotencyKey: identifier}),
      async ({workId, command, snapshotId, idempotencyKey}) => {
        requireRole(access, 'operator');
        const details = await scopedWork(client, access, workId);
        const work = record(details.work);
        const action = await resolveAction(client, command, work);
        if (!allowedActions.has(action)) throw new AdapterError('COMMAND_NOT_ALLOWED', 'Only continue, pause and cancel actions are exposed by this adapter.');
        const result = await scopedCommandResponse(() => client.post('/api/v1/instructor/commands', {command: action, snapshotId, workIds: [workId], idempotencyKey}));
        return projectResult(result, work.projectId as string, (item) => item.id === workId, workId);
      }, true),
  ];
}

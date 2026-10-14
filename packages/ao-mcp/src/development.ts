import {randomUUID} from 'node:crypto';
import * as z from 'zod/v4';
import {AdapterError, assertProject, defineTool, identifier, record, requireRole, type Access, type ToolDefinition} from './core.js';
import {DaemonClient, DaemonError, scopedSession, scopedWork, scopedWorkspacePath} from './daemon.js';

// The daemon remains responsible for symlink confinement, ignored files and
// optimistic updates. These paths deliberately work identically on Windows/Linux.
const relativePath = z.string().min(1).max(4096).refine((value) =>
  !value.startsWith('/') && !/[\\\x00-\x1f:]/u.test(value)
  && value.split('/').every((part) => part !== '' && part !== '.' && part !== '..'),
'Use a workspace-relative path with forward slashes and no traversal.');
const fingerprint = z.string().trim().min(1).max(512);
const scope = z.enum(['combined', 'committed', 'staged', 'unstaged', 'untracked']);
const sessionInput = {workId: identifier, sessionId: identifier};
// Policy keys are opaque strings, not resource IDs. Match the daemon's reserved
// evidence names while preserving spaces, slashes, Unicode and exact spelling.
const reservedProfileNames = new Set(['task-packet', 'ci', 'ci-failed', 'review-report', 'triage', 'delta', 'delta-changes', 'attribution', 'traceability']);
const profileName = z.string().min(1).max(512).refine((name) => !reservedProfileNames.has(name), 'Business evidence names cannot be check profiles.');
const fileFields = ['sessionId', 'path', 'previousPath', 'status', 'additions', 'deletions', 'size', 'binary', 'deleted', 'editable', 'content', 'contentTruncated', 'diff', 'diffTruncated', 'compareBaseSha', 'compareBaseRef', 'compareMode', 'workspaceVersion', 'fileFingerprint'];
// The existing checks endpoint uses the daemon's ordinary 60-second REST
// deadline. A background MCP handle does not make that HTTP operation durable.
const maxCheckSeconds = 45;
const checkTimeoutMarginSeconds = 10;

export interface CheckJob {
  jobId: string;
  operationId: string;
  workId: string;
  sessionId: string;
  profiles: string[];
  state: 'running' | 'completed' | 'failed' | 'uncertain';
  startedAt: string;
  finishedAt?: string;
  result?: unknown;
  error?: {code: string; message: string; status?: number; requestId?: string};
}

interface OwnedCheckJob extends CheckJob {principal: string}
interface WorkspaceEdit {workId: string; sessionId: string; state: 'running' | 'uncertain'}

function definiteWorkspaceRejection(error: unknown): boolean {
  // These exact endpoint contracts reject before file replacement. A generic
  // 400/404 may instead come from the refreshed read after a completed write.
  return error instanceof DaemonError && (
    error.status === 409 && ['WORKSPACE_FILE_STALE', 'INSTRUCTOR_WORKSPACE_SCOPE_CHANGED', 'INSTRUCTOR_WORKSPACE_BUSY'].includes(error.code)
    || error.status === 400 && error.code === 'INSTRUCTOR_WORKSPACE_SCOPE_REQUIRED'
  );
}

/** Process-local handles; AO's durable work/evidence remains the source of truth. */
export class CheckJobs {
  private readonly jobs = new Map<string, OwnedCheckJob>();
  private readonly edits = new Map<string, WorkspaceEdit>();

  constructor(private readonly limit = 64, private readonly concurrentLimit = 4) {}

  get(access: Access, jobId: string): CheckJob {
    const job = this.jobs.get(jobId);
    if (!job || job.principal !== access.principal) {
      throw new AdapterError('JOB_NOT_FOUND', 'No check job is available for this principal.');
    }
    const {principal: _principal, ...visible} = job;
    return {...visible, profiles: [...visible.profiles]};
  }

  assertIdle(sessionId: string, workId?: string): void {
    if ([...this.jobs.values()].some((job) => (job.sessionId === sessionId || workId !== undefined && job.workId === workId) && (job.state === 'running' || job.state === 'uncertain'))
      || [...this.edits.values()].some((edit) => edit.sessionId === sessionId || workId !== undefined && edit.workId === workId)) {
      throw new AdapterError('CHECKS_BUSY', 'An edit or checks are running, or their outcome is uncertain, for this work/session. Inspect AO before further changes.');
    }
  }

  private assertCapacity(): void {
    if ([...this.jobs.values()].filter((job) => job.state === 'running' || job.state === 'uncertain').length + this.edits.size >= this.concurrentLimit) {
      throw new AdapterError('JOB_LIMIT', 'The adapter concurrent edit/check capacity is exhausted.');
    }
  }

  reserveEdit(workId: string, sessionId: string): (error?: unknown) => void {
    this.assertIdle(sessionId, workId);
    this.assertCapacity();
    const id = randomUUID();
    const edit: WorkspaceEdit = {workId, sessionId, state: 'running'};
    this.edits.set(id, edit);
    return (error?: unknown) => {
      if (error === undefined || definiteWorkspaceRejection(error)) this.edits.delete(id);
      else edit.state = 'uncertain';
    };
  }

  start(access: Access, input: {workId: string; sessionId: string; profiles: string[]; operationId: string}, run: () => Promise<unknown>): CheckJob {
    const previous = [...this.jobs.values()].find((job) => job.principal === access.principal && job.operationId === input.operationId);
    if (previous) {
      if (previous.workId !== input.workId || previous.sessionId !== input.sessionId || JSON.stringify(previous.profiles) !== JSON.stringify(input.profiles)) {
        throw new AdapterError('IDEMPOTENCY_CONFLICT', 'This operationId already identifies different check arguments.');
      }
      return this.get(access, previous.jobId);
    }
    this.assertIdle(input.sessionId, input.workId);
    // Do not evict completed operation IDs: doing so could replay an old write.
    if (this.jobs.size >= this.limit) throw new AdapterError('JOB_LIMIT', 'The adapter check-job capacity is exhausted.');
    this.assertCapacity();
    const job: OwnedCheckJob = {
      ...input, profiles: [...input.profiles], principal: access.principal,
      jobId: randomUUID(), state: 'running', startedAt: new Date().toISOString(),
    };
    this.jobs.set(job.jobId, job);
    void Promise.resolve().then(run).then((result) => {
      job.result = result;
      job.state = 'completed';
      job.finishedAt = new Date().toISOString();
    }, (error: unknown) => {
      // Only this exact daemon preflight contract certifies no admission or
      // effects. Other 400/409 responses can follow already executed profiles.
      job.state = error instanceof DaemonError && error.status === 400 && error.code === 'INSTRUCTOR_CHECKS_DEADLINE_PREEXECUTION' ? 'failed' : 'uncertain';
      job.error = error instanceof AdapterError
        ? {code: error.code, message: error.message, ...(error instanceof DaemonError ? {status: error.status, requestId: error.requestId} : {})}
        : {code: 'CHECK_JOB_FAILED', message: 'The check request failed; inspect the AO work for authoritative evidence.'};
      job.finishedAt = new Date().toISOString();
    });
    return this.get(access, job.jobId);
  }
}

// HTTP creates clients per request, so key by origin rather than client instance.
// Different daemon authorities must never share job results or admission locks.
const daemonCheckJobs = new Map<string, CheckJobs>();

function jobsFor(client: DaemonClient): CheckJobs {
  const key = client.url.origin;
  let jobs = daemonCheckJobs.get(key);
  if (!jobs) { jobs = new CheckJobs(); daemonCheckJobs.set(key, jobs); }
  return jobs;
}

function workspaceResult(value: unknown, sessionId: string, fields: readonly string[], path?: string): Record<string, unknown> {
  const response = record(value);
  if (response.sessionId !== sessionId || (path !== undefined && response.path !== path)) {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned a different workspace resource.');
  }
  const result = scalars(response, fields);
  if (fields.includes('entries')) result.entries = collection(response.entries).map((entry) => scalars(entry, ['name', 'path', 'type', 'status', 'hasChanges', 'size', 'binary']));
  if (fields.includes('results')) result.results = collection(response.results).map((entry) => scalars(entry, ['path', 'status', 'size', 'binary', 'fileFingerprint']));
  if (fields.includes('groups')) result.groups = collection(response.groups).map((group) => ({
    ...scalars(group, ['repository', 'patch', 'truncated']),
    includedPaths: Array.isArray(group.includedPaths) ? group.includedPaths.filter((entry): entry is string => typeof entry === 'string') : [],
    deferred: collection(group.deferred).map((entry) => scalars(entry, ['path', 'reason'])),
    errors: collection(group.errors).map((entry) => scalars(entry, ['code', 'message'])),
  }));
  return result;
}

function scalars(value: Record<string, unknown>, fields: readonly string[]): Record<string, unknown> {
  return Object.fromEntries(fields.filter((field) => typeof value[field] === 'string' || typeof value[field] === 'number' || typeof value[field] === 'boolean' || value[field] === null).map((field) => [field, value[field]]));
}

function collection(value: unknown): Record<string, unknown>[] {
  if (!Array.isArray(value)) throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned an unexpected workspace collection.');
  return value.map(record);
}

function checkResult(value: unknown, access: Access, workId: string): Record<string, unknown> {
  const work = record(value);
  assertProject(access, work.projectId);
  if (work.id !== workId) throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned different check work.');
  return {
    ...scalars(work, ['id', 'projectId', 'state', 'phase', 'phaseSequence', 'revision', 'sha', 'policyVersion', 'summary']),
    evidence: collection(work.evidence ?? []).filter((entry) => entry.workId === workId).map((entry) => scalars(entry,
      ['id', 'workId', 'policyVersion', 'sha', 'kind', 'phase', 'phaseSequence', 'profile', 'producer', 'role', 'harness', 'model', 'effort', 'ref', 'verified', 'revoked', 'revocationReason', 'outcome', 'criticalFindings', 'createdAt'])),
  };
}

function approvedCheckTimeout(details: Record<string, unknown>, profiles: readonly string[]): number {
  const approved = record(record(record(details.work).policy).profiles);
  let seconds = 0;
  for (const name of profiles) {
    const candidate = Object.hasOwn(approved, name) ? approved[name] : undefined;
    if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) {
      throw new AdapterError('PROFILE_NOT_APPROVED', 'A requested profile is not approved in the work policy.');
    }
    const profile = record(candidate);
    if (!Array.isArray(profile.command) || profile.command.length === 0
      || !profile.command.every((part) => typeof part === 'string')
      || typeof profile.timeoutSeconds !== 'number' || !Number.isInteger(profile.timeoutSeconds) || profile.timeoutSeconds <= 0) {
      throw new AdapterError('PROFILE_NOT_APPROVED', 'A requested profile has no approved command or bounded timeout.');
    }
    seconds += profile.timeoutSeconds;
  }
  if (seconds > maxCheckSeconds) throw new AdapterError('CHECK_DEADLINE_LIMIT', 'The existing AO REST deadline supports at most 45 seconds of combined configured profile timeouts. Select a smaller approved batch; longer checks require a durable asynchronous daemon API.');
  return (seconds + checkTimeoutMarginSeconds) * 1000;
}

export function developmentTools(client: DaemonClient, access: Access, jobs: CheckJobs = jobsFor(client)): ToolDefinition[] {
  const authorize = async (workId: string, sessionId: string): Promise<Record<string, unknown>> => {
    requireRole(access, 'developer');
    return scopedSession(client, access, workId, sessionId);
  };
  return [
    defineTool('ao_workspace_tree', 'List one level of an attached session workspace. Source access requires developer scope.', 'developer', true,
      z.strictObject({...sessionInput, path: relativePath.optional()}), async (input) => {
        const session = await authorize(input.workId, input.sessionId);
        return workspaceResult(await client.get(scopedWorkspacePath(input.workId, input.sessionId, session, 'tree', {path: input.path})), input.sessionId, ['sessionId', 'path', 'entries', 'truncated']);
      }),
    defineTool('ao_workspace_search', 'Find workspace paths by substring, with bounded pagination. This is a path search, not a content search.', 'developer', true,
      z.strictObject({...sessionInput, query: z.string().trim().min(1).max(512), cursor: z.string().max(4096).optional(), limit: z.number().int().min(1).max(100).default(50)}), async (input) => {
        const session = await authorize(input.workId, input.sessionId);
        return workspaceResult(await client.get(scopedWorkspacePath(input.workId, input.sessionId, session, 'search', {query: input.query, cursor: input.cursor, limit: input.limit})), input.sessionId, ['sessionId', 'query', 'results', 'nextCursor', 'truncated']);
      }),
    defineTool('ao_workspace_read', 'Read a bounded text file and its fingerprint from an attached session workspace. Check contentTruncated before editing.', 'developer', true,
      z.strictObject({...sessionInput, path: relativePath, section: z.enum(['committed', 'staged', 'unstaged', 'untracked']).optional()}), async (input) => {
        const session = await authorize(input.workId, input.sessionId);
        return workspaceResult(await client.get(scopedWorkspacePath(input.workId, input.sessionId, session, 'file', {path: input.path, section: input.section})), input.sessionId, fileFields, input.path);
      }),
    defineTool('ao_workspace_diff', 'Read bounded unified patches for explicit workspace paths and an optional captured workspaceVersion.', 'developer', true,
      z.strictObject({...sessionInput, paths: z.array(relativePath).min(1).max(100), scope: scope.default('combined'), contextLines: z.number().int().min(0).max(20).default(3), ignoreWhitespace: z.boolean().default(false), workspaceVersion: z.string().min(1).max(512).optional()}), async ({workId, sessionId, ...input}) => {
        const session = await authorize(workId, sessionId);
        return workspaceResult(await client.post(scopedWorkspacePath(workId, sessionId, session, 'diffs'), input), sessionId, ['sessionId', 'workspaceVersion', 'groups']);
      }),
    defineTool('ao_workspace_update', 'Replace an existing UTF-8 text file using the fingerprint from a complete read. Does not create/delete files; stale edits are rejected by AO.', 'developer', false,
      z.strictObject({...sessionInput, path: relativePath, content: z.string().max(256 * 1024).refine((text) => Buffer.byteLength(text, 'utf8') <= 256 * 1024 && !text.includes('\0'), 'Only bounded UTF-8 text content is accepted.'), expectedFileFingerprint: fingerprint}), async ({workId, sessionId, ...input}) => {
        const session = await authorize(workId, sessionId);
        const finish = jobs.reserveEdit(workId, sessionId);
        try {
          const result = workspaceResult(await client.put(scopedWorkspacePath(workId, sessionId, session, 'file'), input), sessionId, fileFields, input.path);
          finish();
          return result;
        } catch (error) {
          finish(error);
          throw error;
        }
      }),
    defineTool('ao_check_profiles', 'Discover pinned check-profile names, timeouts and availability within the current REST deadline. Commands, environment and policy configuration are never returned.', 'developer', true,
      z.strictObject({workId: identifier}), async ({workId}) => {
        requireRole(access, 'developer');
        const work = record((await scopedWork(client, access, workId)).work);
        const profiles = record(record(work.policy).profiles);
        return {
          workId, ...scalars(work, ['policyVersion']), maxTotalTimeoutSeconds: maxCheckSeconds,
          profiles: Object.entries(profiles).flatMap(([name, candidate]) => {
            if (!profileName.safeParse(name).success || !candidate || typeof candidate !== 'object' || Array.isArray(candidate)) return [];
            const profile = record(candidate);
            if (!Array.isArray(profile.command) || profile.command.length === 0 || !profile.command.every((part) => typeof part === 'string')
              || typeof profile.timeoutSeconds !== 'number' || !Number.isInteger(profile.timeoutSeconds) || profile.timeoutSeconds <= 0) return [];
            const available = profile.timeoutSeconds <= maxCheckSeconds;
            return [{name, timeoutSeconds: profile.timeoutSeconds, available, ...(!available ? {unavailableReason: 'CHECK_DEADLINE_LIMIT'} : {})}];
          }).sort((left, right) => left.name.localeCompare(right.name, 'en')),
        };
      }),
    defineTool('ao_run_checks', 'Start approved work-policy check profiles with combined configured timeouts of at most 45 seconds. Returns a pollable handle, not a durable daemon job. Reuse operationId; never supplies argv. Requires a daemon-managed worker and clean committed workspace.', 'developer', false,
      z.strictObject({...sessionInput, operationId: identifier, profiles: z.array(profileName).min(1).max(16).refine((values) => new Set(values).size === values.length, 'Profile names must be unique.')}), async (input) => {
        await authorize(input.workId, input.sessionId);
        const details = await scopedWork(client, access, input.workId);
        const timeout = approvedCheckTimeout(details, input.profiles);
        return jobs.start(access, input, async () => checkResult(await client.post(`/api/v1/instructor/works/${encodeURIComponent(input.workId)}/checks`, {sessionId: input.sessionId, profiles: input.profiles}, timeout), access, input.workId));
      }),
    defineTool('ao_check_job_status', 'Read this principal\'s check-job handle. Completed means the request completed; daemon evidence is authoritative. Uncertain outcomes are never automatically retried. Handles expire when the adapter restarts.', 'developer', true,
      z.strictObject({jobId: z.uuid()}), async ({jobId}) => {
        requireRole(access, 'developer');
        const job = jobs.get(access, jobId);
        // Polling reads a principal-owned captured result, not a live workspace.
        // A worker may exit or be replaced after checks finish; only the work's
        // current project authorization must still hold.
        await scopedWork(client, access, job.workId);
        return job;
      }),
  ];
}

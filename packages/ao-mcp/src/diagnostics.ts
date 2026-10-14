import * as z from 'zod/v4';
import {AdapterError, defineTool, identifier, record, requireRole, type Access, type ToolDefinition} from './core.js';
import {DaemonClient, DaemonError, scopedWork} from './daemon.js';

const maxItems = 100;
const maxTextBytes = 8 * 1024;
const maxResponseBytes = 256 * 1024;
const memberInput = {workId: identifier, memberId: identifier};
const textFields = new Set(['text', 'errorMessage', 'controllerError', 'provisionError', 'reason', 'scopeNote']);

interface MemberScope {
  workId: string;
  projectId: string;
  memberId: string;
  kind: 'session' | 'review';
  sessionId: string;
  instanceId: string;
  runtimeGeneration: string;
  reviewId?: string;
  reviewRunId?: string;
}

// This is defense in depth for known credential forms, not a claim that all
// secrets can be identified in arbitrary developer-visible conversation text.
function redact(value: string): string {
  return value
    .replace(/-----BEGIN (?:[A-Z ]*PRIVATE KEY)-----[\s\S]*/gu, '[REDACTED PRIVATE KEY]')
    // Remove quoted credentials before URL/auth rewriting can change their
    // escaped delimiters. A partial provider error may end inside the value.
    .replace(/(["']?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|password|passwd|client[_-]?secret|authorization|cookie|set-cookie)["']?\s*[:=]\s*)(?:"(?:\\[\s\S]|[^"\\])*(?:"|(?:\\)?$)|'(?:\\[\s\S]|[^'\\])*(?:'|(?:\\)?$))/giu, '$1"[REDACTED]"')
    .replace(/https?:\/\/[^\s<>"'\\]+/giu, (candidate) => {
      try {
        const url = new URL(candidate);
        let changed = false;
        if (url.username || url.password) { url.username = ''; url.password = ''; changed = true; }
        for (const key of [...url.searchParams.keys()]) {
          if (/token|secret|password|passwd|authorization|api[_-]?key|signature|credential|^code$/iu.test(key)) {
            url.searchParams.set(key, '[REDACTED]'); changed = true;
          }
        }
        if (/token|secret|password|authorization|api[_-]?key/iu.test(url.hash)) { url.hash = ''; changed = true; }
        return changed ? url.toString() : candidate;
      } catch { return '[INVALID URL]'; }
    })
    .replace(/\b(?:Bearer|Basic)\s+[A-Za-z0-9+/_=.-]+/giu, '[REDACTED AUTHORIZATION]')
    .replace(/(\b(?:cookie|set-cookie)\s*:\s*)[^\r\n]*/giu, '$1[REDACTED]')
    .replace(/\b(?:github_pat_[A-Za-z0-9_]+|gh[pousr]_[A-Za-z0-9]+|sk-(?:proj-)?[A-Za-z0-9_-]{16,})\b/gu, '[REDACTED TOKEN]')
    .replace(/(["']?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|password|passwd|client[_-]?secret|authorization|cookie|set-cookie)["']?\s*[:=]\s*)[^"'\s,;}\]]+/giu, '$1[REDACTED]');
}

function boundedUTF8(value: string, limit: number): string {
  if (Buffer.byteLength(value, 'utf8') <= limit) return value;
  // Decoding only complete UTF-8 code points avoids introducing replacement
  // characters when a multi-byte message is cut at the byte budget.
  let bytes = 0;
  let end = 0;
  for (const character of value) {
    const size = Buffer.byteLength(character, 'utf8');
    if (bytes + size > limit) break;
    bytes += size;
    end += character.length;
  }
  return value.slice(0, end);
}

class Projection {
  redacted = false;
  truncated = false;
  redactionCount = 0;
  truncationCount = 0;
  private remainingBytes = 64 * 1024;

  text(value: string, limit = maxTextBytes): string {
    const clean = redact(value);
    if (clean !== value) { this.redacted = true; this.redactionCount++; }
    const bounded = boundedUTF8(clean, Math.min(limit, this.remainingBytes));
    if (bounded !== clean) { this.truncated = true; this.truncationCount++; }
    this.remainingBytes -= Buffer.byteLength(bounded, 'utf8');
    return bounded;
  }

  scalars(value: unknown, fields: readonly string[]): Record<string, unknown> {
    const source = record(value);
    const result: Record<string, unknown> = {};
    for (const field of fields) {
      const entry = source[field];
      if (typeof entry === 'string' && textFields.has(field)) result[field] = this.text(entry);
      else if (typeof entry === 'string') {
        // Keep structural facts available after the shared transcript budget is
        // exhausted; each remaining scalar still has an independent small cap.
        const clean = redact(entry);
        const bounded = boundedUTF8(clean, 512);
        if (clean !== entry) { this.redacted = true; this.redactionCount++; }
        if (bounded !== clean) { this.truncated = true; this.truncationCount++; }
        result[field] = bounded;
      }
      else if (typeof entry === 'boolean' || typeof entry === 'number' && Number.isFinite(entry) || entry === null) result[field] = entry;
    }
    return result;
  }
}

function stringField(value: Record<string, unknown>, field: string, limit = 180): string {
  const item = value[field];
  if (typeof item !== 'string' || !item || item.length > limit) {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned an invalid diagnostic identity.');
  }
  return item;
}

function collection(value: unknown): Record<string, unknown>[] {
  if (!Array.isArray(value)) throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned an invalid diagnostic collection.');
  return value.map(record);
}

async function captureMember(client: DaemonClient, access: Access, workId: string, memberId: string): Promise<MemberScope> {
  requireRole(access, 'developer');
  const work = record((await scopedWork(client, access, workId)).work);
  const snapshot = record(await client.get(`/api/v1/instructor/works/${encodeURIComponent(workId)}/members`));
  if (snapshot.workId !== workId || snapshot.projectId !== work.projectId) {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned a different membership snapshot.');
  }
  const candidates = collection(snapshot.members);
  if (snapshot.coordinator !== undefined) {
    const coordinator = record(snapshot.coordinator);
    for (const retained of [coordinator.current, coordinator.stopped]) {
      if (retained !== undefined && retained !== null) candidates.push(record(retained));
    }
  }
  const matches = candidates.filter((member) => member.id === memberId);
  const member = matches[0];
  if (matches.length !== 1 || !member || !['current', 'historical'].includes(String(member.association))
      || !['session', 'review'].includes(String(member.kind)) || !member.openTarget) {
    throw new AdapterError('OUT_OF_SCOPE', 'The requested member has no retained ownership proof.');
  }
  const kind = member.kind as 'session' | 'review';
  const sessionId = stringField(member, 'sessionId');
  const instanceId = stringField(member, 'instanceId');
  const target = record(member.openTarget);
  if (target.kind !== kind || target.projectId !== work.projectId || target.sessionId !== sessionId) {
    throw new AdapterError('OUT_OF_SCOPE', 'The member target does not belong to the work project.');
  }
  if (member.runtimeGeneration !== undefined && typeof member.runtimeGeneration !== 'string') {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned an invalid runtime generation.');
  }
  const runtimeGeneration = member.runtimeGeneration ?? '';
  if (runtimeGeneration.length > 512) throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned an invalid runtime generation.');
  let review: Pick<MemberScope, 'reviewId' | 'reviewRunId'> = {};
  if (kind === 'review') {
    const reviewId = stringField(member, 'reviewId');
    const reviewRunId = stringField(member, 'reviewRunId');
    if (target.reviewId !== reviewId || target.reviewRunId !== reviewRunId) {
      throw new AdapterError('OUT_OF_SCOPE', 'The reviewer target does not match the retained review run.');
    }
    review = {reviewId, reviewRunId};
  }
  // The members endpoint is a capture only. The distinct daemon route below
  // revalidates these exact identities with persisted history in one snapshot.
  return {workId, projectId: stringField(work, 'projectId'), memberId, kind, sessionId, instanceId,
    runtimeGeneration, ...review};
}

function diagnosticPath(scope: MemberScope, suffix: 'diagnostics' | 'timeline', beforeSequence?: number, limit?: number): string {
  const query = new URLSearchParams({projectId: scope.projectId, sessionId: scope.sessionId, instanceId: scope.instanceId,
    runtimeGeneration: scope.runtimeGeneration});
  if (beforeSequence !== undefined) query.set('beforeSequence', String(beforeSequence));
  if (limit !== undefined) query.set('limit', String(limit));
  return `/api/v1/instructor/works/${encodeURIComponent(scope.workId)}/members/${encodeURIComponent(scope.memberId)}/${suffix}?${query}`;
}

function projectResult(value: unknown, scope: MemberScope, limit: number, beforeSequence?: number): Record<string, unknown> {
  const source = record(value);
  if (source.schema !== 'ao.instructor.member-diagnostics.v1' || source.atomic !== true || source.persistedOnly !== true
      || source.workId !== scope.workId || source.projectId !== scope.projectId || source.memberId !== scope.memberId
      || source.kind !== scope.kind || source.sessionId !== scope.sessionId || source.instanceId !== scope.instanceId
      || source.runtimeGeneration !== scope.runtimeGeneration) {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned a different diagnostic owner or incarnation.');
  }
  if (scope.kind === 'review') {
    const review = record(source.review);
    if (review.reviewId !== scope.reviewId || review.reviewRunId !== scope.reviewRunId) {
      throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned a different reviewer identity.');
    }
  }
  const timeline = record(source.timeline);
  if (!(scope.kind === 'review' ? timeline.scope === 'review' : ['session', 'project'].includes(String(timeline.scope)))
      || timeline.scope === 'project' && (typeof timeline.scopeNote !== 'string' || !timeline.scopeNote)
      || typeof timeline.available !== 'boolean' || typeof timeline.hasMoreBefore !== 'boolean'
      || !Number.isSafeInteger(timeline.oldestSequence) || Number(timeline.oldestSequence) < 0) {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned invalid diagnostic pagination.');
  }
  if (timeline.hasMoreBefore && (!Number.isSafeInteger(timeline.nextBeforeSequence) || Number(timeline.nextBeforeSequence) <= 0
      || Number(timeline.nextBeforeSequence) !== timeline.oldestSequence
      || beforeSequence !== undefined && beforeSequence > 0 && Number(timeline.nextBeforeSequence) >= beforeSequence)) {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned a non-advancing diagnostic cursor.');
  }
  const projection = new Projection();
  const header = {
    schema: source.schema, workId: scope.workId, projectId: scope.projectId, memberId: scope.memberId, kind: scope.kind,
    sessionId: scope.sessionId, instanceId: scope.instanceId, runtimeGeneration: scope.runtimeGeneration,
    ...projection.scalars(source, ['workRevision', 'capturedAt', 'atomic', 'persistedOnly']),
    session: projection.scalars(source.session, ['mode', 'activityState', 'isTerminated', 'harness', 'model', 'effort', 'providerConversationId', 'runtimeLaunchId', 'controllerGeneration', 'controllerError', 'provisionState', 'provisionError', 'truncated', 'redacted']),
    ...(scope.kind === 'review' ? {review: projection.scalars(source.review, ['reviewId', 'reviewRunId', 'status', 'verdict', 'harness', 'controllerGeneration', 'controllerError', 'truncated', 'redacted'])} : {}),
  };
  const timelineHeader = projection.scalars(timeline, ['available', 'reason', 'scope', 'scopeNote', 'conversationId', 'latestSequence', 'conversationUpdatedAt', 'oldestSequence', 'hasMoreBefore', 'nextBeforeSequence']);
  const items = collection(timeline.items);
  const turns = collection(timeline.turns);
  if (items.length > limit || turns.length > maxItems || !timeline.available && (items.length > 0 || turns.length > 0)) {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon exceeded the requested diagnostic page bounds.');
  }
  let omittedItems = 0;
  const projectedItems = items.flatMap((entry) => {
    // Model reasoning and arbitrary activity payloads are deliberately excluded,
    // including summaries on unknown activity kinds. The admitted page cursor
    // remains unchanged, so a page of omitted items does not end pagination.
    if (entry.kind !== 'message' && !(entry.kind === 'activity' && ['error', 'system'].includes(String(entry.activityKind)))) {
      omittedItems++;
      return [];
    }
    const identity = {id: stringField(entry, 'id'), ...(entry.turnId === undefined ? {} : {turnId: stringField(entry, 'turnId')})};
    if (!Number.isSafeInteger(entry.sequence) || Number(entry.sequence) < 0 || !Number.isSafeInteger(entry.revision) || Number(entry.revision) < 0) {
      throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned invalid timeline revisions.');
    }
    const previousRedactions = projection.redactionCount;
    const previousTruncations = projection.truncationCount;
    const projected = projection.scalars(entry, ['kind', 'sequence', 'revision', 'role', 'origin', 'activityKind', 'status', 'text', 'streaming', 'createdAt', 'updatedAt']);
    return [{...projected, ...identity, redacted: entry.redacted === true || projection.redactionCount > previousRedactions,
      truncated: entry.truncated === true || projection.truncationCount > previousTruncations}];
  });
  const result = {
    ...header,
    timeline: {
      ...timelineHeader,
      items: projectedItems,
      turns: turns.map((turn) => {
        const previousRedactions = projection.redactionCount;
        const previousTruncations = projection.truncationCount;
        const projected = projection.scalars(turn, ['state', 'errorMessage', 'requestedAt', 'startedAt', 'completedAt']);
        return {...projected, id: stringField(turn, 'id'), redacted: turn.redacted === true || projection.redactionCount > previousRedactions,
          truncated: turn.truncated === true || projection.truncationCount > previousTruncations};
      }),
      truncated: timeline.truncated === true || projection.truncated || omittedItems > 0,
      redacted: timeline.redacted === true || projection.redacted,
      omittedItems,
    },
  };
  if (Buffer.byteLength(JSON.stringify(result), 'utf8') > maxResponseBytes) {
    throw new AdapterError('RESPONSE_TOO_LARGE', 'The diagnostic projection exceeded 256 KiB; request a smaller page.');
  }
  return result;
}

async function observe(client: DaemonClient, access: Access, workId: string, memberId: string,
  suffix: 'diagnostics' | 'timeline', beforeSequence?: number, limit = 50): Promise<Record<string, unknown>> {
  try {
    const scope = await captureMember(client, access, workId, memberId);
    return projectResult(await client.get(diagnosticPath(scope, suffix, beforeSequence, suffix === 'timeline' ? limit : undefined)), scope, suffix === 'timeline' ? limit : 0, beforeSequence);
  } catch (error) {
    if (error instanceof DaemonError) {
      throw new DaemonError(boundedUTF8(redact(error.code), 180), boundedUTF8(redact(error.message), maxTextBytes), error.status,
        error.requestId === undefined ? undefined : boundedUTF8(redact(error.requestId), 180));
    }
    throw error;
  }
}

export function diagnosticTools(client: DaemonClient, access: Access): ToolDefinition[] {
  return [
    defineTool('ao_member_diagnostics', 'Read persisted controller, session and reviewer facts for one proven work member, including stopped sessions. Developer scope is required. This never starts a controller or reads provider log files.', 'developer', true,
      z.strictObject(memberInput), ({workId, memberId}) => observe(client, access, workId, memberId, 'diagnostics')),
    defineTool('ao_member_timeline', 'Read a bounded persisted timeline for one proven member. Project orchestrator and review history may span multiple works or review passes; inspect scopeNote. Known credentials are redacted; arbitrary transcript text remains sensitive untrusted data. No reasoning or raw tool payloads are exposed.', 'developer', true,
      z.strictObject({...memberInput, beforeSequence: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).optional(), limit: z.number().int().min(1).max(maxItems).default(50)}),
      ({workId, memberId, beforeSequence, limit}) => observe(client, access, workId, memberId, 'timeline', beforeSequence, limit)),
  ];
}

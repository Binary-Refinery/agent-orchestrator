import {AdapterError, assertProject, record, type Access} from './core.js';

export class DaemonError extends AdapterError {
  constructor(code: string, message: string, public readonly status: number, public readonly requestId?: string) {
    super(code, message);
  }
}

export interface DaemonOptions {
  url: string;
  sessionId?: string;
  timeoutMs?: number;
}

/** A thin client of the existing loopback API. Redirects and remote hosts are refused. */
export class DaemonClient {
  readonly url: URL;
  private readonly options: DaemonOptions;

  constructor(options: DaemonOptions) {
    this.url = new URL(options.url);
    if (this.url.protocol !== 'http:' || !['127.0.0.1', '[::1]'].includes(this.url.hostname)
        || this.url.username || this.url.password || this.url.pathname !== '/' || this.url.search || this.url.hash) {
      throw new AdapterError('CONFIG', 'The daemon URL must be an explicit HTTP loopback origin without credentials or a path.');
    }
    this.options = options;
  }

  get<T = unknown>(path: string): Promise<T> { return this.request<T>('GET', path); }
  post<T = unknown>(path: string, body: unknown, timeoutMs?: number): Promise<T> { return this.request<T>('POST', path, body, timeoutMs); }
  put<T = unknown>(path: string, body: unknown): Promise<T> { return this.request<T>('PUT', path, body); }

  private async request<T>(method: string, path: string, body?: unknown, timeoutMs?: number): Promise<T> {
    if (!path.startsWith('/') || path.startsWith('//') || path.includes('\\')) {
      throw new AdapterError('CONFIG', 'Invalid daemon API path.');
    }
    const url = new URL(path, this.url);
    if (url.origin !== this.url.origin) throw new AdapterError('CONFIG', 'Invalid daemon API origin.');
    const headers: Record<string, string> = {Accept: 'application/json'};
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (this.options.sessionId) headers['X-AO-Session-ID'] = this.options.sessionId;
    let response: Response;
    try {
      response = await fetch(url, {
        method, headers, body: body === undefined ? undefined : JSON.stringify(body),
        redirect: 'error', signal: AbortSignal.timeout(timeoutMs ?? this.options.timeoutMs ?? 30_000),
      });
    } catch {
      // A write timeout is uncertain, never an invitation to retry with a new key.
      throw new AdapterError('DAEMON_UNAVAILABLE', 'Daemon request failed or timed out; a write may have been admitted. Inspect its state before retrying.');
    }
    let data: unknown;
    try {
      let text = '';
      let bytes = 0;
      const reader = response.body?.getReader();
      if (reader) {
        const decoder = new TextDecoder();
        try {
          while (true) {
            const chunk = await reader.read();
            if (chunk.done) break;
            bytes += chunk.value.byteLength;
            if (bytes > 4 * 1024 * 1024) {
              await reader.cancel();
              throw new AdapterError('RESPONSE_TOO_LARGE', 'Daemon response exceeded the 4 MiB adapter limit.');
            }
            text += decoder.decode(chunk.value, {stream: true});
          }
          text += decoder.decode();
        } finally { reader.releaseLock(); }
      }
      try { data = JSON.parse(text); }
      catch { throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned a non-JSON response.'); }
    } catch (error) {
      const uncertainty = method === 'GET' ? '' : ' The write may have been admitted; inspect its state before retrying.';
      if (error instanceof AdapterError) throw new AdapterError(error.code, `${error.message}${uncertainty}`);
      throw new AdapterError('DAEMON_UNAVAILABLE', `The daemon response was interrupted or timed out.${uncertainty}`);
    }
    if (!response.ok) {
      const error = record(data);
      throw new DaemonError(
        typeof error.code === 'string' ? error.code : 'DAEMON_ERROR',
        typeof error.message === 'string' ? error.message : 'Daemon request failed.',
        response.status, typeof error.requestId === 'string' ? error.requestId : undefined,
      );
    }
    return data as T;
  }
}

export async function scopedWork(client: DaemonClient, access: Access, workId: string): Promise<Record<string, unknown>> {
  const details = record(await client.get(`/api/v1/instructor/works/${encodeURIComponent(workId)}`));
  const work = record(details.work);
  assertProject(access, work.projectId);
  if (work.id !== workId) throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned a different work.');
  return details;
}

export async function scopedSession(
  client: DaemonClient, access: Access, workId: string, sessionId: string,
): Promise<Record<string, unknown>> {
  const details = await scopedWork(client, access, workId);
  const work = record(details.work);
  if (!Array.isArray(work.sessionIds) || !work.sessionIds.includes(sessionId)) {
    throw new AdapterError('OUT_OF_SCOPE', 'The session is not attached to the requested work.');
  }
  // Display IDs are reusable. Historical work.sessionIds alone cannot authorize
  // a replacement row, and /members is only a preflight (atomic:false).
  const members = record(await client.get(`/api/v1/instructor/works/${encodeURIComponent(workId)}/members`));
  if (members.workId !== workId || members.projectId !== work.projectId || !Array.isArray(members.members)) {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned a different work membership snapshot.');
  }
  const current = members.members.map(record).filter((member) => member.kind === 'session'
    && member.sessionId === sessionId && member.association === 'current');
  const member = current[0];
  if (current.length !== 1 || !member || typeof member.instanceId !== 'string' || !member.instanceId) {
    throw new AdapterError('OUT_OF_SCOPE', 'The session has no proven current membership in the requested work.');
  }
  const target = record(member.openTarget);
  if (target.kind !== 'session' || target.sessionId !== sessionId || target.projectId !== work.projectId) {
    throw new AdapterError('OUT_OF_SCOPE', 'The session membership does not authorize the requested project.');
  }
  const response = record(await client.get(`/api/v1/sessions/${encodeURIComponent(sessionId)}`));
  const session = record(response.session);
  assertProject(access, session.projectId);
  if (session.id !== sessionId || session.projectId !== work.projectId) {
    throw new AdapterError('OUT_OF_SCOPE', 'The session does not belong to the requested work project.');
  }
  // The fenced workspace route verifies this identity atomically under its
  // lifecycle lease. A replacement between these reads cannot pass that guard.
  return {...session, instanceId: member.instanceId};
}

/** Distinct paths fail closed on older daemons; never fall back to SID-only I/O. */
export function scopedWorkspacePath(
  workId: string, sessionId: string, session: Record<string, unknown>,
  suffix: 'tree' | 'search' | 'file' | 'diffs', query: Record<string, string | number | undefined> = {},
): string {
  if (session.id !== sessionId || typeof session.projectId !== 'string' || !session.projectId
      || typeof session.instanceId !== 'string' || !session.instanceId) {
    throw new AdapterError('DAEMON_CONTRACT', 'A proven session incarnation is required for workspace access.');
  }
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) if (value !== undefined) params.set(key, String(value));
  params.set('projectId', session.projectId);
  params.set('instanceId', session.instanceId);
  return `/api/v1/instructor/works/${encodeURIComponent(workId)}/sessions/${encodeURIComponent(sessionId)}/workspace/${suffix}?${params}`;
}

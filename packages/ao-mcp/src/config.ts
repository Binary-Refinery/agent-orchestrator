import {readFile} from 'node:fs/promises';
import * as z from 'zod/v4';
import {AdapterError, identifier} from './core.js';
import {DaemonClient} from './daemon.js';

export const configSchema = z.strictObject({
  daemonUrl: z.string().max(256),
  transport: z.enum(['stdio', 'http']).default('stdio'),
  access: z.strictObject({
    principal: identifier,
    role: z.enum(['observer', 'operator', 'developer']).default('observer'),
    projectIds: z.array(identifier).min(1).max(100),
    sessionId: identifier.optional(),
  }),
  http: z.strictObject({
    port: z.number().int().min(1).max(65535),
    tokenEnv: z.string().regex(/^[A-Z][A-Z0-9_]{1,127}$/),
  }).optional(),
}).superRefine((config, context) => {
  if (config.transport === 'http' && !config.http) {
    context.addIssue({code: 'custom', message: 'HTTP transport requires port and tokenEnv.', path: ['http']});
  }
});

export type AdapterConfig = z.output<typeof configSchema>;

export async function loadConfig(path: string): Promise<AdapterConfig> {
  const bytes = await readFile(path);
  if (bytes.byteLength > 64 * 1024) throw new AdapterError('CONFIG', 'Configuration exceeds 64 KiB.');
  const config = configSchema.parse(JSON.parse(bytes.toString('utf8')));
  // Validate before opening a transport, even for an observer without any calls.
  new DaemonClient({url: config.daemonUrl});
  return config;
}

export function httpToken(config: AdapterConfig, env: NodeJS.ProcessEnv = process.env): string {
  const value = config.http && env[config.http.tokenEnv];
  if (!value || !/^[A-Za-z0-9_-]{32,512}$/.test(value)) {
    throw new AdapterError('CONFIG', 'HTTP token environment variable must contain a 32–512 character base64url or hex token.');
  }
  return value;
}

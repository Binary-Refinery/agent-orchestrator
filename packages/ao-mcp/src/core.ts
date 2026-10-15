import * as z from 'zod/v4';

export type Role = 'observer' | 'operator' | 'developer';

export interface Access {
  principal: string;
  role: Role;
  projectIds: readonly string[];
  /** Trusted launch identity, never a tool argument. */
  sessionId?: string;
}

export class AdapterError extends Error {
  constructor(public readonly code: string, message: string) {
    super(message);
  }
}

export const identifier = z.string().min(1).max(180).regex(/^[A-Za-z0-9][A-Za-z0-9_.:-]*$/);

export interface ToolDefinition {
  name: string;
  description: string;
  role: Role;
  readOnly: boolean;
  destructive: boolean;
  openWorld: boolean;
  inputSchema: z.ZodObject;
  execute(input: unknown): Promise<unknown>;
}

export function defineTool<S extends z.ZodRawShape>(
  name: string,
  description: string,
  role: Role,
  readOnly: boolean,
  schema: z.ZodObject<S>,
  run: (input: z.output<z.ZodObject<S>>) => Promise<unknown>,
  destructive = !readOnly,
  openWorld = !readOnly,
): ToolDefinition {
  return {
    name, description, role, readOnly, destructive, openWorld, inputSchema: schema,
    execute: async (input) => run(schema.parse(input)),
  };
}

const rank: Record<Role, number> = {observer: 0, operator: 1, developer: 2};

export function requireRole(access: Access, role: Role): void {
  if (typeof access.role !== 'string' || !Object.hasOwn(rank, access.role)
      || typeof role !== 'string' || !Object.hasOwn(rank, role) || rank[access.role] < rank[role]) {
    throw new AdapterError('FORBIDDEN', `This tool requires the ${role} profile.`);
  }
  if (!Array.isArray(access.projectIds) || !access.projectIds.every((project) => typeof project === 'string')) {
    throw new AdapterError('OUT_OF_SCOPE', 'Project scope must be an explicit list of project IDs.');
  }
}

export function assertProject(access: Access, projectId: unknown): asserts projectId is string {
  if (typeof projectId !== 'string' || !Array.isArray(access.projectIds)
      || !access.projectIds.every((project) => typeof project === 'string') || !access.projectIds.includes(projectId)) {
    throw new AdapterError('OUT_OF_SCOPE', 'The requested resource is outside the configured project scope.');
  }
}

export function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new AdapterError('DAEMON_CONTRACT', 'The daemon returned an unexpected response shape.');
  }
  return value as Record<string, unknown>;
}

export function pick(value: unknown, fields: readonly string[]): Record<string, unknown> {
  const source = record(value);
  return Object.fromEntries(fields.filter((key) => Object.hasOwn(source, key)).map((key) => [key, source[key]]));
}

import {createHash, timingSafeEqual} from 'node:crypto';
import {createServer, type Server} from 'node:http';
import {createMcpHandler} from '@modelcontextprotocol/server';
import {localhostHostValidation, toNodeHandler} from '@modelcontextprotocol/node';
import {AdapterError} from './core.js';
import {configSchema, type AdapterConfig} from './config.js';
import {createAOMcpServer, type AuditSink} from './server.js';

export interface HttpHandle {
  server: Server;
  url: string;
  close(): Promise<void>;
}

/** Loopback only. A private external tunnel/gateway supplies the remote access path. */
export async function startHttp(input: AdapterConfig, token: string, audit?: AuditSink, testPort?: number): Promise<HttpHandle> {
  const config = configSchema.parse(input);
  if (config.transport !== 'http' || !config.http || !/^[A-Za-z0-9_-]{32,512}$/.test(token)) {
    throw new AdapterError('CONFIG', 'Authenticated HTTP configuration is required.');
  }
  const tokenHash = createHash('sha256').update(`Bearer ${token}`).digest();
  const handler = createMcpHandler(() => createAOMcpServer(config, audit), {
    legacy: 'stateless', maxRequestBodySize: 1024 * 1024, maxSubscriptions: 0,
  });
  const nodeHandler = toNodeHandler(handler);
  const validateHost = localhostHostValidation();
  const server = createServer({maxHeaderSize: 16 * 1024, requestTimeout: 15_000, headersTimeout: 10_000}, (request, response) => {
    response.setHeader('Cache-Control', 'no-store');
    if (!validateHost(request, response)) return;
    if (request.headers.origin !== undefined) {
      const origin = request.headers.origin;
      const port = (server.address() as {port: number}).port;
      if (origin !== `http://127.0.0.1:${port}` && origin !== `http://localhost:${port}`) {
        response.writeHead(403).end('Origin refused.'); return;
      }
    }
    const supplied = createHash('sha256').update(request.headers.authorization ?? '').digest();
    if (!timingSafeEqual(tokenHash, supplied)) {
      response.writeHead(401, {'WWW-Authenticate': 'Bearer realm="ao-mcp"'}).end('Authentication required.'); return;
    }
    if (request.url !== '/mcp') { response.writeHead(404).end(); return; }
    void nodeHandler(request, response).catch(() => {
      if (!response.headersSent) response.writeHead(500).end('MCP request failed.');
      else response.destroy();
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(testPort ?? config.http!.port, '127.0.0.1', () => {
      server.removeListener('error', reject); resolve();
    });
  });
  const address = server.address() as {port: number};
  return {
    server, url: `http://127.0.0.1:${address.port}/mcp`,
    async close() {
      await handler.close();
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
    },
  };
}

#!/usr/bin/env node
import {serveStdio, StdioServerTransport} from '@modelcontextprotocol/server/stdio';
import {loadConfig, httpToken} from './config.js';
import {createAOMcpServer} from './server.js';
import {startHttp} from './http.js';

async function main(): Promise<void> {
  const args = process.argv.slice(2);
  if (args.length !== 2 || args[0] !== '--config' || !args[1]) {
    throw new Error('Usage: ao-mcp --config /absolute/path/to/config.json');
  }
  const config = await loadConfig(args[1]);
  if (config.transport === 'stdio') {
    const handle = serveStdio(() => createAOMcpServer(config), {
      transport: new StdioServerTransport(process.stdin, process.stdout, {maxBufferSize: 1024 * 1024}),
      onerror: () => process.stderr.write('AO MCP protocol error.\n'),
    });
    process.once('SIGINT', () => { void handle.close(); });
    process.once('SIGTERM', () => { void handle.close(); });
  } else {
    const handle = await startHttp(config, httpToken(config));
    process.stderr.write(`AO MCP listening at ${handle.url} (loopback only).\n`);
    process.once('SIGINT', () => { void handle.close(); });
    process.once('SIGTERM', () => { void handle.close(); });
  }
}

main().catch(() => {
  // Do not echo untrusted configuration or environment values into host logs.
  process.stderr.write('AO MCP startup failed. Check --config, project scope, daemon origin and HTTP token settings.\n');
  process.exitCode = 1;
});

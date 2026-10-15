import assert from 'node:assert/strict';
import {test} from 'node:test';
import {configSchema, httpToken} from '../src/config.js';
import {DaemonClient} from '../src/daemon.js';

const config = {daemonUrl: 'http://127.0.0.1:3001', access: {principal: 'local', projectIds: ['p1']}};

test('scope is mandatory, observer and stdio are the defaults', () => {
  assert.equal(configSchema.parse(config).access.role, 'observer');
  assert.equal(configSchema.parse(config).transport, 'stdio');
  assert.throws(() => configSchema.parse({...config, access: {...config.access, projectIds: []}}));
  assert.throws(() => configSchema.parse({...config, access: {...config.access, role: 'admin'}}));
  assert.throws(() => configSchema.parse({...config, authorization: '*'}));
});

test('HTTP requires explicit token reference and port, no wildcard bind setting', () => {
  assert.throws(() => configSchema.parse({...config, transport: 'http'}));
  assert.throws(() => configSchema.parse({...config, http: {port: 3334, tokenEnv: 'AO_MCP_TOKEN', host: '0.0.0.0'}}));
  const parsed = configSchema.parse({...config, transport: 'http', http: {port: 3334, tokenEnv: 'AO_MCP_TOKEN'}});
  assert.throws(() => httpToken(parsed, {}));
  assert.throws(() => httpToken(parsed, {AO_MCP_TOKEN: 'short'}));
  assert.equal(httpToken(parsed, {AO_MCP_TOKEN: 'a'.repeat(64)}), 'a'.repeat(64));
});

test('daemon origin is pinned to numeric loopback and redirects cannot change it', () => {
  for (const url of ['http://localhost:3001', 'http://example.com:3001', 'https://127.0.0.1:3001',
    'http://127.0.0.1:3001/api', 'http://name:secret@127.0.0.1:3001', 'http://127.0.0.1:3001/?token=x']) {
    assert.throws(() => new DaemonClient({url}));
  }
  assert.equal(new DaemonClient({url: config.daemonUrl}).url.hostname, '127.0.0.1');
});

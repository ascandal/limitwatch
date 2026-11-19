import { afterEach, beforeEach, test } from 'node:test';
import assert from 'node:assert/strict';
import { proxyRequest } from './proxy.ts';

const originalFetch = globalThis.fetch;
const originalOrigin = process.env.LIMITWATCH_API_URL;
beforeEach(() => {
  process.env.LIMITWATCH_API_URL = 'http://go-service:8080';
});
afterEach(() => {
  globalThis.fetch = originalFetch;
  if (originalOrigin === undefined) delete process.env.LIMITWATCH_API_URL;
  else process.env.LIMITWATCH_API_URL = originalOrigin;
});

function request(path, method = 'GET') {
  return new Request(`http://console:3000${path}`, {
    method,
    headers: {
      Authorization: 'Bearer test-token',
      Cookie: 'browser-only=test',
    },
  });
}

test('forwards query and bearer token without browser cookies or cached responses', async () => {
  globalThis.fetch = async (url, options) => {
    assert.equal(
      url.href,
      'http://go-service:8080/v1/alarms/7/events?after_id=4&limit=50',
    );
    assert.equal(options.headers.get('authorization'), 'Bearer test-token');
    assert.equal(options.headers.get('cookie'), null);
    assert.equal(options.cache, 'no-store');
    assert.equal(options.redirect, 'manual');
    assert.ok(options.signal instanceof AbortSignal);
    return Response.json({
      events: [{ id: 5 }],
      next_after_id: 5,
      has_more: false,
    });
  };
  const response = await proxyRequest(
    request('/v1/alarms/7/events?after_id=4&limit=50'),
  );
  assert.equal(response.status, 200);
  assert.equal(response.headers.get('cache-control'), 'no-store');
  assert.equal((await response.json()).events[0].id, 5);
});

test('preserves Go authentication, permission, and conflict responses', async () => {
  for (const status of [401, 403, 409]) {
    globalThis.fetch = async (_url, options) => {
      assert.equal(options.method, 'POST');
      return new Response('Go rejected the action', { status });
    };
    const response = await proxyRequest(request('/v1/alarms/7/ack', 'POST'));
    assert.equal(response.status, status);
    assert.equal(await response.text(), 'Go rejected the action');
  }
});

test('only exposes the console API paths and their supported methods', async () => {
  globalThis.fetch = async () =>
    assert.fail('Unsupported requests must not reach Go');
  for (const path of [
    '/v1/ingest',
    '/v1/alarms/7/silence',
    '/v1/alarms/../rules',
    '/v1/alarms/0/events',
    '/healthz',
  ]) {
    assert.equal(
      (await proxyRequest(request(path, 'POST'))).status,
      path.endsWith('../rules') ? 405 : 404,
    );
  }
  assert.equal((await proxyRequest(request('/v1/rules', 'POST'))).status, 405);
  const response = await proxyRequest(request('/v1/alarms/7/clear'));
  assert.equal(response.status, 405);
  assert.equal(response.headers.get('allow'), 'POST');
});

test('reads the backend origin at runtime rather than fixing it in a build', async () => {
  const origins = [];
  globalThis.fetch = async (url) => {
    origins.push(url.origin);
    return Response.json([]);
  };
  process.env.LIMITWATCH_API_URL = 'http://backend-one:8080';
  await proxyRequest(request('/v1/alarms'));
  process.env.LIMITWATCH_API_URL = 'http://backend-two:9090';
  await proxyRequest(request('/v1/alarms'));
  assert.deepEqual(origins, [
    'http://backend-one:8080',
    'http://backend-two:9090',
  ]);
});

test('the console can report a stopped backend without leaking upstream errors', async () => {
  globalThis.fetch = async () => {
    throw new Error('private upstream details');
  };
  const response = await proxyRequest(request('/v1/alarms'));
  assert.equal(response.status, 502);
  assert.equal(await response.text(), 'Could not reach the LimitWatch API');
});

test('rejects invalid backend configuration without making a request', async () => {
  globalThis.fetch = async () =>
    assert.fail('Invalid configuration must not make a request');
  for (const origin of [
    'file:///tmp/data',
    'http://user:password@host',
    'http://host/base',
    'http://host?query=1',
    'not a URL',
  ]) {
    process.env.LIMITWATCH_API_URL = origin;
    assert.equal((await proxyRequest(request('/v1/rules'))).status, 503);
  }
});

test('does not follow redirects with bearer credentials', async () => {
  let calls = 0;
  globalThis.fetch = async () => {
    calls++;
    return new Response(null, {
      status: 302,
      headers: { Location: 'https://different-host.test' },
    });
  };
  assert.equal((await proxyRequest(request('/v1/alarms'))).status, 502);
  assert.equal(calls, 1);
});

test('forwards telemetry monitoring reads but keeps ingestion closed', async () => {
  globalThis.fetch = async (url, options) => {
    assert.equal(options.method, 'GET');
    assert.equal(options.headers.get('authorization'), 'Bearer test-token');
    assert.ok(url.pathname.startsWith('/v1/telemetry/'));
    if (url.pathname.endsWith('history')) {
      assert.equal(url.searchParams.get('name'), 'Sensor&A');
      assert.equal(url.searchParams.get('entity'), 'STAND-1');
    }
    return Response.json({});
  };
  assert.equal(
    (await proxyRequest(request('/v1/telemetry/streams'))).status,
    200,
  );
  assert.equal(
    (
      await proxyRequest(
        request('/v1/telemetry/history?name=Sensor%26A&entity=STAND-1'),
      )
    ).status,
    200,
  );
  assert.equal(
    (await proxyRequest(request('/v1/telemetry/streams', 'POST'))).status,
    405,
  );
  assert.equal((await proxyRequest(request('/v1/ingest', 'POST'))).status, 404);
});

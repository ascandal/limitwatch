// This module is used only by the server-side route handler. The browser cannot
// select an upstream host or access Go's JWT signing secret or database.
export async function proxyRequest(request: Request): Promise<Response> {
  const incoming = new URL(request.url);
  const path = incoming.pathname;
  const read =
    /^\/v1\/(rules|alarms)$/.test(path) ||
    /^\/v1\/alarms\/[1-9]\d*\/events$/.test(path);
  const write = /^\/v1\/alarms\/[1-9]\d*\/(ack|clear)$/.test(path);
  if (!read && !write) return new Response('Not found', { status: 404 });
  if (request.method !== (read ? 'GET' : 'POST')) {
    return new Response('Method not allowed', {
      status: 405,
      headers: { Allow: read ? 'GET' : 'POST' },
    });
  }

  let upstream: URL;
  try {
    // Read at request time so one build can target different Go deployments.
    upstream = new URL(
      process.env.LIMITWATCH_API_URL || 'http://127.0.0.1:8080',
    );
    if (
      !['http:', 'https:'].includes(upstream.protocol) ||
      upstream.username ||
      upstream.password ||
      upstream.pathname !== '/' ||
      upstream.search ||
      upstream.hash
    ) {
      throw new Error('Expected an HTTP(S) origin');
    }
  } catch {
    return new Response('LIMITWATCH_API_URL must be an HTTP(S) origin', {
      status: 503,
    });
  }
  upstream.pathname = path;
  upstream.search = incoming.search;
  const headers = new Headers({ Accept: 'application/json' });
  const authorization = request.headers.get('authorization');
  if (authorization) headers.set('Authorization', authorization);

  try {
    const response = await fetch(upstream, {
      method: request.method,
      headers,
      cache: 'no-store',
      redirect: 'manual',
      signal: AbortSignal.any([request.signal, AbortSignal.timeout(8000)]),
    });
    // Never send bearer credentials to a redirect destination or cache user data.
    if (response.status >= 300 && response.status < 400) {
      await response.body?.cancel();
      return new Response('LimitWatch returned an unexpected redirect', {
        status: 502,
      });
    }
    return new Response(response.body, {
      status: response.status,
      headers: {
        'Content-Type':
          response.headers.get('content-type') || 'text/plain; charset=utf-8',
        'Cache-Control': 'no-store',
      },
    });
  } catch {
    return new Response('Could not reach the LimitWatch API', { status: 502 });
  }
}

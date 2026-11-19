import { PHASE_DEVELOPMENT_SERVER } from 'next/constants.js';

export default function config(phase) {
  const common = {
    reactStrictMode: true,
    poweredByHeader: false,
    agentRules: false,
  };
  if (phase === PHASE_DEVELOPMENT_SERVER) {
    return {
      ...common,
      // Same-origin requests retain the bearer token; no browser CORS setup.
      async rewrites() {
        return [
          {
            source: '/v1/:path*',
            destination: 'http://127.0.0.1:8080/v1/:path*',
          },
        ];
      },
    };
  }
  return { ...common, output: 'export', trailingSlash: true };
}

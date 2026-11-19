import { proxyRequest } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

// Transport only: the Go service owns validation, roles, and alarm transitions.
export const GET = proxyRequest;
export const POST = proxyRequest;

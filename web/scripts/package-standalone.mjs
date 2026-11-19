import { cp, mkdir } from 'node:fs/promises';

// Next.js traces its server dependencies but leaves static assets separate.
await mkdir('.next/standalone/.next', { recursive: true });
await cp('.next/static', '.next/standalone/.next/static', { recursive: true });

import type { TelemetryBucket } from './api';

export function telemetrySummary(buckets: TelemetryBucket[]) {
  const count = buckets.reduce((sum, bucket) => sum + bucket.count, 0);
  return {
    count,
    min: buckets.length
      ? Math.min(...buckets.map((bucket) => bucket.min))
      : null,
    max: buckets.length
      ? Math.max(...buckets.map((bucket) => bucket.max))
      : null,
    mean: count
      ? buckets.reduce((sum, bucket) => sum + bucket.mean * bucket.count, 0) /
        count
      : null,
  };
}

// Time spacing is based on source timestamps, not array position. The min/max
// envelope retains excursions that a mean-only trace would hide.
export function telemetryGeometry(
  buckets: TelemetryBucket[],
  from: string,
  to: string,
) {
  const summary = telemetrySummary(buckets);
  const low = summary.min ?? 0;
  const high = summary.max ?? 1;
  const padding = (high - low || Math.abs(high) * 0.1 || 1) * 0.12;
  const bottom = low - padding;
  const top = high + padding;
  const start = Date.parse(from);
  const duration = Math.max(1, Date.parse(to) - start);
  const x = (bucket: TelemetryBucket) =>
    60 +
    (((Date.parse(bucket.start) + Date.parse(bucket.end)) / 2 - start) /
      duration) *
      820;
  const y = (value: number) => 245 - ((value - bottom) / (top - bottom)) * 220;
  return {
    line: buckets.map((bucket) => `${x(bucket)},${y(bucket.mean)}`).join(' '),
    band: [
      ...buckets.map((bucket) => `${x(bucket)},${y(bucket.max)}`),
      ...[...buckets]
        .reverse()
        .map((bucket) => `${x(bucket)},${y(bucket.min)}`),
    ].join(' '),
    points: buckets.map((bucket) => ({
      x: x(bucket),
      y: y(bucket.mean),
      low: y(bucket.min),
      high: y(bucket.max),
    })),
    ticks: [bottom, (bottom + top) / 2, top].map((value) => ({
      value,
      y: y(value),
    })),
  };
}

export function telemetryRange(
  period: string,
  frozenTo: string | null,
  custom: { from: string; to: string } | null,
  now: string,
) {
  if (period === 'custom' && custom) return custom;
  const to = frozenTo ?? now;
  return {
    from: new Date(Date.parse(to) - Number(period) * 60000).toISOString(),
    to,
  };
}

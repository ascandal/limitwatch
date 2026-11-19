import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  telemetryGeometry,
  telemetryRange,
  telemetrySummary,
} from './telemetry.ts';

function bucket(start, min, max, mean, count = 1) {
  return { start, end: start, min, max, mean, count };
}
const from = '2026-11-19T00:00:00Z';
const to = '2026-11-19T00:15:00Z';

test('summary weights bucket means by sample count and preserves extrema', () => {
  const summary = telemetrySummary([
    bucket(from, 0, 100, 10, 9),
    bucket(to, 20, 20, 20, 1),
  ]);
  assert.deepEqual(summary, { count: 10, min: 0, max: 100, mean: 11 });
});
test('chart spaces samples by source time and handles flat, single, and empty data', () => {
  const geometry = telemetryGeometry(
    [
      bucket(from, 1, 1, 1),
      bucket('2026-11-19T00:03:00Z', 1, 1, 1),
      bucket(to, 1, 1, 1),
    ],
    from,
    to,
  );
  const [first, middle, last] = geometry.points;
  assert.ok(Math.abs((middle.x - first.x) / (last.x - first.x) - 0.2) < 1e-9);
  for (const buckets of [
    [],
    [bucket(from, 0, 0, 0)],
    [bucket(from, -2, -2, -2)],
  ]) {
    const chart = telemetryGeometry(buckets, from, to);
    assert.ok(!chart.line.includes('NaN') && !chart.line.includes('Infinity'));
    assert.ok(chart.ticks.every((tick) => Number.isFinite(tick.y)));
  }
});
test('historical ranges remain frozen and custom ranges do not parse as durations', () => {
  for (const range of [
    telemetryRange('15', null, null, to),
    telemetryRange('15', to, null, '2026-11-20T00:00:00Z'),
  ]) {
    assert.equal(Date.parse(range.from), Date.parse(from));
    assert.equal(range.to, to);
  }
  const custom = { from: '2026-10-25T00:00:00Z', to: '2026-10-26T00:00:00Z' };
  assert.deepEqual(telemetryRange('custom', null, custom, to), custom);
});

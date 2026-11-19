'use client';

import { useCallback, useEffect, useState } from 'react';
import {
  Activity,
  AlertTriangle,
  ChevronDown,
  Pause,
  Play,
  RefreshCw,
  Search,
} from 'lucide-react';
import {
  errorMessage,
  getTelemetryHistory,
  getTelemetryStreams,
  type TelemetryHistory,
  type TelemetrySample,
} from '@/lib/api';
import {
  telemetryGeometry,
  telemetrySummary,
  telemetryRange,
} from '@/lib/telemetry';

function number(value: number | null) {
  return value === null
    ? '—'
    : value.toLocaleString(undefined, { maximumSignificantDigits: 6 });
}
function time(value: string) {
  return new Date(value).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}
function key(sample: TelemetrySample) {
  return JSON.stringify([sample.name, sample.entity]);
}
function age(value: string) {
  const seconds = Math.max(
    0,
    Math.floor((Date.now() - Date.parse(value)) / 1000),
  );
  return seconds < 60
    ? `${seconds}s ago`
    : seconds < 3600
      ? `${Math.floor(seconds / 60)}m ago`
      : `${Math.floor(seconds / 3600)}h ago`;
}

function usePolling<T>(
  read: (signal: AbortSignal) => Promise<T>,
  live: boolean,
) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [lastSuccess, setLastSuccess] = useState('');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      setLoading(true);
      try {
        const next = await read(controller.signal);
        if (controller.signal.aborted) return;
        setData(next);
        setError('');
        setLastSuccess(new Date().toISOString());
      } catch (error) {
        if (!controller.signal.aborted) setError(errorMessage(error));
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false);
          if (live) timer = setTimeout(poll, 2000);
        }
      }
    }
    timer = setTimeout(poll, 0);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [read, live, revision]);
  return {
    data,
    error,
    loading,
    lastSuccess,
    refresh: () => setRevision((value) => value + 1),
  };
}

export function Telemetry({ token }: { token: string }) {
  const [selection, setSelection] = useState('');
  const [query, setQuery] = useState('');
  const [period, setPeriod] = useState('15');
  const [frozenTo, setFrozenTo] = useState<string | null>(null);
  const [customFrom, setCustomFrom] = useState('');
  const [customTo, setCustomTo] = useState('');
  const [custom, setCustom] = useState<{ from: string; to: string } | null>(
    null,
  );
  const [rangeError, setRangeError] = useState('');
  const live = !frozenTo && period !== 'custom';
  const read = useCallback(
    (signal: AbortSignal) => getTelemetryStreams(token, signal),
    [token],
  );
  const catalog = usePolling(read, live);
  const streams = catalog.data?.streams ?? [];
  const selected =
    streams.find((sample) => key(sample) === selection) ?? streams[0];
  const visible = streams.filter((sample) =>
    `${sample.name} ${sample.entity}`
      .toLowerCase()
      .includes(query.toLowerCase()),
  );
  return (
    <main id="main" className="workspace telemetry-workspace">
      <div className="page-heading">
        <div>
          <div className="eyebrow">OPERATIONS / TELEMETRY</div>
          <h1>Telemetry monitor</h1>
          <p>Live values and retained history, across signals and entities.</p>
        </div>
        <div className="refresh-tools">
          <div className="connection">
            <span
              className={`connection-dot ${catalog.error ? 'offline' : !catalog.lastSuccess ? 'waiting' : ''}`}
            />
            {catalog.error
              ? 'Refresh failed'
              : live
                ? 'Live · every 2 seconds'
                : 'History · refresh paused'}
          </div>
          <span className="refresh-time">
            Last refresh:{' '}
            {catalog.lastSuccess ? time(catalog.lastSuccess) : 'Connecting'}
          </span>
          <div className="refresh-buttons">
            <button
              className="button"
              onClick={() => {
                setPeriod(period === 'custom' ? '15' : period);
                setFrozenTo(live ? new Date().toISOString() : null);
              }}
            >
              {live ? <Pause size={15} /> : <Play size={15} />}
              {live ? 'Pause' : 'Go live'}
            </button>
            <button
              className="button"
              disabled={catalog.loading}
              onClick={catalog.refresh}
            >
              <RefreshCw
                size={15}
                className={catalog.loading ? 'spinning' : ''}
              />
              Refresh streams
            </button>
          </div>
        </div>
      </div>
      {catalog.error && (
        <div className="notice error" role="alert">
          <AlertTriangle size={18} />
          <div>
            <strong>{catalog.error}</strong>
            <span>Previously loaded values may be stale.</span>
          </div>
        </div>
      )}
      <div className="telemetry-layout">
        <section
          className="panel telemetry-streams"
          aria-label="Telemetry streams"
        >
          <div className="panel-heading">
            <div>
              <h2>
                Signals <span className="count">{streams.length}</span>
              </h2>
              <p>Latest received sample for each signal and entity</p>
            </div>
          </div>
          <div className="list-tools">
            <label className="search">
              <Search size={17} />
              <input
                aria-label="Search telemetry"
                placeholder="Search signal or entity"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
              />
            </label>
          </div>
          <div className="telemetry-stream-scroll">
            <table>
              <thead>
                <tr>
                  <th>Signal / entity</th>
                  <th>Value</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((sample) => (
                  <tr
                    className={
                      selected && key(selected) === key(sample)
                        ? 'selected-row'
                        : ''
                    }
                    key={key(sample)}
                  >
                    <td>
                      <button
                        className="alarm-link"
                        aria-pressed={selected && key(selected) === key(sample)}
                        onClick={() => setSelection(key(sample))}
                      >
                        <strong>{sample.name}</strong>
                        <span>{sample.entity}</span>
                      </button>
                    </td>
                    <td className="telemetry-value">
                      <strong>{number(sample.value)}</strong>
                      <span title={`Received ${time(sample.received_at)}`}>
                        {age(sample.received_at)}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {!visible.length && (
            <div className="empty-state">
              <Activity size={28} />
              <h3>
                {catalog.loading
                  ? 'Loading signals'
                  : catalog.error && !catalog.data
                    ? 'Telemetry unavailable'
                    : streams.length
                      ? 'No matching signals'
                      : 'No telemetry recorded yet'}
              </h3>
              <p>New samples appear here as telemetry is ingested.</p>
            </div>
          )}
          {catalog.data?.has_more && (
            <div className="panel-footnote">
              Showing the 200 most recently received streams.
            </div>
          )}
        </section>
        <section
          className="panel telemetry-series"
          aria-label="Telemetry history"
        >
          <div className="panel-heading">
            <div>
              <h2>{selected?.name ?? 'Select a signal'}</h2>
              <p>{selected?.entity ?? 'Ingest telemetry to see its history'}</p>
            </div>
            <div className="select-wrap">
              <select
                aria-label="Telemetry time range"
                value={period}
                onChange={(event) => {
                  setPeriod(event.target.value);
                  setRangeError('');
                }}
              >
                <option value="5">Last 5 minutes</option>
                <option value="15">Last 15 minutes</option>
                <option value="60">Last hour</option>
                <option value="1440">Last 24 hours</option>
                <option value="custom">Custom range</option>
              </select>
              <ChevronDown size={15} />
            </div>
          </div>
          {period === 'custom' && (
            <form
              className="telemetry-range"
              onSubmit={(event) => {
                event.preventDefault();
                const from = new Date(customFrom);
                const to = new Date(customTo);
                const duration = to.getTime() - from.getTime();
                if (
                  !Number.isFinite(duration) ||
                  duration <= 0 ||
                  duration > 31 * 86400000
                ) {
                  setRangeError('Choose a positive time range up to 31 days.');
                  return;
                }
                setCustom({ from: from.toISOString(), to: to.toISOString() });
                setRangeError('');
              }}
            >
              <label>
                From · local time
                <input
                  aria-label="Telemetry from"
                  type="datetime-local"
                  step="1"
                  value={customFrom}
                  onChange={(event) => setCustomFrom(event.target.value)}
                  required
                />
              </label>
              <label>
                To · local time
                <input
                  aria-label="Telemetry to"
                  type="datetime-local"
                  step="1"
                  value={customTo}
                  onChange={(event) => setCustomTo(event.target.value)}
                  required
                />
              </label>
              <button className="button" type="submit">
                Apply range
              </button>
            </form>
          )}
          {rangeError && (
            <p className="action-error" role="alert">
              {rangeError}
            </p>
          )}
          {selected && (
            <div className="telemetry-latest">
              <div>
                <span className="field-label">LATEST RECEIVED VALUE</span>
                <strong>{number(selected.value)}</strong>
              </div>
              <dl>
                <div>
                  <dt>Source time</dt>
                  <dd>{time(selected.ts)}</dd>
                </div>
                <div>
                  <dt>Received</dt>
                  <dd>{time(selected.received_at)}</dd>
                </div>
              </dl>
            </div>
          )}
          {selected && (period !== 'custom' || custom) ? (
            <Series
              key={`${key(selected)}:${period}:${JSON.stringify(custom)}`}
              name={selected.name}
              entity={selected.entity}
              period={period}
              custom={custom}
              frozenTo={frozenTo}
              live={live}
              token={token}
            />
          ) : (
            <div className="empty-state">
              <h3>
                {period === 'custom'
                  ? 'Choose a time range'
                  : 'Your signal history'}
              </h3>
              <p>
                Source time drives the chart. Reception time shows when
                LimitWatch received a sample.
              </p>
            </div>
          )}
        </section>
      </div>
      <p className="telemetry-retention">
        Up to 100,000 recent samples retained across all streams. History
        survives restart; older samples roll off as new ones arrive.
      </p>
    </main>
  );
}

function Series({
  name,
  entity,
  period,
  custom,
  frozenTo,
  live,
  token,
}: {
  name: string;
  entity: string;
  period: string;
  custom: { from: string; to: string } | null;
  frozenTo: string | null;
  live: boolean;
  token: string;
}) {
  const read = useCallback(
    (signal: AbortSignal) => {
      const range = telemetryRange(
        period,
        frozenTo,
        custom,
        new Date().toISOString(),
      );
      return getTelemetryHistory(
        name,
        entity,
        range.from,
        range.to,
        token,
        signal,
      );
    },
    [name, entity, period, custom, frozenTo, token],
  );
  const history = usePolling(read, live);
  const summary = telemetrySummary(history.data?.buckets ?? []);
  return (
    <div className="telemetry-chart-content">
      <div className="telemetry-chart-tools">
        <span>
          {history.data
            ? `${time(history.data.from)} – ${time(history.data.to)}`
            : 'Loading history…'}{' '}
          · source time
        </span>
        <button
          className="text-button"
          disabled={history.loading}
          onClick={history.refresh}
        >
          Refresh chart
        </button>
      </div>
      {history.error && (
        <p className="action-error" role="alert">
          {history.error} The displayed chart may be stale.
        </p>
      )}
      {history.data?.buckets.length ? (
        <>
          <Trend history={history.data} />
          <div className="telemetry-stats">
            <div>
              <span>Samples in range</span>
              <strong>{summary.count.toLocaleString()}</strong>
            </div>
            <div>
              <span>Minimum</span>
              <strong>{number(summary.min)}</strong>
            </div>
            <div>
              <span>Mean</span>
              <strong>{number(summary.mean)}</strong>
            </div>
            <div>
              <span>Maximum</span>
              <strong>{number(summary.max)}</strong>
            </div>
          </div>
          <details className="telemetry-table">
            <summary>
              Inspect {history.data.buckets.length} time buckets
            </summary>
            <div className="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>Source time</th>
                    <th>Count</th>
                    <th>Min</th>
                    <th>Mean</th>
                    <th>Max</th>
                  </tr>
                </thead>
                <tbody>
                  {[...history.data.buckets].reverse().map((bucket) => (
                    <tr key={bucket.start}>
                      <td title={`Until ${time(bucket.end)}`}>
                        {time(bucket.start)}
                      </td>
                      <td>{bucket.count}</td>
                      <td>{number(bucket.min)}</td>
                      <td>{number(bucket.mean)}</td>
                      <td>{number(bucket.max)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </details>
        </>
      ) : (
        <div className="empty-state">
          <Activity size={28} />
          <h3>
            {history.loading
              ? 'Loading history'
              : history.error && !history.data
                ? 'History unavailable'
                : 'No retained samples in this range'}
          </h3>
          <p>
            Try a wider range or ingest new data. Older data can be outside the
            retention limit.
          </p>
        </div>
      )}
    </div>
  );
}

function Trend({ history }: { history: TelemetryHistory }) {
  const geometry = telemetryGeometry(history.buckets, history.from, history.to);
  const last = geometry.points.at(-1);
  return (
    <>
      <div className="telemetry-legend">
        <span className="mean-key" />
        Mean
        <span className="range-key" />
        Min–max range
      </div>
      <svg
        className="telemetry-chart"
        viewBox="0 0 900 285"
        role="img"
        aria-label="Telemetry over source time. The line shows bucket means and shading shows minimum to maximum. Exact values are available in the time bucket table."
      >
        {geometry.ticks.map((tick) => (
          <g key={tick.y}>
            <line
              x1="60"
              x2="880"
              y1={tick.y}
              y2={tick.y}
              className="chart-grid"
            />
            <text x="50" y={tick.y + 5} textAnchor="end">
              {number(tick.value)}
            </text>
          </g>
        ))}
        <polygon points={geometry.band} className="chart-band" />
        <polyline points={geometry.line} className="chart-line" />
        {geometry.points.length === 1 && last && (
          <line
            x1={last.x}
            x2={last.x}
            y1={last.low}
            y2={last.high}
            className="chart-line"
          />
        )}
        {last && (
          <circle cx={last.x} cy={last.y} r="4" className="chart-last" />
        )}
        <text x="60" y="275">
          {new Date(history.from).toLocaleTimeString()}
        </text>
        <text x="880" y="275" textAnchor="end">
          {new Date(history.to).toLocaleTimeString()}
        </text>
      </svg>
    </>
  );
}

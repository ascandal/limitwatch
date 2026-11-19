'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  Activity,
  AlertTriangle,
  Check,
  CheckCheck,
  ChevronDown,
  CircleHelp,
  Clock3,
  KeyRound,
  Radio,
  RefreshCw,
  Search,
  Settings2,
  ShieldCheck,
  X,
} from 'lucide-react';
import {
  actOnAlarm,
  errorMessage,
  getEvents,
  getSnapshot,
  type Alarm,
  type AlarmEvent,
  type AlarmState,
  type Rule,
} from '@/lib/api';

const stateNames: Record<AlarmState, string> = {
  active: 'Active',
  acked: 'Acknowledged',
  silenced: 'Silenced',
  inactive: 'Inactive',
};
const eventNames: Record<string, string> = {
  activated: 'Alarm activated',
  acked: 'Acknowledged',
  cleared: 'Alarm cleared',
  silenced: 'Alarm silenced',
  note: 'Note added',
};
const severityNames = { info: 'Info', warn: 'Warning', crit: 'Critical' };

function timestamp(value: string | null): string {
  if (!value || new Date(value).getFullYear() < 1970) return 'Not recorded';
  return new Date(value).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}
function StateBadge({ state }: { state: AlarmState }) {
  return (
    <span className={`badge state-${state}`}>
      <span className="badge-dot" />
      {stateNames[state]}
    </span>
  );
}
function RuleSummary({ rule }: { rule: Rule }) {
  const config = rule.config ?? {};
  let summary = 'See configuration';
  if (rule.type === 'threshold') {
    summary =
      [
        config.lo != null && `Below ${config.lo}`,
        config.hi != null && `Above ${config.hi}`,
      ]
        .filter(Boolean)
        .join(' or ') || summary;
  } else if (rule.type === 'duration') {
    summary = `${config.op ?? '>'} ${config.threshold ?? '—'}`;
  } else {
    summary = `${config.abs ? 'Absolute rate' : 'Rate'} > ${config.gt ?? '—'} / sec · ${config.window_ms ?? 0} ms window`;
  }
  return (
    <div className="rule-summary">
      <strong>{summary}</strong>
      <span>
        {Number(config.duration_ms) > 0
          ? `${config.duration_ms} ms duration`
          : 'No duration delay'}
        {Number(config.hysteresis) > 0
          ? ` · ${config.hysteresis} hysteresis`
          : ''}
      </span>
    </div>
  );
}

// Polls sequentially, cancels obsolete reads, and retains the last snapshot on
// network failure. Credentials and cached data are scoped to this mounted view.
function useSnapshot(token: string) {
  const [data, setData] = useState<{ alarms: Alarm[]; rules: Rule[] }>({
    alarms: [],
    rules: [],
  });
  const [lastSuccess, setLastSuccess] = useState<string | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [paused, setPaused] = useState(false);
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      setLoading(true);
      try {
        const snapshot = await getSnapshot(token, controller.signal);
        if (controller.signal.aborted) return;
        setData(snapshot);
        setLastSuccess(new Date().toISOString());
        setError('');
      } catch (error) {
        if (!controller.signal.aborted) setError(errorMessage(error));
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false);
          if (!paused) timer = setTimeout(poll, 5000);
        }
      }
    }
    void poll();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [token, revision, paused]);
  return { ...data, lastSuccess, error, loading, paused, setPaused, refresh };
}

export function Console() {
  const [token, setToken] = useState('');
  const [draftToken, setDraftToken] = useState('');
  const [tokenOpen, setTokenOpen] = useState(false);
  const [tab, setTab] = useState<'alarms' | 'rules'>('alarms');
  return (
    <>
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark">
            <Activity size={23} strokeWidth={2.3} />
          </span>
          <span>
            Limit<span className="brand-light">Watch</span>
          </span>
          <span className="product-label">OPERATOR CONSOLE</span>
        </div>
        <nav aria-label="Console navigation">
          <button
            className={tab === 'alarms' ? 'nav-button selected' : 'nav-button'}
            onClick={() => setTab('alarms')}
          >
            <Radio size={17} />
            Alarms
          </button>
          <button
            className={tab === 'rules' ? 'nav-button selected' : 'nav-button'}
            onClick={() => setTab('rules')}
          >
            <Settings2 size={17} />
            Rules
          </button>
        </nav>
        <button
          className="access-button"
          aria-expanded={tokenOpen}
          onClick={() => setTokenOpen(!tokenOpen)}
        >
          <KeyRound size={17} />
          <span>{token ? 'Token connected' : 'Access token'}</span>
          <ChevronDown size={15} />
        </button>
      </header>
      {tokenOpen && (
        <section className="access-panel" aria-label="API access">
          <div>
            <strong>Connect to LimitWatch</strong>
            <p>
              Enter a bearer token when authentication is enabled. It stays in
              memory and is cleared when you reload.
            </p>
          </div>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              setToken(draftToken.trim().replace(/^Bearer\s+/i, ''));
              setDraftToken('');
              setTokenOpen(false);
            }}
          >
            <label htmlFor="access-token">Access token</label>
            <div className="access-fields">
              <input
                id="access-token"
                autoComplete="off"
                type="password"
                value={draftToken}
                placeholder="Paste bearer token"
                onChange={(event) => setDraftToken(event.target.value)}
                required
              />
              <button className="button primary" type="submit">
                Connect
              </button>
              {token && (
                <button
                  className="button"
                  type="button"
                  onClick={() => {
                    setToken('');
                    setDraftToken('');
                  }}
                >
                  Remove token
                </button>
              )}
            </div>
          </form>
        </section>
      )}
      <Dashboard key={token} token={token} tab={tab} />
      <footer className="footer">
        <span>
          <Activity size={14} />
          LimitWatch
        </span>
        <span>Observe the condition. Own the response.</span>
      </footer>
    </>
  );
}

function Dashboard({ token, tab }: { token: string; tab: 'alarms' | 'rules' }) {
  const snapshot = useSnapshot(token);
  const {
    alarms,
    rules,
    lastSuccess,
    error,
    loading,
    paused,
    setPaused,
    refresh,
  } = snapshot;
  const [selection, setSelection] = useState<number | null>(null);
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState('all');
  const byRule = new Map(rules.map((rule) => [rule.id, rule]));
  const selected =
    alarms.find((alarm) => alarm.id === selection) ??
    (selection === null ? alarms[0] : undefined);
  const visible = alarms.filter(
    (alarm) =>
      (filter === 'all' || alarm.state === filter) &&
      `${alarm.entity} ${byRule.get(alarm.rule_id)?.name ?? ''} ${byRule.get(alarm.rule_id)?.signal ?? ''}`
        .toLowerCase()
        .includes(query.toLowerCase()),
  );
  return (
    <main id="main" className="workspace">
      <div className="page-heading">
        <div>
          <div className="eyebrow">OPERATIONS / {tab.toUpperCase()}</div>
          <h1>{tab === 'alarms' ? 'Alarm console' : 'Rule definitions'}</h1>
          <p>
            {tab === 'alarms'
              ? 'See what needs attention. Follow every response.'
              : 'The conditions behind your alarms, in one place.'}
          </p>
        </div>
        <div className="refresh-tools">
          <div className="connection">
            <span
              className={`connection-dot ${error ? 'offline' : !lastSuccess ? 'waiting' : ''}`}
            />
            <span>
              {error
                ? 'Refresh failed'
                : !lastSuccess
                  ? 'Connecting'
                  : paused
                    ? 'Refresh paused'
                    : 'Connected'}
            </span>
          </div>
          <span className="refresh-time">
            Last refresh: {lastSuccess ? timestamp(lastSuccess) : 'Not yet'}
          </span>
          <div className="refresh-buttons">
            <button className="text-button" onClick={() => setPaused(!paused)}>
              {paused ? 'Resume refresh' : 'Pause refresh'}
            </button>
            <button className="button" onClick={refresh} disabled={loading}>
              <RefreshCw size={15} className={loading ? 'spinning' : ''} />
              Refresh
            </button>
          </div>
        </div>
      </div>
      {error && (
        <div className="notice error" role="alert">
          <AlertTriangle size={19} />
          <div>
            <strong>{error}</strong>
            <span>
              {lastSuccess
                ? 'Showing the last successful snapshot. Alarm actions are disabled until the connection recovers.'
                : 'No alarm data has been loaded.'}
            </span>
          </div>
        </div>
      )}
      {paused && !error && (
        <div className="notice">
          <Clock3 size={18} />
          <span>
            Automatic refresh is paused. Use Refresh to check the current alarm
            state.
          </span>
        </div>
      )}
      <section className="metrics" aria-label="Current snapshot totals">
        <div className="metric">
          <span className="metric-icon amber">
            <AlertTriangle size={20} />
          </span>
          <div>
            <span>Active alarms</span>
            <strong>
              {lastSuccess
                ? alarms.filter((a) => a.state === 'active').length
                : '—'}
            </strong>
          </div>
          <span className="metric-note">Need attention</span>
        </div>
        <div className="metric">
          <span className="metric-icon blue">
            <CheckCheck size={20} />
          </span>
          <div>
            <span>Acknowledged</span>
            <strong>
              {lastSuccess
                ? alarms.filter((a) => a.state === 'acked').length
                : '—'}
            </strong>
          </div>
          <span className="metric-note">Response recorded</span>
        </div>
        <div className="metric">
          <span className="metric-icon teal">
            <ShieldCheck size={20} />
          </span>
          <div>
            <span>Enabled rules</span>
            <strong>
              {lastSuccess ? rules.filter((rule) => rule.enabled).length : '—'}
            </strong>
          </div>
          <span className="metric-note">{rules.length} definitions</span>
        </div>
      </section>
      {tab === 'alarms' ? (
        <div className="alarm-layout">
          <section className="panel alarm-list" aria-label="Alarms">
            <div className="panel-heading">
              <div>
                <h2>
                  Alarms <span className="count">{alarms.length}</span>
                </h2>
                <p>Latest 100 alarm records · counts reflect this snapshot</p>
              </div>
            </div>
            <div className="list-tools">
              <label className="search">
                <Search size={17} />
                <input
                  aria-label="Search alarms"
                  placeholder="Search entity, rule, or signal"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                {query && (
                  <button
                    aria-label="Clear search"
                    onClick={() => setQuery('')}
                  >
                    <X size={15} />
                  </button>
                )}
              </label>
              <div className="select-wrap">
                <select
                  aria-label="Filter alarm state"
                  value={filter}
                  onChange={(event) => setFilter(event.target.value)}
                >
                  <option value="all">All states</option>
                  {Object.entries(stateNames).map(([value, label]) => (
                    <option key={value} value={value}>
                      {label}
                    </option>
                  ))}
                </select>
                <ChevronDown size={15} />
              </div>
            </div>
            {visible.length ? (
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>Rule / entity</th>
                      <th>Severity</th>
                      <th>State</th>
                      <th className="time-cell">Updated</th>
                    </tr>
                  </thead>
                  <tbody>
                    {visible.map((alarm) => {
                      const rule = byRule.get(alarm.rule_id);
                      return (
                        <tr
                          key={alarm.id}
                          className={
                            selected?.id === alarm.id ? 'selected-row' : ''
                          }
                        >
                          <td>
                            <button
                              className="alarm-link"
                              aria-pressed={selected?.id === alarm.id}
                              onClick={() => setSelection(alarm.id)}
                            >
                              <strong>
                                {rule?.name ?? `Rule ${alarm.rule_id}`}
                              </strong>
                              <span>{alarm.entity}</span>
                            </button>
                          </td>
                          <td>
                            <span
                              className={`severity severity-${rule?.severity ?? 'info'}`}
                            >
                              {rule ? severityNames[rule.severity] : 'Unknown'}
                            </span>
                          </td>
                          <td>
                            <StateBadge state={alarm.state} />
                          </td>
                          <td className="time-cell">
                            {timestamp(alarm.updated_at)}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="empty-state">
                <Radio size={30} />
                <h3>
                  {loading && !lastSuccess
                    ? 'Loading alarms'
                    : error && !lastSuccess
                      ? 'Waiting for a connection'
                      : alarms.length
                        ? 'No matching alarms'
                        : 'No alarms yet'}
                </h3>
                <p>
                  {alarms.length
                    ? 'Try a different search or state filter.'
                    : 'Alarms appear when an enabled rule detects an out-of-limits condition.'}
                </p>
              </div>
            )}
            <div className="panel-footnote">
              <CircleHelp size={15} />
              Select an alarm to inspect its response and history.
            </div>
          </section>
          <AlarmDetail
            key={selected?.id ?? 'none'}
            alarm={selected}
            rule={selected ? byRule.get(selected.rule_id) : undefined}
            token={token}
            disabled={!!error || !lastSuccess || paused}
            onChange={refresh}
          />
        </div>
      ) : (
        <section className="panel rules-panel" aria-label="Rule definitions">
          <div className="panel-heading">
            <div>
              <h2>
                Configured rules <span className="count">{rules.length}</span>
              </h2>
              <p>
                Read-only · new or changed definitions require a service restart
                to activate.
              </p>
            </div>
          </div>
          {rules.length ? (
            <div className="rule-grid">
              {rules.map((rule) => (
                <article className="rule-card" key={rule.id}>
                  <div className="rule-card-top">
                    <span className={`severity severity-${rule.severity}`}>
                      {severityNames[rule.severity]}
                    </span>
                    <span
                      className={`rule-enabled ${!rule.enabled ? 'muted' : ''}`}
                    >
                      {rule.enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </div>
                  <h3>{rule.name}</h3>
                  <code>{rule.signal}</code>
                  <RuleSummary rule={rule} />
                  <div className="rule-card-meta">
                    <span>{rule.type}</span>
                    <span>{rule.latch ? 'Latched' : 'Auto-clear'}</span>
                    <span>v{rule.version}</span>
                  </div>
                  <details>
                    <summary>Full configuration</summary>
                    <pre>{JSON.stringify(rule.config, null, 2)}</pre>
                  </details>
                </article>
              ))}
            </div>
          ) : (
            <div className="empty-state">
              <Settings2 size={30} />
              <h3>No rules loaded</h3>
              <p>
                {error ||
                  'Seed a rule before starting the service, or create it through the API and restart.'}
              </p>
            </div>
          )}
        </section>
      )}
    </main>
  );
}

function AlarmDetail({
  alarm,
  rule,
  token,
  disabled,
  onChange,
}: {
  alarm?: Alarm;
  rule?: Rule;
  token: string;
  disabled: boolean;
  onChange: () => void;
}) {
  const [events, setEvents] = useState<AlarmEvent[]>([]);
  const [cursor, setCursor] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState('');
  const [pending, setPending] = useState<'ack' | 'clear' | null>(null);
  const [actionMessage, setActionMessage] = useState('');
  const [actionError, setActionError] = useState('');
  const historyRequest = useRef<AbortController | null>(null);
  const actionRequest = useRef<AbortController | null>(null);
  const busy = useRef(false);
  const id = alarm?.id;
  const loadHistory = useCallback(
    async (after = 0, replace = true) => {
      if (!id) return;
      historyRequest.current?.abort();
      const controller = new AbortController();
      historyRequest.current = controller;
      setHistoryLoading(true);
      try {
        const page = await getEvents(id, after, token, controller.signal);
        if (controller.signal.aborted) return;
        setEvents((current) =>
          replace
            ? page.events
            : [
                ...current,
                ...page.events.filter(
                  (item) => !current.some((old) => old.id === item.id),
                ),
              ],
        );
        setCursor(page.next_after_id);
        setHasMore(page.has_more);
        setHistoryError('');
      } catch (error) {
        if (!controller.signal.aborted) setHistoryError(errorMessage(error));
      } finally {
        if (!controller.signal.aborted) setHistoryLoading(false);
      }
    },
    [id, token],
  );
  useEffect(() => {
    // Reset to the first page after a state change; explicit pagination is stable
    // by event ID, even when the source clock is earlier than an operator action.
    const timer = setTimeout(() => void loadHistory(), 0);
    return () => {
      clearTimeout(timer);
      historyRequest.current?.abort();
    };
  }, [loadHistory, alarm?.state]);
  useEffect(() => () => actionRequest.current?.abort(), []);
  async function act(action: 'ack' | 'clear') {
    if (!alarm || busy.current) return;
    busy.current = true;
    const controller = new AbortController();
    actionRequest.current = controller;
    setPending(action);
    setActionMessage('');
    setActionError('');
    try {
      await actOnAlarm(alarm.id, action, token, controller.signal);
      const updated = await getSnapshot(token, controller.signal);
      if (controller.signal.aborted) return;
      const current = updated.alarms.find((item) => item.id === alarm.id);
      setActionMessage(
        action === 'ack'
          ? current?.state === 'acked'
            ? 'Acknowledgment saved and confirmed.'
            : 'Acknowledgment saved. The alarm state has changed since the action.'
          : current?.state === 'inactive'
            ? 'Alarm cleared and confirmed. A new violating sample can activate it again.'
            : 'Clear saved. The alarm state has changed since the action.',
      );
      onChange();
      await loadHistory();
    } catch (error) {
      if (!controller.signal.aborted) {
        setActionError(errorMessage(error));
        onChange();
      }
    } finally {
      busy.current = false;
      if (!controller.signal.aborted) setPending(null);
    }
  }
  if (!alarm)
    return (
      <aside className="panel detail-panel empty-detail">
        <span className="detail-icon">
          <Activity size={26} />
        </span>
        <h2>Your alarm, explained</h2>
        <p>
          Select an alarm to see its rule, operator response, and event history.
        </p>
      </aside>
    );
  return (
    <aside className="panel detail-panel" aria-label="Alarm detail">
      <div className="detail-heading">
        <div className="eyebrow">
          ALARM DETAIL <span>#{alarm.id}</span>
        </div>
        <StateBadge state={alarm.state} />
        <h2>{rule?.name ?? `Rule ${alarm.rule_id}`}</h2>
        <p>
          {alarm.entity}
          <span className="separator">/</span>
          <code>{rule?.signal ?? 'Unknown signal'}</code>
        </p>
      </div>
      <div className="detail-content">
        {rule && (
          <div className="condition-box">
            <span className="field-label">RULE CONDITION</span>
            <RuleSummary rule={rule} />
            <div className="condition-meta">
              <span className={`severity severity-${rule.severity}`}>
                {severityNames[rule.severity]}
              </span>
              <span>
                {rule.latch
                  ? 'Latched · operator clears'
                  : 'Clears when condition recovers'}
              </span>
            </div>
          </div>
        )}
        <dl className="detail-facts">
          <div>
            <dt>First active · source time</dt>
            <dd>{timestamp(alarm.first_seen)}</dd>
          </div>
          <div>
            <dt>Last active · source time</dt>
            <dd>{timestamp(alarm.last_seen)}</dd>
          </div>
          <div>
            <dt>Acknowledged by</dt>
            <dd>{alarm.acked_by ?? 'Not acknowledged'}</dd>
          </div>
        </dl>
        <div className="alarm-actions">
          <button
            className="button primary"
            onClick={() => void act('ack')}
            disabled={
              disabled ||
              !!pending ||
              alarm.state === 'acked' ||
              alarm.state === 'inactive'
            }
          >
            <Check size={16} />
            {pending === 'ack' ? 'Saving…' : 'Acknowledge'}
          </button>
          <button
            className="button"
            onClick={() => void act('clear')}
            disabled={disabled || !!pending || alarm.state === 'inactive'}
          >
            <CheckCheck size={16} />
            {pending === 'clear' ? 'Clearing…' : 'Clear alarm'}
          </button>
        </div>
        <p className="action-help">
          Acknowledging records your response. Clearing does not change the
          source condition.
        </p>
        {actionMessage && (
          <p className="action-success" role="status">
            {actionMessage}
          </p>
        )}
        {actionError && (
          <p className="action-error" role="alert">
            {actionError}
          </p>
        )}
      </div>
      <section className="history" aria-label="Alarm history">
        <div className="history-heading">
          <h3>
            <Clock3 size={17} />
            Event history
          </h3>
          <button
            className="text-button"
            disabled={historyLoading}
            onClick={() => void loadHistory()}
          >
            Refresh history
          </button>
        </div>
        <p className="history-caption">
          Event ID order · times shown in your local time zone
        </p>
        {historyError && (
          <p className="action-error" role="alert">
            History could not refresh. {historyError} Previously loaded events
            may be stale.
          </p>
        )}
        {events.length ? (
          <ol className="timeline">
            {events.map((event) => (
              <li key={event.id} className={`event-${event.type}`}>
                <span className="event-marker">
                  {event.type === 'activated' ? (
                    <AlertTriangle size={13} />
                  ) : (
                    <Check size={13} />
                  )}
                </span>
                <div>
                  <div className="event-title">
                    <strong>{eventNames[event.type] ?? event.type}</strong>
                    <span>#{event.id}</span>
                  </div>
                  <p>
                    {event.actor || 'Unknown actor'}
                    <span className="separator">·</span>
                    {event.actor === 'engine' ? 'Source time' : 'Action time'}
                  </p>
                  <time dateTime={event.at}>{timestamp(event.at)}</time>
                  {event.note && <p className="event-note">{event.note}</p>}
                </div>
              </li>
            ))}
          </ol>
        ) : (
          <p className="history-empty">
            {historyLoading ? 'Loading history…' : 'No events recorded.'}
          </p>
        )}
        {hasMore && (
          <button
            className="button more-events"
            disabled={historyLoading}
            onClick={() => void loadHistory(cursor, false)}
          >
            {historyLoading ? 'Loading…' : 'Load more events'}
          </button>
        )}
      </section>
    </aside>
  );
}

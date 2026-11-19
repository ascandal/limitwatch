export type AlarmState = 'active' | 'acked' | 'silenced' | 'inactive';
export interface Alarm {
  id: number;
  rule_id: number;
  entity: string;
  state: AlarmState;
  first_seen: string | null;
  last_seen: string | null;
  acked_by: string | null;
  silence_until: string | null;
  updated_at: string;
}
export interface Rule {
  id: number;
  name: string;
  signal: string;
  type: 'threshold' | 'duration' | 'roc';
  severity: 'info' | 'warn' | 'crit';
  latch: boolean;
  enabled: boolean;
  config: Record<string, unknown>;
  version: number;
}
export interface AlarmEvent {
  id: number;
  alarm_id: number;
  type: 'activated' | 'acked' | 'silenced' | 'cleared' | 'note';
  actor: string;
  at: string;
  note: string;
}
export interface EventPage {
  events: AlarmEvent[];
  next_after_id: number;
  has_more: boolean;
}
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 401)
      return 'Authentication required. Add a valid access token to connect.';
    if (error.status === 403)
      return 'Permission denied. An operator or admin token is required for this action.';
    if (error.status === 409)
      return 'The alarm state changed. Refresh and try again.';
    return `Request failed (${error.status}): ${error.message}`;
  }
  return 'Could not reach LimitWatch. Check the service and try again.';
}
async function request<T>(
  path: string,
  token: string,
  signal?: AbortSignal,
  method = 'GET',
): Promise<T> {
  // Timeout applies even if a caller supplies its own cancellation signal.
  const timeout = AbortSignal.timeout(10000);
  const response = await fetch(path, {
    method,
    cache: 'no-store',
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
  });
  if (!response.ok)
    throw new ApiError(response.status, (await response.text()).trim());
  return response.json() as Promise<T>;
}
export async function getSnapshot(token: string, signal?: AbortSignal) {
  const [alarms, rules] = await Promise.all([
    request<Alarm[] | null>('/v1/alarms?limit=100', token, signal),
    request<Rule[] | null>('/v1/rules', token, signal),
  ]);
  return { alarms: alarms ?? [], rules: rules ?? [] };
}
export function getEvents(
  id: number,
  after: number,
  token: string,
  signal?: AbortSignal,
) {
  return request<EventPage>(
    `/v1/alarms/${id}/events?after_id=${after}&limit=50`,
    token,
    signal,
  );
}
export function actOnAlarm(
  id: number,
  action: 'ack' | 'clear',
  token: string,
  signal?: AbortSignal,
) {
  return request<{ status: string }>(
    `/v1/alarms/${id}/${action}`,
    token,
    signal,
    'POST',
  );
}

export interface TelemetrySample {
  id: number;
  name: string;
  entity: string;
  value: number;
  ts: string;
  received_at: string;
}
export interface TelemetryBucket {
  start: string;
  end: string;
  min: number;
  max: number;
  mean: number;
  count: number;
}
export interface TelemetryHistory {
  buckets: TelemetryBucket[];
  from: string;
  to: string;
  retention_samples: number;
}
export function getTelemetryStreams(token: string, signal?: AbortSignal) {
  return request<{
    streams: TelemetrySample[];
    has_more: boolean;
    retention_samples: number;
  }>('/v1/telemetry/streams', token, signal);
}
export function getTelemetryHistory(
  name: string,
  entity: string,
  from: string,
  to: string,
  token: string,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ name, entity, from, to });
  return request<TelemetryHistory>(
    `/v1/telemetry/history?${query}`,
    token,
    signal,
  );
}

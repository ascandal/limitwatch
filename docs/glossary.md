# Glossary

### Telemetry Point
Input event sample with form `{name, value, ts, entity}`

- ts is event timestamp
- entity tags the source (e.g., a spacecraft ID).

### Rule
Declarative condition to evaluate against telemetry points for a specific signal name.

#### Signal Names
##### Threshold:
Trip when point `value < lo` or `hi < value`.
- Clear with hysteresis spacing to avoid flap.
- Optional duration to require sustained violation.
- Deadband to ignore tiny value changes.

##### Duration:
Trip when value op threshold holds for at least duration_ms.
- e.g. > 1.2 for 3s
- Hysteresis on clear.

##### ROC (Rate‑of‑Change):
Trip when slope over a window exceeds a threshold.
- Optionally absolute.
- Deadband filters tiny deltas.

### Hysteresis
Extra margin that must be crossed to clear after a trip. Prevents alarm flapping near the threshold.

### Deadband
Ignore small changes around the last value. Reduces noise.

### Per‑rule/per‑entity state
In‑memory tracking (last value/time, arming start time, sliding window) needed for hysteresis, duration, and ROC.

### Ingest queue
Bounded channel decoupling HTTP ingestion from evaluation. Preserves responsiveness and limits memory growth.

### Engine
Workers that consume points from the queue, evaluate rules, and track active/not‑active state in process.

### Migrations
Versioned SQL changes that bootstrap and evolve the DB schema.

### FSM

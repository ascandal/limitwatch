# Performance and scale test

Run from the repository root. No running Go service, web app, or generator is needed.

```bash
make perf
```

This takes about four minutes and writes `bin/performance.json`. Each case starts a real local HTTP server with a fresh temporary SQLite database, then removes it. Demo data is untouched. The scale sweep is opt-in; normal `go test ./...` runs include a two-second mixed-rate correctness check.

The sweep intentionally includes overload. A nonzero exit means at least one case failed a timing or correctness gate; inspect the printed summary and JSON report. Do not use `-race` for performance measurements.

## Local results: November 19, 2026

Apple M4 Max, 16 logical CPUs, 128 GiB RAM, macOS 26.7.3, Go 1.25.4; service commit `3898cd1`. One engine worker; all other conditions are described below. These are passing tested workloads, not universal maximums.

| Workload | Duration | Result |
| --- | --- | --- |
| 500 channels at 10 Hz each | 60 seconds | Passed: 300,000 samples and alarm events |
| 1,000 channels at 1 Hz each | 10 seconds | Passed: 10,000 samples and alarm events |
| 30 channels: ten each at 1, 10, and 100 Hz | 10 seconds | Passed: 11,100 samples and alarm events |
| One channel at 10,000 Hz, batched | 60 seconds | Passed: 600,000 samples and alarm events |
| One channel at 2,000 Hz, one point per request | 10 seconds | Passed: 20,000 samples and alarm events |
| History only at 50,000 samples/second, batched | 60 seconds | Passed: 3,000,000 inserts; latest 100,000 retained |

Higher loads failed: 1,000 channels at 10 Hz, one ruled channel at 20,000 Hz, and one-point requests at 5,000 Hz missed scheduled samples and alarm events. For history only, 100,000 samples/second passed for ten seconds but missed 1,200 scheduled samples in a 60-second run; 200,000 samples/second also failed. All requests actually sent in these cases returned `202`, so HTTP success alone would have hidden the failed workload.

Sender drops mean the configured producer could not submit the full load; they are distinct from server rejection or a missing alarm event. These measurements bound the tested producer/service combination. In passing 60-second ruled cases, scheduled-to-response p99 was 194.5 ms for 500 channels and 14.5 ms for one 10,000 Hz channel.

[Raw JSON reports](performance-results-2026-11-19.json) include passing and failing runs, counts, latencies, and configuration. Sender-queue settings are recorded per run; compare matching configurations. The successful 500-channel and 10,000 Hz configurations use 16 queued batches per sender.

## What it measures

| Case | Load | What success establishes |
| --- | --- | --- |
| Channel count | 10, 100, 250, 500, and 1,000 channels at 10 Hz each | Concurrent channel processing with one threshold rule per channel |
| Mixed rates | 10 channels each at 1, 10, and 100 Hz: 1,110 points/second total | Independent sample counts, source timestamps, and alarm transitions at different rates |
| Frequency | One channel at 100, 500, 1,000, 2,000, 5,000, 10,000, and 20,000 Hz | Frequency with complete alarm processing and telemetry history |
| Archive only | One channel at 5,000, 10,000, 20,000, 50,000, 100,000, and 200,000 Hz, without rules | HTTP ingestion and history-storage capacity, without alarm evaluation |

A channel is a unique signal/entity pair. Every ruled sample alternates above/below its threshold, so every sample must create an activation or clear event. This deliberately demanding workload detects lost evaluations; stable signals and different rule types can have different limits.

Default conditions: ten seconds per case, one engine worker, a 4,096-point ingest queue, up to four HTTP senders, at most 100 points per request, a bounded queue per sender (at least 16 batches, large enough for two flushes), and a 10 ms batching interval. A single channel uses one sender to preserve order. SQLite uses WAL, `synchronous(NORMAL)`, and a five-second busy timeout. The client and server share the test process and machine. There is no web polling, JWT, remote network, or container overhead in this measurement.

The producer follows a fixed schedule instead of waiting for responses to decide when to generate data. If its bounded sender queue fills, it counts the unsent points as failures. Failed HTTP batches are not retried: a `503` can already have accepted a prefix.

A case passes only when:

- Every planned point gets a valid `202` acknowledgment, with no sender drops or HTTP failures.
- Confirmed throughput reaches at least 98% of the target. The p99 time from scheduled batch window to complete HTTP response stays within 250 ms, including sender backlog.
- All planned archive inserts commit. Retained samples have valid, unique sequence values and matching source timestamps, respecting the shared 100,000-sample retention cap.
- Every expected alarm event has the correct type, source time, and per-channel order, and final alarm states match. Remaining alarm work finishes within 250 ms after HTTP sending ends.
- The server logs no errors.

The report includes HTTP p50/p95/p99 latency, schedule-to-completion p99, achieved points/second, queue depth observed in response headers, status counts, archive/event counts, and failures. Latency percentiles are per request; queue depth is sampled, not an exact peak. Alarm drain time is a final backlog check, not an alarm-latency percentile. After sending, the test waits up to five seconds to inspect late events, but completion beyond the 250 ms gate still fails.

## Repeat or focus a test

Longer confirmation of one case:

```bash
LIMITWATCH_PERF=1 LIMITWATCH_PERF_DURATION=60s \
  LIMITWATCH_PERF_OUTPUT="$PWD/bin/performance-60s.json" \
  go test ./tests/performance -run '^TestScale/frequency_1000hz$' \
  -count=1 -timeout 5m -v
```

Compare one-point requests with batching:

```bash
LIMITWATCH_PERF=1 LIMITWATCH_PERF_BATCH=1 \
  LIMITWATCH_PERF_OUTPUT="$PWD/bin/performance-unbatched.json" \
  go test ./tests/performance -run '^TestScale/frequency_' \
  -count=1 -timeout 5m -v
```

Change the tested steps with comma-separated `LIMITWATCH_PERF_CHANNELS`, `LIMITWATCH_PERF_HZ`, and `LIMITWATCH_PERF_ARCHIVE_HZ`. `LIMITWATCH_PERF_CHANNEL_HZ` changes the channel-count sweep's per-channel frequency (default 10). For example:

```bash
LIMITWATCH_PERF=1 LIMITWATCH_PERF_CHANNELS=1000,2000 \
  LIMITWATCH_PERF_CHANNEL_HZ=1 \
  go test ./tests/performance -run '^TestScale/channels_' -count=1 -timeout 5m -v
```

Other controls: `LIMITWATCH_PERF_CLIENTS`, `LIMITWATCH_PERF_QUEUE`, `LIMITWATCH_PERF_DRAIN`, and `LIMITWATCH_PERF_MAX_DELAY`. Duration must be between one second and five minutes. Keep settings identical when comparing runs. Engine workers remain fixed at one because this test requires ordered evaluation within a channel; it does not validate the default multiworker configuration.

## Interpret the limits

Use the highest passing tested rate as a measured lower bound, not a universal maximum. Hz means samples per second **per channel**, not HTTP requests per second. For example, one 10,000 Hz channel sends 100 requests/second when each request holds 100 samples. Longer runs, another machine, more rules per signal, alarm behavior, disk settings, and concurrent readers can change capacity. A failed case with all `202` responses still fails if history or alarms are missing. The engine's 2,048-entry report queue currently drops reports when the manager cannot keep up.

Mixed-rate channels do not need a common frequency. Samples retain their own source timestamps; LimitWatch does not resample or align channels. This test supplies timestamps in order within each channel. It does not establish behavior for late/out-of-order data. Duration and rate-of-change rules are covered by separate functional engine tests, not this threshold throughput sweep.

The console lists at most 200 recently updated streams, refreshes telemetry every two seconds, and plots at most 300 time buckets. These display limits differ from ingestion capacity. Telemetry history keeps 100,000 samples across **all** channels and stores timestamps at millisecond precision. At 20,000 points/second, that is about five seconds of retained history; sub-millisecond samples are retained but share millisecond timestamps.

#!/usr/bin/env python3
"""Run the console against a temporary database and a repeatable synthetic trace."""

import argparse
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import time
from datetime import datetime, timedelta, timezone
from urllib.error import URLError
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
RULE = {
    "name": "TestStandHot",
    "signal": "TestStand.TempC",
    "type": "threshold",
    "severity": "warn",
    "enabled": True,
    "latch": True,
    "config": {"hi": 80, "hysteresis": 3, "duration_ms": 500},
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--check", action="store_true", help="verify the full replay and restart, then exit")
    args = parser.parse_args()
    # Refuse to send demo data to a service already listening on this port.
    with socket.socket() as probe:
        try:
            probe.bind(("127.0.0.1", args.port))
        except OSError:
            parser.error(f"Port {args.port} is already in use; choose --port or stop that service")
    url = f"http://127.0.0.1:{args.port}"

    def call(path, body=None):
        data = json.dumps(body).encode() if body is not None else None
        req = Request(url + path, data=data, headers={"Content-Type": "application/json"})
        with urlopen(req, timeout=5) as response:
            payload = response.read()
            return json.loads(payload) if payload.startswith((b"{", b"[", b"null")) else payload.decode()

    with tempfile.TemporaryDirectory(prefix="limitwatch-demo-") as tmp:
        binary = Path(tmp) / "alarmd"
        subprocess.run(["go", "build", "-o", str(binary), "./cmd/alarmd"], cwd=ROOT, check=True)
        env = {**os.environ, "PORT": str(args.port), "WORKERS": "1", "JWT_SECRET": "", "PPROF_ENABLED": "false", "TRACE_ENABLED": "false", "DB_DSN": f"file:{tmp}/demo.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"}
        process = None
        log = open(Path(tmp) / "server.log", "w+")

        def stop():
            nonlocal process
            if process is not None and process.poll() is None:
                process.send_signal(signal.SIGTERM)
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            process = None

        def start():
            nonlocal process
            process = subprocess.Popen([str(binary)], env=env, cwd=ROOT, stdout=log, stderr=log)
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    log.flush()
                    raise RuntimeError((Path(tmp) / "server.log").read_text())
                try:
                    if call("/healthz") == "ok":
                        return
                except (URLError, OSError):
                    pass
                time.sleep(0.1)
            raise RuntimeError("Demo service did not become ready")

        def wait_alarm(state):
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                alarms = call("/v1/alarms") or []
                for alarm in alarms:
                    if alarm["entity"] == "STAND-1" and alarm["state"] == state:
                        return alarm
                time.sleep(0.05)
            raise RuntimeError(f"Expected STAND-1 alarm state {state}")

        try:
            start()
            call("/v1/rules", RULE)
            stop()
            start()  # Rules are compiled at startup.
            base_time = datetime.now(timezone.utc) - timedelta(seconds=3)

            def send(values, offsets):
                points = [{"name": "TestStand.TempC", "entity": "STAND-1", "value": value, "ts": (base_time + timedelta(seconds=offset)).isoformat().replace("+00:00", "Z")} for value, offset in zip(values, offsets)]
                call("/v1/ingest", points)

            send([78, 82, 82, 82], [0, 0.3, 0.6, 0.9])
            alarm = wait_alarm("active")
            path = f'/v1/alarms/{alarm["id"]}'
            print(f"Synthetic telemetry API ready: {url}", flush=True)
            print(f"Run the console separately: LIMITWATCH_API_URL={url} npm --prefix web run dev", flush=True)
            print("Open http://localhost:3000 and select STAND-1. All demo data is temporary.", flush=True)
            if args.check:
                call(path + "/ack", {})
                wait_alarm("acked")
                send([79, 76], [1.2, 1.5])
                time.sleep(0.2)
                stop()
                start()
                wait_alarm("acked")
                call(path + "/clear", {})
                wait_alarm("inactive")
                events = call(path + "/events")["events"]
                if [event["type"] for event in events] != ["activated", "acked", "cleared"]:
                    raise RuntimeError(f"Unexpected trace: {events}")
                print("PASS: activation, acknowledgment, recovery, restart persistence, clear, and HTTP history", flush=True)
            else:
                send([79, 76], [1.2, 1.5])
                print("Recovery samples sent; the latched alarm remains available for operator action.", flush=True)
                while process.poll() is None:
                    time.sleep(0.5)
        except KeyboardInterrupt:
            print("\nDemo stopped.")
        finally:
            stop()
            log.close()


if __name__ == "__main__":
    main()

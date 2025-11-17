package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ascandal/limitwatch/internal/config"
	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/repository/sqlite"
	"github.com/ascandal/limitwatch/internal/storage"
)

func usage() {
	fmt.Fprintf(
		os.Stderr,
		`alarmctl - temporary CLI for rules, alarms, and test signal generation

Usage:
  alarmctl rule create --name NAME --signal SIGNAL --type threshold --severity warn --latch [--config-json '{"lo":23.5,"hysteresis":0.2}']
  alarmctl rule list
  alarmctl alarm create --rule-id ID --entity ENTITY --state active
  alarmctl alarm list
  alarmctl gen [flags]   # fake signal generator, see: alarmctl gen -h

Env:
  DB_DSN   SQLite/Postgres DSN (default: file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000))
`)

	os.Exit(2)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage()
	}

	dsn := config.Getenv(
		"DB_DSN",
		"file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
	)

	db, err := storage.Open(dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	defer storage.Close(db, nil) // ignore close error

	if err := storage.Migrate(context.Background(), db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	repo := sqlite.New(db)

	switch os.Args[1] {
	case "rule":
		if len(os.Args) < 3 {
			usage()
		}

		switch os.Args[2] {
		case "create":
			fs := flag.NewFlagSet("rule create", flag.ExitOnError)
			name := fs.String("name", "", "rule name (unique)")
			signal := fs.String("signal", "", "signal name (e.g., Battery.Voltage)")
			rtype := fs.String("type", "threshold", "rule type: threshold|roc|duration")
			severity := fs.String("severity", "warn", "severity: info|warn|crit")
			latch := fs.Bool("latch", false, "latch until cleared explicitly")
			cfgJSON := fs.String("config-json", "{}", "JSON for rule config (type-specific)")
			_ = fs.Parse(os.Args[3:])

			if *name == "" || *signal == "" {
				return fmt.Errorf("name and signal are required")
			}

			raw := json.RawMessage(*cfgJSON)
			r := &domain.Rule{
				Name:     *name,
				Signal:   *signal,
				Type:     domain.RuleType(strings.ToLower(*rtype)),
				Severity: domain.Severity(strings.ToLower(*severity)),
				Latch:    *latch,
				Enabled:  true,
				Config:   raw,
			}

			if err := repo.Rules.Create(context.Background(), r); err != nil {
				return fmt.Errorf("create rule: %w", err)
			}

			fmt.Printf("created rule id=%d name=%s\n", r.ID, r.Name)

		case "list":
			rules, err := repo.Rules.List(context.Background())

			if err != nil {
				return fmt.Errorf("list rules: %w", err)
			}

			for _, r := range rules {
				fmt.Printf(
					"%d\t%s\t%s\t%s\t%t\t%s\n",
					r.ID,
					r.Name,
					r.Signal,
					r.Type,
					r.Latch,
					r.Severity,
				)
			}
		default:
			usage()
		}

	case "alarm":
		if len(os.Args) < 3 {
			usage()
		}

		switch os.Args[2] {
		case "create":
			fs := flag.NewFlagSet("alarm create", flag.ExitOnError)
			ruleID := fs.Int64("rule-id", 0, "rule id")
			entity := fs.String("entity", "", "entity label (e.g., SC-1)")
			state := fs.String("state", "active", "state: inactive|active|acked|silenced")
			note := fs.String("note", "", "optional note")
			_ = fs.Parse(os.Args[3:])

			if *ruleID == 0 || *entity == "" {
				return fmt.Errorf("rule-id and entity are required")
			}

			a := &domain.Alarm{
				RuleID:    *ruleID,
				Entity:    *entity,
				State:     domain.AlarmState(strings.ToLower(*state)),
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}

			if err := repo.Alarms.Create(context.Background(), a); err != nil {
				return fmt.Errorf("create alarm: %w", err)
			}

			if strings.TrimSpace(*note) != "" {
				ev := &domain.AlarmEvent{
					AlarmID: a.ID,
					Type:    domain.EventNote,
					Actor:   "alarmctl",
					At:      time.Now().UTC(),
					Note:    *note,
				}

				if err := repo.Alarms.CreateEvent(context.Background(), ev); err != nil {
					return fmt.Errorf("add event: %w", err)
				}
			}

			fmt.Printf(
				"created alarm id=%d rule=%d entity=%s state=%s\n",
				a.ID,
				a.RuleID,
				a.Entity,
				a.State,
			)

		case "list":
			alarms, err := repo.Alarms.List(context.Background(), 100)

			if err != nil {
				return fmt.Errorf("list alarms: %w", err)
			}

			for _, a := range alarms {
				fmt.Printf(
					"%d\t%d\t%s\t%s\t%s\n",
					a.ID,
					a.RuleID,
					a.Entity,
					a.State,
					a.UpdatedAt.Format(time.RFC3339),
				)
			}
		default:
			usage()
		}
	case "gen", "generator", "signalgen":
		return runGen(os.Args[2:])
	default:
		usage()
	}

	return nil
}

// runGen starts a fake telemetry generator that POSTs points to /v1/ingest.
// Usage:
//
//	alarmctl gen -url http://localhost:8080 -entity SC-1 -pattern step -rate 10 -seed 42 [-token operator.jwt] [-duration 1m]
func runGen(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)

	baseURL := fs.String("url", "http://127.0.0.1:8080", "base URL of alarmd (e.g., http://127.0.0.1:8080)")
	entity := fs.String("entity", "SC-1", "entity id/tag to annotate samples with")
	pattern := fs.String("pattern", "step", "pattern: step|sine|ramp|noise")
	rate := fs.Float64("rate", 10.0, "points per second (each tick sends 2 points: Battery.Voltage and Temp.C)")
	seed := fs.Int64("seed", 42, "random seed for noise")
	tokenPath := fs.String("token", "", "path to JWT file for Authorization header (optional)")
	runFor := fs.Duration("duration", 0, "how long to run (0 = until Ctrl-C)")
	verbose := fs.Bool("v", false, "verbose logs")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *rate <= 0 {
		*rate = 10.0
	}

	period := time.Duration(float64(time.Second) / *rate)

	target := strings.TrimRight(*baseURL, "/") + "/v1/ingest"

	var bearer string

	if *tokenPath != "" {
		b, err := os.ReadFile(*tokenPath)

		if err != nil {
			return fmt.Errorf("read token: %w", err)
		}

		bearer = strings.TrimSpace(string(b))
	}

	type point struct {
		Name   string  `json:"name"`
		Value  float64 `json:"value"`
		Entity string  `json:"entity"`
	}

	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(period)

	defer ticker.Stop()

	ctx, cancel := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)

	defer cancel()

	var timeout <-chan time.Time

	if *runFor > 0 {
		timer := time.NewTimer(*runFor)
		defer timer.Stop()
		timeout = timer.C
	}

	rnd := rand.New(rand.NewSource(*seed))
	start := time.Now()
	var i int64

	logf := func(format string, args ...any) {
		if *verbose {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}

	post := func(payload []byte) error {
		req, err := http.NewRequest(
			http.MethodPost, target, bytes.NewReader(payload),
		)

		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")

		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}

		resp, err := client.Do(req)

		if err != nil {
			return err
		}

		defer func() {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)

			return fmt.Errorf(
				"ingest %s: status=%d body=%s", target, resp.StatusCode, string(b),
			)
		}

		return nil
	}

	logf(
		"signal generator start: url=%s entity=%s pattern=%s rate=%.2fHz seed=%d",
		target, *entity, *pattern, *rate, *seed,
	)

	for {
		select {
		case <-ctx.Done():
			logf("signal generator: stop (signal)")
			return nil
		case <-timeout:
			logf(
				"signal generator: stop (duration %s)",
				time.Since(start).Truncate(time.Millisecond),
			)
			return nil
		case ts := <-ticker.C:
			_ = ts // we don't currently use the timestamp directly

			// Battery.Voltage
			v := 24.2

			switch *pattern {
			case "step":
				if i%50 < 25 {
					v = 24.2
				} else {
					v = 23.3 // low step to cross threshold
				}
			case "sine":
				// slow sine around 24.0
				v = 24.0 + 0.8*math.Sin(2*math.Pi*float64(i)/100.0)
			case "ramp":
				// ramps down then wraps every ~20 seconds at 10Hz
				v = 24.2 - float64((i%200))/100.0
			case "noise":
				v = 24.0 + (rnd.Float64()-0.5)*1.5
			default:
				// fall back to step if unknown
				if i%50 < 25 {
					v = 24.2
				} else {
					v = 23.3
				}
			}

			// Temp.C with slight upward trend to exercise ROC
			elapsed := time.Since(start).Seconds()
			base := 20.0 + elapsed*0.2
			temp := base + (rnd.Float64()-0.5)*0.1

			pts := []point{
				{Name: "Battery.Voltage", Value: v, Entity: *entity},
				{Name: "Temp.C", Value: temp, Entity: *entity},
			}

			body, err := json.Marshal(pts)

			if err != nil {
				return fmt.Errorf("encode points: %w", err)
			}

			if err := post(body); err != nil {
				logf("WARN: %v", err)
			} else {
				logf(
					"tick=%d sent Battery.Voltage=%.3f Temp.C=%.3f entity=%s",
					i, v, temp, *entity,
				)
			}

			i++
		}
	}
}

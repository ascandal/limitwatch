package main

import (
    "context"
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "os"
    "strings"
    "time"

    "github.com/ascandal/limitwatch/internal/config"
    "github.com/ascandal/limitwatch/internal/domain"
    "github.com/ascandal/limitwatch/internal/repository/sqlite"
    "github.com/ascandal/limitwatch/internal/storage"
)

func usage() {
    fmt.Fprintf(
        os.Stderr,
        `alarmctl - temporary CLI for Phase 1 (rules & alarms)
Usage:
  alarmctl rule create --name NAME --signal SIGNAL --type threshold --severity warn --latch [--config-json '{"lo":23.5,"hysteresis":0.2}']
  alarmctl rule list
  alarmctl alarm create --rule-id ID --entity ENTITY --state active
  alarmctl alarm list

Env:
  DB_DSN   SQLite/Postgres DSN (default: file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000))
`)

    os.Exit(2)
}

func main() {
    if len(os.Args) < 2 {
        usage()
    }

    dsn := config.Getenv(
        "DB_DSN",
        "file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
    )
    db, err := storage.Open(dsn)

    if err != nil {
        fmt.Fprintln(os.Stderr, "open db:", err)
        os.Exit(1)
    }

    defer db.Close()
    if err := storage.Migrate(context.Background(), db); err != nil {
        fmt.Fprintln(os.Stderr, "migrate:", err)
        os.Exit(1)
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
                fmt.Fprintln(os.Stderr, "name and signal are required")
                os.Exit(1)
            }

            var raw json.RawMessage = json.RawMessage(*cfgJSON)
            r := &domain.Rule{
                Name:    *name,
                Signal:  *signal,
                Type:    domain.RuleType(strings.ToLower(*rtype)),
                Severity: domain.Severity(strings.ToLower(*severity)),
                Latch:   *latch,
                Enabled: true,
                Config:  raw,
            }

            if err := repo.Rules.Create(context.Background(), r); err != nil {
                fmt.Fprintln(os.Stderr, "create rule:", err)
                os.Exit(1)
            }

            fmt.Printf("created rule id=%d name=%s\n", r.ID, r.Name)

        case "list":
            rules, err := repo.Rules.List(context.Background())

            if err != nil {
                fmt.Fprintln(os.Stderr, "list rules:", err)
                os.Exit(1)
            }

            for _, r := range rules {
                fmt.Printf(
                    "%d\t%s\t%s\t%s\t%t\t%s\n",
                    r.ID,
                    r.Name,
                    r.Signal,
                    r.Type,
                    r.Latch,
                    r.Severity
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
                fmt.Fprintln(os.Stderr, "rule-id and entity are required")
                os.Exit(1)
            }

            a := &domain.Alarm{
                RuleID: *ruleID,
                Entity: *entity,
                State:  domain.AlarmState(strings.ToLower(*state)),
                CreatedAt: time.Now().UTC(),
                UpdatedAt: time.Now().UTC(),
            }

            if err := repo.Alarms.Create(context.Background(), a); err != nil {
                fmt.Fprintln(os.Stderr, "create alarm:", err)
                os.Exit(1)
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
                    fmt.Fprintln(os.Stderr, "add event:", err); os.Exit(1)
                }
            }

            fmt.Printf(
                "created alarm id=%d rule=%d entity=%s state=%s\n",
                a.ID,
                a.RuleID,
                a.Entity,
                a.State
            )

        case "list":
            alarms, err := repo.Alarms.List(context.Background(), 100)

            if err != nil {
                fmt.Fprintln(os.Stderr, "list alarms:", err)
                os.Exit(1)
            }

            for _, a := range alarms {
                fmt.Printf(
                    "%d\t%d\t%s\t%s\t%s\n",
                    a.ID,
                    a.RuleID,
                    a.Entity,
                    a.State,
                    a.UpdatedAt.Format(time.RFC3339)
                )
            }
        default:
            usage()
        }
    default:
        usage()
    }
}

package manager

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/engine"
	"github.com/ascandal/limitwatch/internal/repository"
	"github.com/ascandal/limitwatch/internal/repository/sqlite"
	"github.com/ascandal/limitwatch/internal/storage"
)

func fixture(t *testing.T) (*Manager, *sql.DB, repository.Repositories, int64) {
	t.Helper()
	db, err := storage.Open(fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := sqlite.New(db)
	rule := &domain.Rule{Name: "hot", Signal: "Temp", Type: domain.RuleThreshold, Severity: domain.SevWarn, Enabled: true, Latch: true, Config: []byte(`{"hi":80}`)}
	if err := store.Rules.Create(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	repos := repository.Repositories{Rules: store.Rules, Alarms: store.Alarms}
	return New(repos, nil), db, repos, rule.ID
}

func TestTransitionsRollbackWhenEventWriteFails(t *testing.T) {
	m, db, repos, ruleID := fixture(t)
	ctx := context.Background()
	// A database trigger injects a failure after the state write would succeed.
	if _, err := db.Exec(`CREATE TRIGGER fail_event BEFORE INSERT ON alarm_events BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`); err != nil {
		t.Fatal(err)
	}
	report := engine.EvalReport{RuleID: ruleID, Entity: "STAND-1", At: time.Now().UTC(), Active: true, Latch: true}
	m.handleReport(ctx, report)
	if _, err := repos.Alarms.GetByRuleEntity(ctx, ruleID, report.Entity); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("failed activation left a record: %v", err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_event`); err != nil {
		t.Fatal(err)
	}
	m.handleReport(ctx, report)
	alarm, err := repos.Alarms.GetByRuleEntity(ctx, ruleID, report.Entity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_event BEFORE INSERT ON alarm_events BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := m.Ack(ctx, alarm.ID, "operator"); err == nil {
		t.Fatal("expected acknowledgment failure")
	}
	if err := m.Clear(ctx, alarm.ID, "operator"); err == nil {
		t.Fatal("expected clear failure")
	}
	saved, err := repos.Alarms.GetByID(ctx, alarm.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := repos.Alarms.ListEvents(ctx, alarm.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != domain.Active || saved.AckedBy != nil || len(events) != 1 {
		t.Fatalf("partial transition: alarm=%+v events=%+v", saved, events)
	}
}

func TestConcurrentEvaluatorAndOperatorPreserveAcknowledgment(t *testing.T) {
	m, _, repos, ruleID := fixture(t)
	ctx := context.Background()
	report := engine.EvalReport{RuleID: ruleID, Entity: "STAND-1", At: time.Now().UTC(), Active: true, Latch: true}
	m.handleReport(ctx, report)
	alarm, err := repos.Alarms.GetByRuleEntity(ctx, ruleID, report.Entity)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for worker := 0; worker < 12; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				m.handleReport(ctx, report)
				if err := m.Ack(ctx, alarm.ID, "operator"); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	saved, err := repos.Alarms.GetByID(ctx, alarm.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := repos.Alarms.ListEvents(ctx, alarm.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != domain.Acked || saved.AckedBy == nil || *saved.AckedBy != "operator" || len(events) != 2 {
		t.Fatalf("lost acknowledgment or duplicate event: alarm=%+v events=%+v", saved, events)
	}
	if err := m.Clear(ctx, alarm.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	m.handleReport(ctx, report)
	saved, err = repos.Alarms.GetByID(ctx, alarm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != domain.Active || saved.AckedBy != nil {
		t.Fatalf("reactivation retained stale operator state: %+v", saved)
	}
}

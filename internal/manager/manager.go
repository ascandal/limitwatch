package manager

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/engine"
	"github.com/ascandal/limitwatch/internal/repository"
)

// Manager applies evaluator reports and operator actions inside serialized
// SQLite transactions. Each state transition and audit event commit together.
type Manager struct {
	repo   repository.Repositories
	logger *slog.Logger
}

func New(repo repository.Repositories, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{repo: repo, logger: logger}
}

func (m *Manager) Start(ctx context.Context, reports <-chan engine.EvalReport) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case report, ok := <-reports:
				if !ok {
					return
				}
				if report != (engine.EvalReport{}) {
					m.handleReport(ctx, report)
				}
			}
		}
	}()
}

func (m *Manager) handleReport(ctx context.Context, r engine.EvalReport) {
	err := m.repo.Alarms.Transact(ctx, func(repo repository.AlarmRepository) error {
		alarm, err := repo.GetByRuleEntity(ctx, r.RuleID, r.Entity)
		if errors.Is(err, domain.ErrNotFound) {
			if !r.Active {
				return nil
			}
			alarm = &domain.Alarm{
				RuleID: r.RuleID, Entity: r.Entity, State: domain.Active,
				FirstSeen: &r.At, LastSeen: &r.At,
			}
			if err := repo.Create(ctx, alarm); err != nil {
				return err
			}
			return recordEvent(ctx, repo, alarm.ID, domain.EventActivated, "engine", r.At, nil)
		}
		if err != nil {
			return err
		}
		// Record-update time is distinct from the source telemetry timestamp.
		alarm.UpdatedAt = time.Now().UTC()
		if r.Active {
			alarm.LastSeen = &r.At
			if alarm.State == domain.Inactive {
				alarm.State = domain.Active
				alarm.AckedBy = nil
				alarm.SilenceUntil = nil
				if alarm.FirstSeen == nil {
					alarm.FirstSeen = &r.At
				}
				if err := repo.Update(ctx, alarm); err != nil {
					return err
				}
				return recordEvent(ctx, repo, alarm.ID, domain.EventActivated, "engine", r.At, nil)
			}
			// An active report preserves an operator's acknowledgment/silence.
			return repo.Update(ctx, alarm)
		}
		if r.Latch || alarm.State == domain.Inactive {
			return nil
		}
		alarm.State = domain.Inactive
		alarm.AckedBy = nil
		alarm.SilenceUntil = nil
		if err := repo.Update(ctx, alarm); err != nil {
			return err
		}
		return recordEvent(ctx, repo, alarm.ID, domain.EventCleared, "engine", r.At, nil)
	})
	if err != nil {
		m.logger.Error("apply evaluation report failed", "err", err, "rule", r.RuleID, "entity", r.Entity)
	}
}

// Ack is idempotent and rejects inactive alarms.
func (m *Manager) Ack(ctx context.Context, id int64, actor string) error {
	return m.change(ctx, id, actor, domain.EventAcked, func(alarm *domain.Alarm) (bool, json.RawMessage, error) {
		if alarm.State == domain.Inactive {
			return false, nil, domain.ErrConflict
		}
		if alarm.State == domain.Acked {
			return false, nil, nil
		}
		alarm.State = domain.Acked
		alarm.AckedBy = &actor
		alarm.SilenceUntil = nil
		return true, nil, nil
	})
}

func (m *Manager) Silence(ctx context.Context, id int64, until time.Time, actor string) error {
	return m.change(ctx, id, actor, domain.EventSilenced, func(alarm *domain.Alarm) (bool, json.RawMessage, error) {
		if alarm.State == domain.Inactive {
			return false, nil, domain.ErrConflict
		}
		alarm.State = domain.Silenced
		alarm.SilenceUntil = &until
		payload, err := json.Marshal(map[string]string{"until": until.UTC().Format(time.RFC3339Nano)})
		return true, payload, err
	})
}

// Clear is idempotent. A later active sample can activate the alarm again.
func (m *Manager) Clear(ctx context.Context, id int64, actor string) error {
	return m.change(ctx, id, actor, domain.EventCleared, func(alarm *domain.Alarm) (bool, json.RawMessage, error) {
		if alarm.State == domain.Inactive {
			return false, nil, nil
		}
		alarm.State = domain.Inactive
		alarm.AckedBy = nil
		alarm.SilenceUntil = nil
		return true, nil, nil
	})
}

func (m *Manager) change(ctx context.Context, id int64, actor string, kind domain.AlarmEventType,
	apply func(*domain.Alarm) (bool, json.RawMessage, error)) error {
	return m.repo.Alarms.Transact(ctx, func(repo repository.AlarmRepository) error {
		alarm, err := repo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		changed, payload, err := apply(alarm)
		if err != nil || !changed {
			return err
		}
		now := time.Now().UTC()
		alarm.UpdatedAt = now
		if err := repo.Update(ctx, alarm); err != nil {
			return err
		}
		return recordEvent(ctx, repo, id, kind, actor, now, payload)
	})
}

func recordEvent(ctx context.Context, repo repository.AlarmRepository, id int64,
	kind domain.AlarmEventType, actor string, at time.Time, payload json.RawMessage) error {
	return repo.CreateEvent(ctx, &domain.AlarmEvent{
		AlarmID: id, Type: kind, Actor: actor, At: at, Payload: payload,
	})
}

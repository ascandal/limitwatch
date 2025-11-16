package manager

import (
	"context"
	"errors"
	"time"

	"log/slog"

	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/engine"
	"github.com/ascandal/limitwatch/internal/repository"
)

// Consumes engine evaluation reports and updates persistent alarms.
type Manager struct {
	repo   repository.Repositories
	logger *slog.Logger

	// simple debounce window to avoid hammering DB for same rule rapidly
	// (optional for now; not used)
}

// Creates a Manager backed by the provided repositories and logger.
func New(repo repository.Repositories, logger *slog.Logger) *Manager {
	return &Manager{repo: repo, logger: logger}
}

// Launches a goroutine that consumes evaluation reports until the context
// is canceled or the reports channel is closed.
func (m *Manager) Start(ctx context.Context, reports <-chan engine.EvalReport) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case r, ok := <-reports:
				if !ok {
					// Channel closed: nothing more to process.
					return
				}

				if r == (engine.EvalReport{}) {
					continue
				}

				m.handleReport(ctx, r)
			}
		}
	}()
}

func (m *Manager) handleReport(ctx context.Context, r engine.EvalReport) {
	// Fetch existing alarm by (rule, entity)
	al, err := m.repo.Alarms.GetByRuleEntity(ctx, r.RuleID, r.Entity)
	now := r.At

	if errors.Is(err, domain.ErrNotFound) || al == nil {
		if r.Active {
			// Create new active alarm
			a := &domain.Alarm{
				RuleID:    r.RuleID,
				Entity:    r.Entity,
				State:     domain.Active,
				FirstSeen: &r.At,
				LastSeen:  &r.At,
				CreatedAt: now,
				UpdatedAt: now,
			}

			if err := m.repo.Alarms.Create(ctx, a); err != nil {
				m.logger.Error("create alarm failed", "err", err, "rule", r.RuleID, "entity", r.Entity)
				return
			}

			if err := m.repo.Alarms.CreateEvent(ctx, &domain.AlarmEvent{
				AlarmID: a.ID, Type: domain.EventActivated, Actor: "engine", At: r.At,
			}); err != nil {
				m.logger.Error(
					"create alarm event failed",
					"err", err, "alarm", a.ID, "type", domain.EventActivated,
				)
			}

			return
		}

		// inactive & not found -> nothing to do
		return
	} else if err != nil {
		m.logger.Error("get alarm failed", "err", err, "rule", r.RuleID, "entity", r.Entity)
		return
	}

	// Update paths based on activity
	if r.Active {
		// Always update last_seen
		al.LastSeen = &r.At
		al.UpdatedAt = now

		// If previously inactive -> activate
		if al.State == domain.Inactive {
			al.State = domain.Active
			if al.FirstSeen == nil {
				al.FirstSeen = &r.At
			}

			if err := m.repo.Alarms.Update(ctx, al); err != nil {
				m.logger.Error("activate alarm update failed", "err", err, "alarm", al.ID)
				return
			}

			if err := m.repo.Alarms.CreateEvent(ctx, &domain.AlarmEvent{
				AlarmID: al.ID, Type: domain.EventActivated, Actor: "engine", At: r.At,
			}); err != nil {
				m.logger.Error(
					"create alarm event failed", "err",
					err, "alarm", al.ID, "type", domain.EventActivated,
				)
			}

			return
		}

		// Already active/acked/silenced -> just update last_seen
		if err := m.repo.Alarms.Update(ctx, al); err != nil {
			m.logger.Error("touch alarm failed", "err", err, "alarm", al.ID)
		}

		return
	}

	// r.Active == false: condition not present
	if r.Latch {
		// latched rules do not auto-clear
		return
	}

	// unlatch: auto-clear if currently active/acked/silenced
	switch al.State {
	case domain.Active, domain.Acked, domain.Silenced:
		al.State = domain.Inactive
		al.SilenceUntil = nil
		al.AckedBy = nil
		al.UpdatedAt = now

		if err := m.repo.Alarms.Update(ctx, al); err != nil {
			m.logger.Error("clear alarm failed", "err", err, "alarm", al.ID)
			return
		}

		if err := m.repo.Alarms.CreateEvent(ctx, &domain.AlarmEvent{
			AlarmID: al.ID, Type: domain.EventCleared, Actor: "engine", At: r.At,
		}); err != nil {
			m.logger.Error(
				"create alarm event failed",
				"err", err, "alarm", al.ID, "type", domain.EventCleared,
			)
		}
	}
}

// ------------------------------
// Ops helpers used by API
// ------------------------------

// Marks the alarm as acknowledged.
// It is idempotent: if the alarm is already in the Acked state, no change occurs.
// Returns ErrConflict if the alarm is inactive.
func (m *Manager) Ack(ctx context.Context, alarmID int64, actor string) error {
	al, err := m.repo.Alarms.GetByID(ctx, alarmID)

	if err != nil {
		return err
	}

	// idempotent
	if al.State == domain.Acked {
		return nil
	}

	if al.State == domain.Inactive {
		return domain.ErrConflict
	}

	al.State = domain.Acked
	al.AckedBy = &actor
	now := time.Now().UTC()
	al.UpdatedAt = now

	if err := m.repo.Alarms.Update(ctx, al); err != nil {
		return err
	}

	return m.repo.Alarms.CreateEvent(ctx, &domain.AlarmEvent{
		AlarmID: al.ID, Type: domain.EventAcked, Actor: actor, At: now,
	})
}

// Moves the alarm to the Silenced state until the given time.
// Returns ErrConflict if the alarm is inactive.
func (m *Manager) Silence(ctx context.Context, alarmID int64, until time.Time, actor string) error {
	al, err := m.repo.Alarms.GetByID(ctx, alarmID)

	if err != nil {
		return err
	}

	if al.State == domain.Inactive {
		return domain.ErrConflict
	}

	al.State = domain.Silenced
	al.SilenceUntil = &until
	now := time.Now().UTC()
	al.UpdatedAt = now

	if err := m.repo.Alarms.Update(ctx, al); err != nil {
		return err
	}

	payload := []byte(`{"until":"` + until.UTC().Format(time.RFC3339Nano) + `"}`)

	return m.repo.Alarms.CreateEvent(ctx, &domain.AlarmEvent{
		AlarmID: al.ID, Type: domain.EventSilenced, Actor: actor, At: now, Payload: payload,
	})
}

// Moves the alarm to the Inactive state.
// It is idempotent: clearing an already inactive alarm is a no-op.
func (m *Manager) Clear(ctx context.Context, alarmID int64, actor string) error {
	al, err := m.repo.Alarms.GetByID(ctx, alarmID)

	if err != nil {
		return err
	}

	if al.State == domain.Inactive {
		return nil // idempotent
	}

	al.State = domain.Inactive
	al.SilenceUntil = nil
	al.AckedBy = nil
	now := time.Now().UTC()
	al.UpdatedAt = now

	if err := m.repo.Alarms.Update(ctx, al); err != nil {
		return err
	}

	return m.repo.Alarms.CreateEvent(ctx, &domain.AlarmEvent{
		AlarmID: al.ID, Type: domain.EventCleared, Actor: actor, At: now,
	})
}

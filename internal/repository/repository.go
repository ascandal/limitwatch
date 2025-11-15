package repository

import (
	"context"

	"github.com/ascandal/limitwatch/internal/domain"
)

type RuleRepository interface {
	Create(ctx context.Context, r *domain.Rule) error
	GetByID(ctx context.Context, id int64) (*domain.Rule, error)
	GetByName(ctx context.Context, name string) (*domain.Rule, error)
	List(ctx context.Context) ([]domain.Rule, error)
	// Update/Delete will come in later phases as needed.
}

type AlarmRepository interface {
	Create(ctx context.Context, a *domain.Alarm) error
	GetByID(ctx context.Context, id int64) (*domain.Alarm, error)
	GetByRuleEntity(ctx context.Context, ruleID int64, entity string) (*domain.Alarm, error)
	List(ctx context.Context, limit int) ([]domain.Alarm, error)
	Update(ctx context.Context, a *domain.Alarm) error
	CreateEvent(ctx context.Context, ev *domain.AlarmEvent) error
	ListEvents(ctx context.Context, alarmID int64) ([]domain.AlarmEvent, error)
}

// Aggregate entry point (optional)
type Repositories struct {
	Rules  RuleRepository
	Alarms AlarmRepository
}
